package test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/gateway"
	pb "github.com/autofik-development/autofik.dev.api.garages.getway/internal/grpc/pb/garages/v1"
	service "github.com/autofik-development/autofik.dev.api.garages.getway/internal/grpc/service"
)

func TestGRPCHealthProtocol(t *testing.T) {
	gw := newTestGateway(t)
	client := healthpb.NewHealthClient(gw.conn)

	// "" is the whole-server status; the named entry is what the binary's
	// own -healthcheck flag asks for.
	for _, name := range []string{"", service.ServiceName} {
		resp, err := client.Check(context.Background(), &healthpb.HealthCheckRequest{Service: name})
		if err != nil {
			t.Fatalf("Check(%q): %v", name, err)
		}
		if resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
			t.Errorf("Check(%q) = %s, want SERVING", name, resp.GetStatus())
		}
	}
}

func TestHealthCheckRPC(t *testing.T) {
	gw := newTestGateway(t)

	resp, err := pb.NewGaragesGatewayServiceClient(gw.conn).HealthCheck(context.Background(), &pb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	if resp.GetStatus() != "ok" || resp.GetService() != service.ServiceName {
		t.Errorf("HealthCheck = {status:%q service:%q}, want {ok %s}", resp.GetStatus(), resp.GetService(), service.ServiceName)
	}
}

// TestRESTHealthCheck goes through grpc-gateway, so it also proves the
// google.api.http route is wired and the JSON keys are the proto names.
func TestRESTHealthCheck(t *testing.T) {
	gw := newTestGateway(t)

	var body map[string]string
	status := getJSON(t, gw.rest.URL+"/garages/v1/health", &body)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if body["status"] != "ok" || body["service"] != service.ServiceName {
		t.Errorf("body = %v, want status=ok service=%s", body, service.ServiceName)
	}
}

func TestProbeEndpoints(t *testing.T) {
	gw := newTestGateway(t)

	for _, path := range []string{"/healty", "/healthz", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			var body map[string]string
			status := getJSON(t, gw.rest.URL+path, &body)

			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200", status)
			}
			want := map[string]string{
				"status":    "success",
				"code":      "HEALTH_CHECK_SUCCESS",
				"serive":    "garages getway",
				"ping-code": "AA001",
			}
			for key, value := range want {
				if body[key] != value {
					t.Errorf("%s = %q, want %q", key, body[key], value)
				}
			}
		})
	}
}

// TestDraining checks that only readiness flips during shutdown: a liveness
// probe that failed mid-drain would get the container killed mid-request.
func TestDraining(t *testing.T) {
	gw := newTestGateway(t)

	gateway.SetReady(false)
	t.Cleanup(func() { gateway.SetReady(true) })

	var ready map[string]string
	if status := getJSON(t, gw.rest.URL+"/readyz", &ready); status != http.StatusServiceUnavailable {
		t.Errorf("/readyz while draining = %d, want 503", status)
	}
	if ready["code"] != "HEALTH_CHECK_DRAINING" {
		t.Errorf("/readyz code = %q, want HEALTH_CHECK_DRAINING", ready["code"])
	}

	var live map[string]string
	if status := getJSON(t, gw.rest.URL+"/healthz", &live); status != http.StatusOK {
		t.Errorf("/healthz while draining = %d, want 200", status)
	}
}

func TestLoopbackTarget(t *testing.T) {
	cases := map[string]string{
		":50057":         "127.0.0.1:50057",
		"0.0.0.0:9000":   "127.0.0.1:9000",
		"not-an-address": "127.0.0.1:" + gateway.DefaultGRPCPort,
	}
	for in, want := range cases {
		if got := gateway.LoopbackTarget(in); got != want {
			t.Errorf("LoopbackTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

func getJSON(t *testing.T, url string, out any) int {
	t.Helper()

	status, raw := restJSON(t, http.MethodGet, url, "")
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("GET %s: decode: %v", url, err)
	}
	return status
}
