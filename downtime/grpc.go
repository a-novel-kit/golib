package downtime

import (
	"context"
	"slices"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Reason is the ErrorInfo reason of a gRPC call refused during a planned downtime.
const Reason = "PLANNED_DOWNTIME"

// refusal is the status of every refused call. WithDetails fails only on an OK status.
var refusal, _ = status.New(codes.Unavailable, "planned downtime").WithDetails(&errdetails.ErrorInfo{Reason: Reason})

// UnaryServerInterceptor refuses unary calls once start has passed, except for methods starting
// with one of open, such as "/grpc.health.v1.Health/". A refused call returns codes.Unavailable
// with an ErrorInfo of reason [Reason], which [Refused] detects.
func UnaryServerInterceptor(start *time.Time, open ...string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if Started(start, time.Now()) && !isOpen(info.FullMethod, open) {
			return nil, refusal.Err()
		}

		return handler(ctx, req)
	}
}

// StreamServerInterceptor is the streaming counterpart of [UnaryServerInterceptor].
func StreamServerInterceptor(start *time.Time, open ...string) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if Started(start, time.Now()) && !isOpen(info.FullMethod, open) {
			return refusal.Err()
		}

		return handler(srv, stream)
	}
}

// Refused reports whether err, or an error it wraps, is a call refused by a downtime interceptor.
func Refused(err error) bool {
	return slices.ContainsFunc(status.Convert(err).Details(), func(detail any) bool {
		info, ok := detail.(*errdetails.ErrorInfo)

		return ok && info.GetReason() == Reason
	})
}
