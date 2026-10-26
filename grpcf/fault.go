package grpcf

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/a-novel-kit/golib/downtime"
	"github.com/a-novel-kit/golib/sentryf"
)

// Fault returns the error a handler answers a server fault with, such as
// Fault(codes.Internal, "internal error", err). The caller receives code and msg alone.
//
// A server from [NewServer] sends a call that ends with Internal or Unavailable to Sentry, grouped by
// the type of the innermost error the call returned. A bare status error wraps nothing, so Fault
// keeps cause for that grouping.
func Fault(code codes.Code, msg string, cause error) error {
	return &faultError{status: status.New(code, msg), cause: cause}
}

// faultError answers a call with status, and wraps cause for errors.Is, errors.As and Sentry.
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
	ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler,
) (any, error) {
	response, err := handler(ctx, req)
	captureFault(ctx, info.FullMethod, err)

	return response, err
}

// captureStreamInterceptor reports the server fault a streaming call ends with.
func captureStreamInterceptor(
	srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler,
) error {
	err := handler(srv, ss)
	captureFault(ss.Context(), info.FullMethod, err)

	return err
}

// captureFault sends err to Sentry when it ends the call to method with Internal or Unavailable. A
// dependency's planned downtime refusal is not a fault.
func captureFault(ctx context.Context, method string, err error) {
	code := status.Code(err)
	if (code == codes.Internal || code == codes.Unavailable) && !downtime.Refused(err) {
		sentryf.Capture(ctx, method, err)
	}
}
