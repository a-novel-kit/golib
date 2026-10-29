package grpcf

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/tap"
)

// MaxConcurrentCalls returns a server option that refuses a call while limit others run across all
// connections, so the memory their requests hold stays bounded. A limit of zero or less bounds
// nothing. The health service stays open, so probes still reach a busy server.
//
// The server refuses a call from its headers, before reading the request, with Unavailable: the
// client may retry it, here or on another instance. The refusal reaches no interceptor, so only the
// client records it. A call holds its slot until it ends or its client cancels it, and a stream
// holds one for as long as it stays open.
//
// Pass it to [NewServer] as an extra option. It sets the server's tap handle, of which gRPC allows
// one.
func MaxConcurrentCalls(limit int) grpc.ServerOption {
	if limit <= 0 {
		return grpc.EmptyServerOption{}
	}

	slots := make(chan struct{}, limit)

	return grpc.InTapHandle(func(ctx context.Context, info *tap.Info) (context.Context, error) {
		if strings.HasPrefix(info.FullMethodName, healthMethods) {
			return ctx, nil
		}

		select {
		case slots <- struct{}{}:
		default:
			return ctx, status.Error(codes.Unavailable, "server at its concurrent call limit: retry later")
		}

		// The transport cancels the call's context once the call ends, however it ends.
		context.AfterFunc(ctx, func() { <-slots })

		return ctx, nil
	})
}
