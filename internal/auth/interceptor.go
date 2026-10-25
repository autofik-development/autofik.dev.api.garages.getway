package auth

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	reflectionv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	reflectionv1alpha "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/grpc/status"

	pb "github.com/autofik-development/autofik.dev.api.garages.getway/internal/grpc/pb/garages/v1"
)

// publicMethods answer without a bearer token: a health probe - k8s, a load
// balancer, grpcurl, the REST /garages/v1/health route - has none to send.
// Every other method is gated, including any added to the proto later, so a
// method missing here is locked, never open.
var publicMethods = map[string]bool{
	pb.GaragesGatewayService_HealthCheck_FullMethodName: true,
	healthpb.Health_Check_FullMethodName:                true,
	healthpb.Health_Watch_FullMethodName:                true,
	// Reflection is registered only when GRPC_ENABLE_REFLECTION is set, as a
	// local debugging aid. Gating it would make grpcurl need a garage token
	// just to list the services.
	reflectionv1.ServerReflection_ServerReflectionInfo_FullMethodName:      true,
	reflectionv1alpha.ServerReflection_ServerReflectionInfo_FullMethodName: true,
}

// UnaryInterceptor is the gate every unary RPC passes through: no verified
// garage, no handler. A handler reads the garage with GarageFromContext.
//
// It belongs inside interceptors.Timeout, so verification spends from the
// RPC's own deadline and its retry can see what is left of it.
func UnaryInterceptor(verifier Verifier) grpc.UnaryServerInterceptor {
	if verifier == nil {
		// A gateway wired without a verifier must not start, rather than
		// let every caller in.
		panic("auth: UnaryInterceptor needs a verifier")
	}

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if publicMethods[info.FullMethod] {
			return handler(ctx, req)
		}

		garage, err := authenticate(ctx, verifier)
		if err != nil {
			return nil, err
		}
		return handler(newContext(ctx, garage), req)
	}
}

// StreamInterceptor is the same gate for streaming RPCs. GaragesGatewayService
// has none today; this is what keeps one added later from running ungated. A
// stream carries its context on the ServerStream, so the verified one is
// wrapped back onto it.
func StreamInterceptor(verifier Verifier) grpc.StreamServerInterceptor {
	if verifier == nil {
		panic("auth: StreamInterceptor needs a verifier")
	}

	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if publicMethods[info.FullMethod] {
			return handler(srv, ss)
		}

		garage, err := authenticate(ss.Context(), verifier)
		if err != nil {
			return err
		}
		return handler(srv, &verifiedStream{ServerStream: ss, ctx: newContext(ss.Context(), garage)})
	}
}

type verifiedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *verifiedStream) Context() context.Context { return s.ctx }

// authenticate resolves the caller of one RPC, or returns the gRPC error to
// send back. The bearer and the service-code are checked before SSO is
// asked, so a request missing either costs no SSO call.
func authenticate(ctx context.Context, verifier Verifier) (VerifiedGarage, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	// One value each, or the request is refused: with two, which one SSO is
	// asked about would come down to header order.
	authorization := md.Get("authorization")
	if len(authorization) == 0 {
		return VerifiedGarage{}, status.Error(codes.Unauthenticated, "missing authorization")
	}
	if len(authorization) > 1 {
		return VerifiedGarage{}, status.Error(codes.Unauthenticated, "more than one authorization header")
	}
	token := BearerToken(authorization[0])
	if token == "" {
		return VerifiedGarage{}, status.Error(codes.Unauthenticated, "empty bearer token")
	}

	serviceCodes := md.Get(ServiceCodeHeader)
	if len(serviceCodes) > 1 {
		return VerifiedGarage{}, status.Error(codes.InvalidArgument, "more than one service-code header")
	}
	// Required, as on the SOS gateway: SSO checks the token against the app
	// it was issued to, and only the caller knows which that is.
	if len(serviceCodes) == 0 || strings.TrimSpace(serviceCodes[0]) == "" {
		return VerifiedGarage{}, status.Error(codes.InvalidArgument, "missing service-code header")
	}

	garage, err := verifier.VerifyToken(ctx, token, strings.TrimSpace(serviceCodes[0]))
	if err != nil {
		return VerifiedGarage{}, verifyError(ctx, err)
	}
	return garage, nil
}

// verifyError turns a VerifyToken error into the status the caller sees: SSO
// said no (Unauthenticated), the account is not verified yet
// (PermissionDenied), the service-code is not the account's (InvalidArgument,
// SSO's own 400), or verification did not complete (Unavailable - the caller
// could not be proven wrong, and nothing ran). The messages are fixed: the
// error itself names SSO's status and how far the call got, which belongs in
// the "sso verify" log line, not in a response.
//
// A verification cut short because the RPC itself ended is the RPC's
// outcome instead, as from the CMS client: a caller that hung up gets
// Canceled and one whose deadline passed DeadlineExceeded, so neither is
// logged as an SSO failure.
func verifyError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		return status.Error(codes.Unauthenticated, "sso: unauthenticated")
	case errors.Is(err, ErrNotVerified):
		return status.Error(codes.PermissionDenied, "garage profile is not fully verified")
	case errors.Is(err, ErrInvalidServiceCode):
		return status.Error(codes.InvalidArgument, "sso: invalid service code")
	case errors.Is(ctx.Err(), context.Canceled):
		return status.Error(codes.Canceled, "request cancelled before sso answered")
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "request deadline exceeded before sso answered")
	default:
		return status.Error(codes.Unavailable, "sso: verification unavailable")
	}
}
