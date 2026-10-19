// Package interceptors holds the server-side gRPC interceptor chain that
// keeps this gateway alive in production: panic recovery, a request
// deadline ceiling, and structured access logging.
package interceptors

import (
	"context"
	"log/slog"
	"os"
	"runtime/debug"
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// DefaultRequestTimeout bounds how long any single RPC may run when the
// caller did not set its own deadline.
const DefaultRequestTimeout = 15 * time.Second

// Recovery converts a panic in any handler into codes.Internal.
//
// grpc-go does not recover handler panics: without this, one nil dereference
// in one RPC terminates the whole process and every in-flight request with
// it. The stack goes to the log; the caller gets a bare Internal, because a
// stack trace in an API response is an information leak.
func Recovery() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Error("recovered from panic in gRPC handler",
					"method", info.FullMethod,
					"panic", recovered,
					"stack", string(debug.Stack()))
				err = status.Error(codes.Internal, "internal server error")
				resp = nil
			}
		}()

		return handler(ctx, req)
	}
}

// Timeout applies a ceiling to every RPC. A caller-supplied deadline that is
// already tighter wins; an absent or longer one is capped, so a stalled
// upstream cannot hold a server goroutine indefinitely.
func Timeout(timeout time.Duration) grpc.UnaryServerInterceptor {
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= timeout {
			return handler(ctx, req)
		}

		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		return handler(ctx, req)
	}
}

// TimeoutFromEnv reads REQUEST_TIMEOUT_SECONDS, falling back to
// DefaultRequestTimeout.
func TimeoutFromEnv() time.Duration {
	raw := os.Getenv("REQUEST_TIMEOUT_SECONDS")
	if raw == "" {
		return DefaultRequestTimeout
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return DefaultRequestTimeout
	}

	return time.Duration(seconds) * time.Second
}

// requestIDKey carries the correlation id through the handler context.
type requestIDKey struct{}

// RequestIDFromContext returns the correlation id for this RPC, if any.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// Logging emits one structured line per RPC with its method, gRPC code, and
// latency, and threads the caller's x-request-id through the context so
// downstream logs can be correlated.
func Logging() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		started := time.Now()
		requestID := requestIDFromMetadata(ctx)
		if requestID != "" {
			ctx = context.WithValue(ctx, requestIDKey{}, requestID)
		}

		resp, err := handler(ctx, req)

		attrs := []any{
			"method", info.FullMethod,
			"code", status.Code(err).String(),
			"duration_ms", time.Since(started).Milliseconds(),
		}
		if requestID != "" {
			attrs = append(attrs, "request_id", requestID)
		}

		switch code := status.Code(err); {
		case code == codes.OK:
			slog.Info("rpc handled", attrs...)
		case code == codes.Internal || code == codes.Unknown || code == codes.Unavailable || code == codes.DataLoss:
			// Only server-fault codes carry the error text; a client's own
			// 4xx-equivalent is not an operational event worth alerting on.
			slog.Error("rpc failed", append(attrs, "error", err.Error())...)
		default:
			slog.Warn("rpc rejected", attrs...)
		}

		return resp, err
	}
}

// requestIDFromMetadata reads the correlation id an upstream proxy set.
// grpc-gateway forwards non-permanent HTTP headers with a "grpcgateway-"
// prefix, so both spellings are checked.
func requestIDFromMetadata(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	for _, key := range []string{"x-request-id", "grpcgateway-x-request-id"} {
		if values := md.Get(key); len(values) > 0 && values[0] != "" {
			return values[0]
		}
	}

	return ""
}
