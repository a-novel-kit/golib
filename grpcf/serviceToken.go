package grpcf

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/a-novel-kit/jwt/v2"

	"go.opentelemetry.io/otel/trace"

	"github.com/a-novel-kit/golib/otel"
	"github.com/a-novel-kit/golib/servicetoken"
)

// ServiceTokenGate returns the server options that refuse a call unless its one service token
// matches config. The health service stays open for the kubelet's probes, and client fetches the
// issuer's keys, such as a client from httpf.NewPoolClient. Pass the options to [NewServer] as extra
// options.
//
// A refused token answers Unauthenticated, with a message that tells the caller why. A verifier
// fault, such as an issuer it cannot reach, answers Unavailable as a [Fault].
//
// The gate fails closed: without an issuer it returns [servicetoken.ErrIssuerRequired], unless
// config.IssuerOptional lets every call through, with no option.
func ServiceTokenGate(config servicetoken.Config, client *http.Client) ([]grpc.ServerOption, error) {
	if config.Issuer == "" {
		if config.IssuerOptional {
			return nil, nil
		}

		return nil, servicetoken.ErrIssuerRequired
	}

	verifier := servicetoken.NewVerifier(config, client)

	return []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(ServiceTokenUnaryInterceptor(verifier)),
		grpc.ChainStreamInterceptor(ServiceTokenStreamInterceptor(verifier)),
	}, nil
}

// ServiceTokenUnaryInterceptor refuses a unary call unless verifier accepts its one service token,
// which a client sends through [ClientConfig.DialOptions]. The health service stays open.
//
// A refused token answers Unauthenticated, with a message that tells the caller why. A verifier
// fault, such as an issuer it cannot reach, answers Unavailable as a [Fault].
//
// Pass it to [NewServer] through grpc.ChainUnaryInterceptor, so it runs within the standard
// interceptors.
//
// Prefer [ServiceTokenGate], which builds the verifier and fails closed without an issuer.
// A later release removes it once no consumer uses it.
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
//
// Prefer [ServiceTokenGate], which builds the verifier and fails closed without an issuer.
// A later release removes it once no consumer uses it.
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
