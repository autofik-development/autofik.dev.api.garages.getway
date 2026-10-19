package gateway

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
)

// healthyResponse mirrors the platform's external health-check contract
// verbatim (including its "serive"/"ping-code" key spelling), so it is
// written as a plain struct rather than generated from proto -
// grpc-gateway's UseProtoNames marshaler can't emit a hyphenated key or an
// intentionally misspelled field name from a .proto field.
type healthyResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	Code     string `json:"code"`
	Serive   string `json:"serive"`
	PingCode string `json:"ping-code"`
}

// ready reports whether this process is accepting traffic. It is flipped to
// false at the start of graceful shutdown so a load balancer stops routing
// new requests before the listeners actually close, instead of seeing
// connection refused mid-drain.
var ready atomic.Bool

func init() { ready.Store(true) }

// SetReady marks the process ready or draining.
func SetReady(value bool) { ready.Store(value) }

// IsReady reports the current readiness state.
func IsReady() bool { return ready.Load() }

// HealthyHandler serves the liveness endpoint. It is mounted ahead of the
// grpc-gateway mux in cmd/main.go, so it never passes through the gRPC
// UnaryInterceptor chain.
//
// Liveness answers "is this process alive", so it stays 200 while draining -
// only readiness flips. A liveness probe that fails during drain gets the
// container killed mid-request.
func HealthyHandler(w http.ResponseWriter, r *http.Request) {
	writeHealth(w, http.StatusOK, "success", "ok", "HEALTH_CHECK_SUCCESS")
}

// ReadyHandler serves the readiness endpoint used to gate traffic.
func ReadyHandler(w http.ResponseWriter, r *http.Request) {
	if !IsReady() {
		writeHealth(w, http.StatusServiceUnavailable, "error", "draining", "HEALTH_CHECK_DRAINING")
		return
	}
	writeHealth(w, http.StatusOK, "success", "ready", "HEALTH_CHECK_SUCCESS")
}

func writeHealth(w http.ResponseWriter, httpStatus int, status, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(httpStatus)
	_ = json.NewEncoder(w).Encode(healthyResponse{
		Status:   status,
		Message:  message,
		Code:     code,
		Serive:   "garages getway",
		PingCode: "AA001",
	})
}
