// Package test holds the gateway's integration tests. They exercise the
// service the way a caller does - over a real gRPC connection and, for the
// REST surface, through the real grpc-gateway mux - against a fake CMS that
// speaks GraphQL over HTTP and a fake garage SSO that answers verify-token,
// rather than reaching into unexported internals.
package test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"

	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/auth"
	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/cms"
	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/gateway"
	pb "github.com/autofik-development/autofik.dev.api.garages.getway/internal/grpc/pb/garages/v1"
	service "github.com/autofik-development/autofik.dev.api.garages.getway/internal/grpc/service"
	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/interceptors"
)

// Fixture ids. The gateway checks path ids are uuids, so these must be.
const (
	garageID  = "3b2f5c1e-8d4a-4f6b-9c2e-1a7d5e9f0b3c"
	adminID   = "9a1c2b3d-4e5f-4a6b-8c7d-0e1f2a3b4c5d"
	serviceID = "7c6b5a49-3827-4615-a4b3-c2d1e0f9a8b7"
	staffID   = "1f2e3d4c-5b6a-4978-8695-a4b3c2d1e0f9"
)

// The session the fake SSO knows, as testdata/sso-verify-token.json lists
// it: garageToken is the bearer it accepts, for the account ssoGarageUUID,
// which logged in from garageServiceCode. Every test client sends the token
// and the code unless a test says otherwise, so a test about garages need
// not think about auth.
const (
	garageToken        = "garage-session-token"
	garageServiceCode  = "sso-garage"
	ssoGarageUUID      = "5d0c6a8e-2f4b-4c1d-9e7a-3b8f1c2d4e6f"
	ssoGarageAccountID = "12345678"
	ssoDeviceID        = "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d"
	ssoVerifyPath      = "/api/garage/verify-token"
)

// TestMain silences the gateway's structured access logging.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

type testGateway struct {
	// conn sends garageToken and garageServiceCode on every unary call;
	// anon sends no credentials of its own.
	conn *grpc.ClientConn
	anon *grpc.ClientConn
	rest *httptest.Server
}

// newTestGateway starts the real gRPC server - same interceptor chain and
// health registration as cmd/main.go - on a loopback port, puts the real
// REST mux in front of it, and returns clients for both. Its CMS address is
// unroutable; tests that reach the CMS use newTestGatewayWithCMS.
func newTestGateway(t *testing.T) *testGateway {
	t.Helper()
	return newTestGatewayWithCMS(t, "http://cms.invalid/graphql")
}

// newTestGatewayWithCMS is newTestGateway with its CMS client pointed at
// cmsURL, typically a fakeCMS. Callers are verified by a fake SSO that
// accepts garageToken.
func newTestGatewayWithCMS(t *testing.T, cmsURL string) *testGateway {
	t.Helper()
	return newTestGatewayWithSSO(t, cmsURL, newFakeSSO(t, ssoAccepting(t)).verifyURL())
}

// newTestGatewayWithSSO is newTestGatewayWithCMS with callers verified
// against ssoVerifyURL, typically a fakeSSO's.
func newTestGatewayWithSSO(t *testing.T, cmsURL, ssoVerifyURL string) *testGateway {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	verifier := auth.NewHTTPVerifier(auth.Config{VerifyURL: ssoVerifyURL, VerifyTimeout: 2 * time.Second}, nil)
	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptors.Recovery(),
			interceptors.Logging(),
			interceptors.Timeout(5*time.Second),
			auth.UnaryInterceptor(verifier),
		),
		grpc.StreamInterceptor(auth.StreamInterceptor(verifier)),
	)
	pb.RegisterGaragesGatewayServiceServer(grpcServer, service.NewGaragesGatewayServer(cms.New(cmsURL, nil)))

	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus(service.ServiceName, healthpb.HealthCheckResponse_SERVING)

	go func() { _ = grpcServer.Serve(listener) }()

	ctx, cancel := context.WithCancel(context.Background())
	handler, closeGatewayConn, err := gateway.NewHandler(ctx, listener.Addr().String())
	if err != nil {
		cancel()
		grpcServer.Stop()
		t.Fatalf("NewHandler: %v", err)
	}
	rest := httptest.NewServer(gateway.NewMux(handler))

	conn, err := grpc.NewClient(listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(sendGarageCredentials))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	anon, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
		_ = anon.Close()
		rest.Close()
		_ = closeGatewayConn()
		cancel()
		grpcServer.Stop()
	})

	return &testGateway{conn: conn, anon: anon, rest: rest}
}

// sendGarageCredentials adds the bearer and service-code the fake SSO
// accepts to every unary call made on the connection it is installed on.
func sendGarageCredentials(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ctx = metadata.AppendToOutgoingContext(ctx,
		"authorization", "Bearer "+garageToken,
		auth.ServiceCodeHeader, garageServiceCode)
	return invoker(ctx, method, req, reply, cc, opts...)
}

// fakeSSO stands in for the garage SSO's verify-token: it records the
// headers of every request it gets and answers each one with its handler.
type fakeSSO struct {
	*httptest.Server

	mu      sync.Mutex
	headers []http.Header
}

func newFakeSSO(t *testing.T, handler http.HandlerFunc) *fakeSSO {
	t.Helper()

	sso := &fakeSSO{}
	sso.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != ssoVerifyPath {
			t.Errorf("SSO request = %s %s, want GET %s", r.Method, r.URL.Path, ssoVerifyPath)
		}
		sso.mu.Lock()
		sso.headers = append(sso.headers, r.Header.Clone())
		sso.mu.Unlock()

		handler(w, r)
	}))
	t.Cleanup(sso.Close)

	return sso
}

func (s *fakeSSO) verifyURL() string { return s.URL + ssoVerifyPath }

// calls returns the headers of every request the SSO got, oldest first.
func (s *fakeSSO) calls() []http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]http.Header(nil), s.headers...)
}

// ssoBearer is SSO's own match on Authorization.
var ssoBearer = regexp.MustCompile(`(?i)^Bearer (.+)$`)

// ssoAccepting answers the way the garage SSO's verify-token does for the
// one session it knows: 401 INVALID_TOKEN to any bearer but garageToken, 400
// INVALID_SERVICE_CODE to any service-code but garageServiceCode, and
// otherwise testdata/sso-verify-token.json.
func ssoAccepting(t *testing.T) http.HandlerFunc {
	t.Helper()

	body, err := os.ReadFile("testdata/sso-verify-token.json")
	if err != nil {
		t.Fatalf("read SSO fixture: %v", err)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		match := ssoBearer.FindStringSubmatch(r.Header.Get("Authorization"))
		switch {
		case match == nil || match[1] != garageToken:
			writeSSO(w, http.StatusUnauthorized, `{"status": "failure", "message": "Invalid or expired token", "code": "INVALID_TOKEN"}`)
		case strings.TrimSpace(r.Header.Get(auth.ServiceCodeHeader)) != garageServiceCode:
			writeSSO(w, http.StatusBadRequest, `{"status": "failure", "message": "Invalid service code",
				"code": "INVALID_SERVICE_CODE", "state": "garage_verify_token_list"}`)
		default:
			writeSSO(w, http.StatusOK, string(body))
		}
	}
}

// ssoAnswering answers every verify-token call with httpStatus and body.
func ssoAnswering(httpStatus int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeSSO(w, httpStatus, body)
	}
}

func writeSSO(w http.ResponseWriter, httpStatus int, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(httpStatus)
	_, _ = io.WriteString(w, body)
}

// ssoSessions is a verify-token 200 listing sessions, each the fixture's
// logged-in session with changes applied; a nil change removes the field.
func ssoSessions(t *testing.T, changes ...map[string]any) string {
	t.Helper()

	sessions := make([]map[string]any, 0, len(changes))
	for _, change := range changes {
		session := map[string]any{
			"uuid":            ssoGarageUUID,
			"deviceId":        ssoDeviceID,
			"otpVerify":       true,
			"profileVerify":   true,
			"TokenStatus":     "success",
			"isLogout":        false,
			"garageAccountId": ssoGarageAccountID,
			"serviceCode":     garageServiceCode,
		}
		for key, value := range change {
			if value == nil {
				delete(session, key)
			} else {
				session[key] = value
			}
		}
		sessions = append(sessions, session)
	}

	body, err := json.Marshal(map[string]any{
		"status":  "success",
		"message": "Verify tokens retrieved successfully",
		"code":    "VERIFY_TOKEN_LIST_SUCCESS",
		"state":   "garage_verify_token_list",
		"data":    sessions,
	})
	if err != nil {
		t.Fatalf("encode SSO answer: %v", err)
	}
	return string(body)
}

// cmsCall is one GraphQL request the fake CMS received.
type cmsCall struct {
	OperationName string         `json:"operationName"`
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables"`
	header        http.Header
}

// fakeCMS stands in for the CMS GraphQL endpoint: it records every request
// and answers each one with httpStatus and body.
func fakeCMS(t *testing.T, httpStatus int, body string) (*httptest.Server, chan cmsCall) {
	t.Helper()

	calls := make(chan cmsCall, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call cmsCall
		if r.Method != http.MethodPost || r.URL.Path != "/graphql" {
			t.Errorf("CMS request = %s %s, want POST /graphql", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Errorf("CMS request body: %v", err)
		}
		call.header = r.Header.Clone()
		calls <- call

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpStatus)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	return srv, calls
}

// cmsResult wraps a JSON value as the CMS's answer to an operation whose
// root field the gateway aliases as `result`.
func cmsResult(result string) string {
	return `{"data":{"result":` + result + `}}`
}

// receivedCMSCall returns the one call the CMS got, checking its operation.
func receivedCMSCall(t *testing.T, calls chan cmsCall, operation string) cmsCall {
	t.Helper()
	select {
	case call := <-calls:
		if call.OperationName != operation {
			t.Errorf("CMS operation = %q, want %q", call.OperationName, operation)
		}
		return call
	default:
		t.Fatalf("CMS was not called (want %s)", operation)
		return cmsCall{}
	}
}

func assertNoCMSCall(t *testing.T, calls chan cmsCall) {
	t.Helper()
	select {
	case call := <-calls:
		t.Errorf("CMS was called (%s) for a request the gateway should have rejected", call.OperationName)
	default:
	}
}

func assertVariables(t *testing.T, call cmsCall, want string) {
	t.Helper()
	got, err := json.Marshal(call.Variables)
	if err != nil {
		t.Fatalf("encode variables: %v", err)
	}
	assertJSONEqual(t, got, want)
}

// restJSON sends a request as the test garage: with the bearer and
// service-code the fake SSO accepts.
func restJSON(t *testing.T, method, url, body string) (int, []byte) {
	t.Helper()
	return restJSONWithHeader(t, method, url, body, nil)
}

// restJSONWithHeader is restJSON with header on top: each entry replaces
// the header of that name, and a nil entry removes it.
func restJSONWithHeader(t *testing.T, method, url, body string, header http.Header) (int, []byte) {
	t.Helper()

	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	req.Header.Set("Authorization", "Bearer "+garageToken)
	req.Header.Set(auth.ServiceCodeHeader, garageServiceCode)
	for key, values := range header {
		req.Header.Del(key)
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("%s %s Content-Type = %q, want application/json", method, url, got)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s: read body: %v", method, url, err)
	}
	return resp.StatusCode, raw
}

func assertJSONEqual(t *testing.T, got []byte, want string) {
	t.Helper()

	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode response %q: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("decode expected JSON: %v", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("body mismatch\n got: %s\nwant: %s", got, want)
	}
}
