package downtime

import (
	"context"
	"strings"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/protoadapt"
	"google.golang.org/protobuf/types/known/durationpb"
)

// Reason is the ErrorInfo reason of a gRPC call refused during a planned downtime.
const Reason = "PLANNED_DOWNTIME"

// UnaryServerInterceptor refuses unary calls to service while window is in progress, except for
// methods starting with one of open, such as "/grpc.health.v1.Health/". A refused call returns
// codes.Unavailable with an ErrorInfo holding the window, which [FromError] reads, and a RetryInfo
// while the window's end is ahead.
func UnaryServerInterceptor(window *Window, service string, open ...string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		err := refuse(window, service, info.FullMethod, open)
		if err != nil {
			return nil, err
		}

		return handler(ctx, req)
	}
}

// StreamServerInterceptor is the streaming counterpart of [UnaryServerInterceptor].
func StreamServerInterceptor(window *Window, service string, open ...string) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		err := refuse(window, service, info.FullMethod, open)
		if err != nil {
			return err
		}

		return handler(srv, stream)
	}
}

// FromError returns the window behind a call refused by a downtime interceptor, or nil when err
// is not such a refusal.
func FromError(err error) *Window {
	for _, detail := range status.Convert(err).Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if !ok || info.GetReason() != Reason {
			continue
		}

		start, startErr := time.Parse(time.RFC3339, info.GetMetadata()["start"])
		end, endErr := time.Parse(time.RFC3339, info.GetMetadata()["end"])

		if startErr != nil || endErr != nil {
			return nil
		}

		return &Window{Services: strings.Split(info.GetMetadata()["services"], ","), Start: start, End: end}
	}

	return nil
}

// refuse returns the status of a call refused for window, or nil when the call proceeds.
func refuse(window *Window, service, method string, open []string) error {
	now := time.Now()
	if !window.InProgress(service, now) || isOpen(method, open) {
		return nil
	}

	refusal := status.New(codes.Unavailable, "planned downtime")
	info := &errdetails.ErrorInfo{
		Reason: Reason,
		Domain: service,
		Metadata: map[string]string{
			"services": strings.Join(window.Services, ","),
			"start":    window.Start.UTC().Format(time.RFC3339),
			"end":      window.End.UTC().Format(time.RFC3339),
		},
	}

	details := []protoadapt.MessageV1{info}
	if window.End.After(now) {
		details = append(details, &errdetails.RetryInfo{RetryDelay: durationpb.New(window.End.Sub(now))})
	}

	detailed, err := refusal.WithDetails(details...)
	// Details only help clients; without them the call is still refused.
	if err != nil {
		return refusal.Err()
	}

	return detailed.Err()
}
