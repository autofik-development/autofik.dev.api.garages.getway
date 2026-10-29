package test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/auth"
	pb "github.com/autofik-development/autofik.dev.api.garages.getway/internal/grpc/pb/garages/v1"
)

// TestEveryRPCRequiresABearer calls every method of the service with no
// credentials. Unauthenticated is the only acceptable answer, except from
// HealthCheck: the gate is fail-closed, so a method added to the proto later
// is covered without anyone having to remember it.
func TestEveryRPCRequiresABearer(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(`[]`))
	sso := newFakeSSO(t, ssoAccepting(t))
	gw := newTestGatewayWithSSO(t, cms.URL+"/graphql", sso.verifyURL())

	desc := pb.GaragesGatewayService_ServiceDesc
	for _, method := range desc.Methods {
		fullMethod := "/" + desc.ServiceName + "/" + method.MethodName
		if fullMethod == pb.GaragesGatewayService_HealthCheck_FullMethodName {
			continue // TestHealthNeedsNoCredentials
		}
		t.Run(method.MethodName, func(t *testing.T) {
			// An empty message decodes as any request type, and the gate
			// answers before a handler would read it.
			err := gw.anon.Invoke(context.Background(), fullMethod, &emptypb.Empty{}, &emptypb.Empty{})
			if got := status.Code(err); got != codes.Unauthenticated {
				t.Errorf("code = %v, want Unauthenticated (err=%v)", got, err)
			}
		})
	}

	assertNoCMSCall(t, calls)
	if n := len(sso.calls()); n != 0 {
		t.Errorf("SSO received %d requests for callers with no bearer, want none", n)
	}
}

// TestHealthNeedsNoCredentials: a probe (k8s, a load balancer, grpcurl) has
// no token to send.
func TestHealthNeedsNoCredentials(t *testing.T) {
	sso := newFakeSSO(t, ssoAccepting(t))
	gw := newTestGatewayWithSSO(t, "http://cms.invalid/graphql", sso.verifyURL())

	if _, err := pb.NewGaragesGatewayServiceClient(gw.anon).HealthCheck(context.Background(), &pb.HealthCheckRequest{}); err != nil {
		t.Errorf("HealthCheck: %v", err)
	}
	resp, err := healthpb.NewHealthClient(gw.anon).Check(context.Background(), &healthpb.HealthCheckRequest{})
	if err != nil || resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		t.Errorf("Health/Check = %v, %v; want SERVING", resp.GetStatus(), err)
	}

	noCredentials := http.Header{"Authorization": nil, "Service-Code": nil}
	if status, body := restJSONWithHeader(t, http.MethodGet, gw.rest.URL+"/garages/v1/health", "", noCredentials); status != http.StatusOK {
		t.Errorf("GET /garages/v1/health = %d, want 200; body %s", status, body)
	}

	if n := len(sso.calls()); n != 0 {
		t.Errorf("SSO received %d requests for health checks, want none", n)
	}
}

// TestRESTCredentialErrors pins what a caller with missing or malformed
// credentials gets. None of them costs an SSO call or reaches the CMS.
func TestRESTCredentialErrors(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(`[]`))
	sso := newFakeSSO(t, ssoAccepting(t))
	gw := newTestGatewayWithSSO(t, cms.URL+"/graphql", sso.verifyURL())

	cases := []struct {
		name   string
		header http.Header
		status int
		want   string
	}{
		{"no authorization", http.Header{"Authorization": nil},
			http.StatusUnauthorized, `{"status": "error", "code": "UNAUTHENTICATED", "message": "missing authorization"}`},
		{"bearer with no token", http.Header{"Authorization": {"Bearer "}},
			http.StatusUnauthorized, `{"status": "error", "code": "UNAUTHENTICATED", "message": "empty bearer token"}`},
		{"two authorization headers", http.Header{"Authorization": {"Bearer " + garageToken, "Bearer another-token"}},
			http.StatusUnauthorized, `{"status": "error", "code": "UNAUTHENTICATED", "message": "more than one authorization header"}`},
		{"no service-code", http.Header{"Service-Code": nil},
			http.StatusBadRequest, `{"status": "error", "code": "INVALID_ARGUMENT", "message": "missing service-code header"}`},
		{"blank service-code", http.Header{"Service-Code": {"  "}},
			http.StatusBadRequest, `{"status": "error", "code": "INVALID_ARGUMENT", "message": "missing service-code header"}`},
		{"two service-codes", http.Header{"Service-Code": {garageServiceCode, "booking"}},
			http.StatusBadRequest, `{"status": "error", "code": "INVALID_ARGUMENT", "message": "more than one service-code header"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := restJSONWithHeader(t, http.MethodGet, gw.rest.URL+"/garages/v1/garages", "", tc.header)
			if status != tc.status {
				t.Fatalf("status = %d, want %d; body %s", status, tc.status, body)
			}
			assertJSONEqual(t, body, tc.want)
		})
	}

	assertNoCMSCall(t, calls)
	if n := len(sso.calls()); n != 0 {
		t.Errorf("SSO received %d requests, want none", n)
	}
}

// TestRESTSSOAnswers runs each answer SSO's verify-token can give through the
// gateway. A definite no is the caller's (401, 403 or 400); only an SSO that
// could not answer is the gateway's (503); and either way nothing reaches the
// CMS.
func TestRESTSSOAnswers(t *testing.T) {
	const (
		unauthenticated    = `{"status": "error", "code": "UNAUTHENTICATED", "message": "sso: unauthenticated"}`
		notVerified        = `{"status": "error", "code": "PERMISSION_DENIED", "message": "garage profile is not fully verified"}`
		invalidServiceCode = `{"status": "error", "code": "INVALID_ARGUMENT", "message": "sso: invalid service code"}`
		unavailable        = `{"status": "error", "code": "SERVICE_UNAVAILABLE", "message": "sso: verification unavailable"}`
	)
	loggedOut := map[string]any{"isLogout": true, "TokenStatus": "unauth"}

	cases := []struct {
		name   string
		sso    http.HandlerFunc
		status int
		want   string
	}{
		{"bad or expired token",
			ssoAnswering(http.StatusUnauthorized, `{"status": "failure", "message": "Invalid or expired token", "code": "INVALID_TOKEN"}`),
			http.StatusUnauthorized, unauthenticated},
		{"account with no sessions",
			ssoAnswering(http.StatusNotFound, `{"status": "failure", "message": "Verify token not found", "code": "NOT_FOUND", "state": "garage_verify_token_list"}`),
			http.StatusUnauthorized, unauthenticated},
		{"refusal with no body",
			ssoAnswering(http.StatusUnauthorized, ``),
			http.StatusUnauthorized, unauthenticated},
		// SSO's own "garage not complete profile". A 401 from SSO, but
		// logging in again would not fix it, so the caller is told so.
		{"profile not complete",
			ssoAnswering(http.StatusUnauthorized, `{"status": "failure", "message": "garage not complete profile", "code": "unauthorized", "state": "garage_verify_token_list"}`),
			http.StatusForbidden, notVerified},
		{"another app's service-code",
			ssoAnswering(http.StatusBadRequest, `{"status": "failure", "message": "Invalid service code", "code": "INVALID_SERVICE_CODE", "state": "garage_verify_token_list"}`),
			http.StatusBadRequest, invalidServiceCode},
		{"every session logged out",
			ssoAnswering(http.StatusOK, ssoSessions(t, loggedOut, loggedOut)),
			http.StatusUnauthorized, unauthenticated},
		{"unverified session",
			ssoAnswering(http.StatusOK, ssoSessions(t, map[string]any{"profileVerify": false})),
			http.StatusForbidden, notVerified},
		{"sso error",
			ssoAnswering(http.StatusInternalServerError, `{"status": "error", "message": "boom", "code": "INTERNAL_SERVER_ERROR"}`),
			http.StatusServiceUnavailable, unavailable},
		{"sso rate limited",
			ssoAnswering(http.StatusTooManyRequests, `{"message": "API rate limit exceeded"}`),
			http.StatusServiceUnavailable, unavailable},
		{"unreadable body",
			ssoAnswering(http.StatusOK, `<html>bad gateway</html>`),
			http.StatusServiceUnavailable, unavailable},
		{"no sessions in a 200",
			ssoAnswering(http.StatusOK, `{"status": "success", "data": []}`),
			http.StatusServiceUnavailable, unavailable},
		{"session without its verification flags",
			ssoAnswering(http.StatusOK, ssoSessions(t, map[string]any{"otpVerify": nil})),
			http.StatusServiceUnavailable, unavailable},
		{"session without isLogout",
			ssoAnswering(http.StatusOK, ssoSessions(t, map[string]any{}, map[string]any{"isLogout": nil})),
			http.StatusServiceUnavailable, unavailable},
		{"session without uuid",
			ssoAnswering(http.StatusOK, ssoSessions(t, map[string]any{"uuid": nil})),
			http.StatusServiceUnavailable, unavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cms, calls := fakeCMS(t, http.StatusOK, cmsResult(`[]`))
			sso := newFakeSSO(t, tc.sso)
			gw := newTestGatewayWithSSO(t, cms.URL+"/graphql", sso.verifyURL())

			status, body := restJSON(t, http.MethodGet, gw.rest.URL+"/garages/v1/garages", "")
			if status != tc.status {
				t.Fatalf("status = %d, want %d; body %s", status, tc.status, body)
			}
			assertJSONEqual(t, body, tc.want)
			assertNoCMSCall(t, calls)
		})
	}
}

// TestRESTSSOUnreachable: SSO going away under a running gateway fails
// closed, as 503 - the caller was never proven wrong - not 401.
func TestRESTSSOUnreachable(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(`[]`))
	sso := newFakeSSO(t, ssoAccepting(t))
	gw := newTestGatewayWithSSO(t, cms.URL+"/graphql", sso.verifyURL())
	sso.Close()

	status, body := restJSON(t, http.MethodGet, gw.rest.URL+"/garages/v1/garages", "")
	if status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", status, body)
	}
	assertJSONEqual(t, body, `{"status": "error", "code": "SERVICE_UNAVAILABLE", "message": "sso: verification unavailable"}`)
	assertNoCMSCall(t, calls)
}

// TestRESTCredentialsReachSSO: grpc-gateway drops headers it does not know,
// so service-code needs the gateway's own header matcher to reach SSO at all.
// The request id goes along for correlation.
func TestRESTCredentialsReachSSO(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(`[]`))
	sso := newFakeSSO(t, ssoAccepting(t))
	gw := newTestGatewayWithSSO(t, cms.URL+"/graphql", sso.verifyURL())

	status, body := restJSONWithHeader(t, http.MethodGet, gw.rest.URL+"/garages/v1/garages", "", http.Header{
		"X-Request-Id": {"req-auth-1"},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", status, body)
	}
	receivedCMSCall(t, calls, "Garages")

	ssoCalls := sso.calls()
	if len(ssoCalls) != 1 {
		t.Fatalf("SSO received %d requests, want 1", len(ssoCalls))
	}
	want := map[string]string{
		"Authorization": "Bearer " + garageToken,
		"Service-Code":  garageServiceCode,
		"X-Request-Id":  "req-auth-1",
	}
	for name, value := range want {
		if got := ssoCalls[0].Get(name); got != value {
			t.Errorf("SSO %s = %q, want %q", name, got, value)
		}
	}

	t.Run("another app's code", func(t *testing.T) {
		status, body := restJSONWithHeader(t, http.MethodGet, gw.rest.URL+"/garages/v1/garages", "", http.Header{
			"Service-Code": {"booking"},
		})
		if status != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body %s", status, body)
		}
		assertJSONEqual(t, body, `{"status": "error", "code": "INVALID_ARGUMENT", "message": "sso: invalid service code"}`)
		assertNoCMSCall(t, calls)
	})
}

// TestGRPCCredentials: on the gRPC path the scheme is matched in any case, as
// SSO matches it, and the bearer goes to SSO under one canonical Bearer.
func TestGRPCCredentials(t *testing.T) {
	cms, calls := fakeCMS(t, http.StatusOK, cmsResult(`[]`))
	sso := newFakeSSO(t, ssoAccepting(t))
	gw := newTestGatewayWithSSO(t, cms.URL+"/graphql", sso.verifyURL())
	client := pb.NewGaragesGatewayServiceClient(gw.anon)

	ctx := metadata.AppendToOutgoingContext(context.Background(),
		"authorization", "bearer "+garageToken, "service-code", " "+garageServiceCode+" ")
	if _, err := client.ListGarages(ctx, &pb.ListGaragesRequest{}); err != nil {
		t.Fatalf("ListGarages: %v", err)
	}
	receivedCMSCall(t, calls, "Garages")

	ssoCalls := sso.calls()
	if len(ssoCalls) != 1 {
		t.Fatalf("SSO received %d requests, want 1", len(ssoCalls))
	}
	if got := ssoCalls[0].Get("Authorization"); got != "Bearer "+garageToken {
		t.Errorf("SSO Authorization = %q, want the token under one Bearer", got)
	}
	if got := ssoCalls[0].Get("Service-Code"); got != garageServiceCode {
		t.Errorf("SSO service-code = %q, want %q", got, garageServiceCode)
	}

	t.Run("wrong token", func(t *testing.T) {
		ctx := metadata.AppendToOutgoingContext(context.Background(),
			"authorization", "Bearer forged", "service-code", garageServiceCode)
		_, err := client.ListGarages(ctx, &pb.ListGaragesRequest{})
		if got := status.Code(err); got != codes.Unauthenticated {
			t.Errorf("code = %v, want Unauthenticated (err=%v)", got, err)
		}
		assertNoCMSCall(t, calls)
	})
}

func newTestVerifier(sso *fakeSSO, timeout time.Duration) *auth.HTTPVerifier {
	return auth.NewHTTPVerifier(auth.Config{VerifyURL: sso.verifyURL(), VerifyTimeout: timeout}, nil)
}

// TestVerifierResolvesTheLoggedInSession: SSO lists every session of the
// account, in no set order, a logged-out device's included. The garage
// resolves from one that is still logged in.
func TestVerifierResolvesTheLoggedInSession(t *testing.T) {
	sso := newFakeSSO(t, ssoAccepting(t))

	garage, err := newTestVerifier(sso, time.Second).VerifyToken(context.Background(), garageToken, garageServiceCode)
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}

	want := auth.VerifiedGarage{
		UUID:            ssoGarageUUID,
		GarageAccountID: ssoGarageAccountID,
		DeviceID:        ssoDeviceID,
		ServiceCode:     garageServiceCode,
	}
	if garage != want {
		t.Errorf("garage = %+v, want %+v", garage, want)
	}
}

// hangOnce is an SSO whose first verify-token call never gets an answer; the
// calls after it are answered by next.
func hangOnce(next http.HandlerFunc) http.HandlerFunc {
	var calls atomic.Int32
	return func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			<-r.Context().Done()
			return
		}
		next(w, r)
	}
}

// TestVerifierRetriesOnceWithoutAnAnswer: one stall past the per-call limit
// gets a second attempt when the RPC's deadline has room for it, instead of
// failing the request with most of its budget unused.
func TestVerifierRetriesOnceWithoutAnAnswer(t *testing.T) {
	sso := newFakeSSO(t, hangOnce(ssoAccepting(t)))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := newTestVerifier(sso, 200*time.Millisecond).VerifyToken(ctx, garageToken, garageServiceCode); err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if n := len(sso.calls()); n != 2 {
		t.Errorf("SSO received %d requests, want 2", n)
	}
}

// TestVerifierDoesNotRetry: a stall is final when the deadline has no room
// for a whole second attempt, or when there is no deadline to judge by; and
// an SSO that answered is never asked twice.
func TestVerifierDoesNotRetry(t *testing.T) {
	cases := []struct {
		name     string
		sso      http.HandlerFunc
		deadline time.Duration // 0: none
	}{
		{"no room in the deadline", hangOnce(ssoAccepting(t)), 300 * time.Millisecond},
		{"no deadline", hangOnce(ssoAccepting(t)), 0},
		{"sso answered", ssoAnswering(http.StatusBadGateway, ``), 5 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sso := newFakeSSO(t, tc.sso)

			ctx := context.Background()
			if tc.deadline > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.deadline)
				defer cancel()
			}
			_, err := newTestVerifier(sso, 200*time.Millisecond).VerifyToken(ctx, garageToken, garageServiceCode)
			if !errors.Is(err, auth.ErrUnavailable) {
				t.Errorf("err = %v, want ErrUnavailable", err)
			}
			if n := len(sso.calls()); n != 1 {
				t.Errorf("SSO received %d requests, want 1", n)
			}
		})
	}
}

// verifierFunc adapts a function to auth.Verifier, to drive the interceptors
// directly.
type verifierFunc func(ctx context.Context, token, serviceCode string) (auth.VerifiedGarage, error)

func (f verifierFunc) VerifyToken(ctx context.Context, token, serviceCode string) (auth.VerifiedGarage, error) {
	return f(ctx, token, serviceCode)
}

func credentialsContext(ctx context.Context, pairs ...string) context.Context {
	return metadata.NewIncomingContext(ctx, metadata.Pairs(pairs...))
}

// TestInterceptorPutsTheGarageInTheContext: what the handler reads is what
// SSO verified, and SSO is asked about the bearer without its scheme and the
// service-code without its padding.
func TestInterceptorPutsTheGarageInTheContext(t *testing.T) {
	want := auth.VerifiedGarage{UUID: ssoGarageUUID, GarageAccountID: ssoGarageAccountID, DeviceID: ssoDeviceID, ServiceCode: garageServiceCode}
	var asked string
	verifier := verifierFunc(func(_ context.Context, token, serviceCode string) (auth.VerifiedGarage, error) {
		asked = token + "|" + serviceCode
		return want, nil
	})

	ctx := credentialsContext(context.Background(), "authorization", "BEARER "+garageToken, "service-code", " "+garageServiceCode+" ")
	info := &grpc.UnaryServerInfo{FullMethod: pb.GaragesGatewayService_ListGarages_FullMethodName}

	var got auth.VerifiedGarage
	var ok bool
	_, err := auth.UnaryInterceptor(verifier)(ctx, nil, info, func(ctx context.Context, _ interface{}) (interface{}, error) {
		got, ok = auth.GarageFromContext(ctx)
		return nil, nil
	})
	if err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if !ok || got != want {
		t.Errorf("GarageFromContext = %+v, %v; want %+v, true", got, ok, want)
	}
	if asked != garageToken+"|"+garageServiceCode {
		t.Errorf("verifier asked about %q, want %q", asked, garageToken+"|"+garageServiceCode)
	}

	if _, ok := auth.GarageFromContext(context.Background()); ok {
		t.Error("GarageFromContext found a garage in a context the gate never saw")
	}
}

// TestVerificationCutShortByTheRPC: a verification the RPC itself ended is the
// RPC's outcome - Canceled for a caller that hung up, DeadlineExceeded for one
// whose deadline passed - not Unavailable, which is logged as an SSO failure.
func TestVerificationCutShortByTheRPC(t *testing.T) {
	waitForTheRPC := verifierFunc(func(ctx context.Context, _, _ string) (auth.VerifiedGarage, error) {
		<-ctx.Done()
		return auth.VerifiedGarage{}, fmt.Errorf("%w: verify-token: %w", auth.ErrUnavailable, ctx.Err())
	})
	intercept := auth.UnaryInterceptor(waitForTheRPC)
	info := &grpc.UnaryServerInfo{FullMethod: pb.GaragesGatewayService_ListGarages_FullMethodName}
	handler := func(context.Context, interface{}) (interface{}, error) {
		t.Error("handler ran for an unverified caller")
		return nil, nil
	}

	deadlined, cancelDeadline := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelDeadline()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	for name, tc := range map[string]struct {
		ctx  context.Context
		want codes.Code
	}{
		"deadline": {deadlined, codes.DeadlineExceeded},
		"canceled": {cancelled, codes.Canceled},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := credentialsContext(tc.ctx, "authorization", "Bearer "+garageToken, "service-code", garageServiceCode)
			_, err := intercept(ctx, nil, info, handler)
			if got := status.Code(err); got != tc.want {
				t.Errorf("code = %v, want %v (err=%v)", got, tc.want, err)
			}
		})
	}
}

// fakeServerStream is the part of a grpc.ServerStream the interceptor uses.
type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *fakeServerStream) Context() context.Context { return s.ctx }

// TestStreamInterceptor: the service has no streaming RPC today, so the
// stream gate is driven directly - it is what keeps one added later from
// running unverified.
func TestStreamInterceptor(t *testing.T) {
	want := auth.VerifiedGarage{UUID: ssoGarageUUID, ServiceCode: garageServiceCode}
	var verifications int
	intercept := auth.StreamInterceptor(verifierFunc(func(context.Context, string, string) (auth.VerifiedGarage, error) {
		verifications++
		return want, nil
	}))
	watch := &grpc.StreamServerInfo{FullMethod: "/garages.v1.GaragesGatewayService/WatchGarage", IsServerStream: true}

	t.Run("no credentials", func(t *testing.T) {
		err := intercept(nil, &fakeServerStream{ctx: context.Background()}, watch, func(interface{}, grpc.ServerStream) error {
			t.Error("handler ran for an unverified caller")
			return nil
		})
		if got := status.Code(err); got != codes.Unauthenticated {
			t.Errorf("code = %v, want Unauthenticated (err=%v)", got, err)
		}
	})

	t.Run("verified", func(t *testing.T) {
		ctx := credentialsContext(context.Background(), "authorization", "Bearer "+garageToken, "service-code", garageServiceCode)
		err := intercept(nil, &fakeServerStream{ctx: ctx}, watch, func(_ interface{}, ss grpc.ServerStream) error {
			if got, ok := auth.GarageFromContext(ss.Context()); !ok || got != want {
				t.Errorf("GarageFromContext = %+v, %v; want %+v, true", got, ok, want)
			}
			return nil
		})
		if err != nil {
			t.Errorf("interceptor: %v", err)
		}
	})

	t.Run("health watch is public", func(t *testing.T) {
		before := verifications
		ran := false
		health := &grpc.StreamServerInfo{FullMethod: healthpb.Health_Watch_FullMethodName, IsServerStream: true}
		err := intercept(nil, &fakeServerStream{ctx: context.Background()}, health, func(interface{}, grpc.ServerStream) error {
			ran = true
			return nil
		})
		if err != nil || !ran {
			t.Errorf("Health/Watch: ran=%v err=%v, want it to run without credentials", ran, err)
		}
		if verifications != before {
			t.Error("Health/Watch asked the verifier")
		}
	})
}

func TestBearerToken(t *testing.T) {
	cases := map[string]string{
		"Bearer abc":   "abc",
		"bearer abc":   "abc",
		"BEARER  abc ": "abc",
		"Bearer":       "",
		"bearer ":      "",
		"abc":          "abc",
		"":             "",
	}
	for in, want := range cases {
		if got := auth.BearerToken(in); got != want {
			t.Errorf("BearerToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSSOConfigFromEnv(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		want    auth.Config
		wantErr bool
	}{
		{"defaults", nil,
			auth.Config{VerifyURL: "http://localhost:4000/api/garage/verify-token", VerifyTimeout: 3 * time.Second}, false},
		{"base url", map[string]string{auth.EnvSSOGarageServiceURL: " http://sso-garage.sso.svc.cluster.local:80/ "},
			auth.Config{VerifyURL: "http://sso-garage.sso.svc.cluster.local:80/api/garage/verify-token", VerifyTimeout: 3 * time.Second}, false},
		{"verify url wins", map[string]string{
			auth.EnvSSOGarageServiceURL: "http://ignored:4000",
			auth.EnvSSOGarageVerifyURL:  "https://sso-garages.example/api/garage/verify-token",
		}, auth.Config{VerifyURL: "https://sso-garages.example/api/garage/verify-token", VerifyTimeout: 3 * time.Second}, false},
		{"timeout", map[string]string{auth.EnvSSOVerifyTimeout: "4s"},
			auth.Config{VerifyURL: "http://localhost:4000/api/garage/verify-token", VerifyTimeout: 4 * time.Second}, false},
		{"url without a scheme", map[string]string{auth.EnvSSOGarageServiceURL: "sso-garage:4000"}, auth.Config{}, true},
		{"url that is not http", map[string]string{auth.EnvSSOGarageVerifyURL: "ftp://sso/verify-token"}, auth.Config{}, true},
		{"timeout without a unit", map[string]string{auth.EnvSSOVerifyTimeout: "3"}, auth.Config{}, true},
		{"zero timeout", map[string]string{auth.EnvSSOVerifyTimeout: "0s"}, auth.Config{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{auth.EnvSSOGarageServiceURL, auth.EnvSSOGarageVerifyURL, auth.EnvSSOVerifyTimeout} {
				t.Setenv(key, tc.env[key])
			}

			got, err := auth.ConfigFromEnv()
			if tc.wantErr {
				if err == nil {
					t.Errorf("ConfigFromEnv = %+v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ConfigFromEnv: %v", err)
			}
			if got != tc.want {
				t.Errorf("ConfigFromEnv = %+v, want %+v", got, tc.want)
			}
		})
	}
}
