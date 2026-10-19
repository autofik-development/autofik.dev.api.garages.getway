// Package test holds the gateway's integration tests. They exercise the
// service the way a caller does - over a real gRPC connection and, for the
// REST surface, through the real grpc-gateway mux - against a fake CMS that
// speaks GraphQL over HTTP, rather than reaching into unexported internals.
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
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

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

// TestMain silences the gateway's structured access logging.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

type testGateway struct {
	conn *grpc.ClientConn
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
// cmsURL, typically a fakeCMS.
func newTestGatewayWithCMS(t *testing.T, cmsURL string) *testGateway {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptors.Recovery(),
			interceptors.Logging(),
			interceptors.Timeout(5*time.Second),
		),
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

	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
		rest.Close()
		_ = closeGatewayConn()
		cancel()
		grpcServer.Stop()
	})

	return &testGateway{conn: conn, rest: rest}
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

func restJSON(t *testing.T, method, url, body string) (int, []byte) {
	t.Helper()
	return restJSONWithHeader(t, method, url, body, nil)
}

func restJSONWithHeader(t *testing.T, method, url, body string, header http.Header) (int, []byte) {
	t.Helper()

	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	for key, values := range header {
		req.Header[key] = values
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
