package grpcf

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/downtime"
	"github.com/a-novel-kit/golib/otel"
)

// Fault returns the error a handler answers a server fault with, such as
// Fault(codes.Internal, "internal error", err). The caller receives code and msg alone.
//
// A server from [NewServer] reports a call that ends with Internal or Unavailable as a fault. The
// call's server span names the type of the innermost error the call returned. A bare status error
// wraps nothing, so Fault keeps cause for that type.
func Fault(code codes.Code, msg string, cause error) error {
	return &faultError{status: status.New(code, msg), cause: cause}
}

// faultError answers a call with status, and wraps cause for errors.Is, errors.As and fault reports.
type faultError struct {
	status *status.Status
	cause  error
}

func (err *faultError) Error() string {
	return err.status.Err().Error()
}

// GRPCStatus is the status gRPC sends the caller.
func (err *faultError) GRPCStatus() *status.Status {
	return err.status
}

func (err *faultError) Unwrap() error {
	return err.cause
}

// captureUnaryInterceptor reports the server fault a unary call ends with.
func captureUnaryInterceptor(
	ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
) (any, error) {
	response, err := handler(ctx, req)
	captureFault(ctx, err)

	return response, err
}

// captureStreamInterceptor reports the server fault a streaming call ends with.
func captureStreamInterceptor(
	srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler,
) error {
	err := handler(srv, ss)
	captureFault(ss.Context(), err)

	return err
}

// captureFault reports err when it ends the call with Internal or Unavailable: the call's server
// span names its type. A dependency's planned downtime refusal is not a fault.
func captureFault(ctx context.Context, err error) {
	code := status.Code(err)
	if (code == codes.Internal || code == codes.Unavailable) && !downtime.Refused(err) {
		trace.SpanFromContext(ctx).SetAttributes(otel.ErrorType(err))
	}
}
