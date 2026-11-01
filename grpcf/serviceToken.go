package grpcf

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/a-novel-kit/jwt/v2"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
	"github.com/a-novel-kit/golib/servicetoken"
)

// healthMethods prefixes the methods of the health service, which the kubelet probes without a
// token.
var healthMethods = "/" + healthpb.Health_ServiceDesc.ServiceName + "/"

// ServiceTokenUnaryInterceptor refuses a unary call unless verifier accepts its one service token,
// which a client sends through [ServiceTokenCredentialsProvider]. The health service stays open.
//
// A refused token answers Unauthenticated, with a message that tells the caller why. A verifier
// fault, such as an issuer it cannot reach, answers Unavailable as a [Fault].
//
// Pass it to [NewServer] through grpc.ChainUnaryInterceptor, so it runs within the standard
// interceptors.
func ServiceTokenUnaryInterceptor(verifier *servicetoken.Verifier) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		err := checkServiceToken(ctx, verifier, info.FullMethod)
		if err != nil {
			return nil, err
		}

		return handler(ctx, req)
	}
}

// ServiceTokenStreamInterceptor is the streaming counterpart of [ServiceTokenUnaryInterceptor].
func ServiceTokenStreamInterceptor(verifier *servicetoken.Verifier) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		err := checkServiceToken(stream.Context(), verifier, info.FullMethod)
		if err != nil {
			return err
		}

		return handler(srv, stream)
	}
}

// checkServiceToken returns the status that refuses a call to method, or nil to let it through.
func checkServiceToken(ctx context.Context, verifier *servicetoken.Verifier, method string) error {
	if strings.HasPrefix(method, healthMethods) {
		return nil
	}

	tokens := metadata.ValueFromIncomingContext(ctx, servicetoken.Header)
	if len(tokens) != 1 {
		return status.Errorf(codes.Unauthenticated, "expected one %s header", servicetoken.Header)
	}

	_, err := verifier.Verify(ctx, tokens[0])

	switch {
	case errors.Is(err, jwt.ErrTokenRejected):
		return status.Error(codes.Unauthenticated, err.Error())
	case err != nil:
		return Fault(codes.Unavailable, "service token unverified", otel.ReportError(trace.SpanFromContext(ctx), err))
	}

	return nil
}
