package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/cms"
	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/gateway"
	pb "github.com/autofik-development/autofik.dev.api.garages.getway/internal/grpc/pb/garages/v1"
	service "github.com/autofik-development/autofik.dev.api.garages.getway/internal/grpc/service"
	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/interceptors"
)

// serviceName is reported by the gRPC health service and in every log line.
const serviceName = service.ServiceName

// version is stamped at build time with -ldflags "-X main.version=...".
var version = "dev"

const (
	// maxMessageSize caps a single gRPC message. The grpc-go default is 4 MiB
	// on receive and unlimited on send; both are pinned here so the limit is
	// explicit and symmetric with the loopback client in internal/gateway.
	maxMessageSize = 8 << 20 // 8 MiB

	// maxConcurrentStreams bounds in-flight RPCs per connection. Unset, a
	// single client can open unlimited concurrent streams and exhaust this
	// process's memory.
	maxConcurrentStreams = 512

	// drainDelay is the pause between flipping readiness to false and
	// actually shutting the listeners down, so a load balancer has time to
	// notice and stop sending new work.
	drainDelay = 5 * time.Second

	// shutdownTimeout bounds the graceful stop before connections are cut.
	shutdownTimeout = 25 * time.Second
)

func main() {
	// The runtime image is distroless: no shell, no curl. `garages-gateway
	// -healthcheck` therefore acts as the container healthcheck by speaking
	// the gRPC health protocol to this same process's listener.
	if len(os.Args) > 1 && (os.Args[1] == "-healthcheck" || os.Args[1] == "--healthcheck") {
		os.Exit(runHealthCheck())
	}

	setupLogging()

	listenAddr := envOr("GRPC_LISTEN_ADDR", ":"+gateway.DefaultGRPCPort)
	gatewayAddr := envOr("GATEWAY_LISTEN_ADDR", ":8083")

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		slog.Error("failed to listen", "addr", listenAddr, "error", err)
		os.Exit(1)
	}

	grpcServer := newGRPCServer()

	// Register the standard gRPC health protocol. This - not an HTTP path -
	// is what k8s `grpc:` probes, Envoy, and gRPC client-side load balancing
	// actually call. The status is flipped to NOT_SERVING on shutdown so
	// callers drain before the listener closes.
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus(serviceName, healthpb.HealthCheckResponse_SERVING)

	// Server reflection enumerates the full API surface to anyone who can
	// reach the port. It stays off unless explicitly enabled, so it is a
	// local debugging aid rather than a production disclosure.
	if enabled("GRPC_ENABLE_REFLECTION") {
		reflection.Register(grpcServer)
		slog.Warn("gRPC server reflection is enabled; do not enable this in production")
	}

	pb.RegisterGaragesGatewayServiceServer(grpcServer, service.NewGaragesGatewayServer(cms.NewFromEnv()))

	serveErrs := make(chan error, 2)

	go func() {
		slog.Info("gRPC server listening", "addr", listenAddr)
		if err := grpcServer.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			serveErrs <- err
		}
	}()

	gwCtx, cancelGateway := context.WithCancel(context.Background())
	defer cancelGateway()

	handler, closeConn, err := gateway.NewHandler(gwCtx, gateway.LoopbackTarget(listenAddr))
	if err != nil {
		slog.Error("failed to build grpc-gateway handler", "error", err)
		grpcServer.Stop()
		os.Exit(1)
	}
	// Deliberately not deferred: shutdown() must close this connection at a
	// specific point in the drain, before GracefulStop - see below.

	httpServer := &http.Server{
		Addr:    gatewayAddr,
		Handler: gateway.NewMux(handler),
		// Without these an idle or slow client holds a connection (and its
		// goroutine) open indefinitely - the Slowloris shape.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}

	go func() {
		slog.Info("REST/JSON gateway listening", "addr", gatewayAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrs <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stop:
		slog.Info("shutdown signal received", "signal", sig.String())
	case err := <-serveErrs:
		slog.Error("server stopped unexpectedly", "error", err)
	}

	shutdown(grpcServer, healthServer, httpServer, closeConn)
}

// shutdown drains in a deliberate order:
//
//  1. stop advertising readiness, so the load balancer routes elsewhere;
//  2. wait, so it actually notices before the listeners close;
//  3. close the HTTP gateway, letting in-flight REST requests finish - they
//     loop back into this same gRPC server, so they must drain first;
//  4. close the gateway's loopback gRPC client connection;
//  5. GracefulStop the gRPC server.
//
// Step 4 is not optional. GracefulStop blocks until every client connection
// is gone, and the grpc-gateway handler holds a long-lived one to this very
// server. Leaving it open deadlocks the graceful stop until the timeout
// below force-kills it.
func shutdown(grpcServer *grpc.Server, healthServer *health.Server, httpServer *http.Server, closeGatewayConn func() error) {
	slog.Info("draining", "delay", drainDelay.String())

	gateway.SetReady(false)
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	healthServer.SetServingStatus(serviceName, healthpb.HealthCheckResponse_NOT_SERVING)

	if drainDelay > 0 {
		timer := time.NewTimer(drainDelay)
		defer timer.Stop()
		<-timer.C
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("gateway shutdown error", "error", err)
	}

	if closeGatewayConn != nil {
		if err := closeGatewayConn(); err != nil {
			slog.Warn("closing gateway loopback connection", "error", err)
		}
	}

	// GracefulStop blocks until every in-flight RPC finishes; fall back to a
	// hard Stop if that outlasts the shutdown budget, so the process cannot
	// hang past the orchestrator's termination grace period.
	stopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
		slog.Info("shutdown complete")
	case <-shutdownCtx.Done():
		slog.Warn("graceful stop timed out, forcing shutdown")
		grpcServer.Stop()
	}
}

// runHealthCheck dials this process's own gRPC listener and reports whether
// it is SERVING. It returns a process exit code.
func runHealthCheck() int {
	target := gateway.LoopbackTarget(envOr("GRPC_LISTEN_ADDR", ":"+gateway.DefaultGRPCPort))

	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: dial %s: %v\n", target, err)
		return 1
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resp, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{Service: serviceName})
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: %v\n", err)
		return 1
	}
	if resp.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		fmt.Fprintf(os.Stderr, "healthcheck: status %s\n", resp.GetStatus())
		return 1
	}

	return 0
}

func newGRPCServer() *grpc.Server {
	return grpc.NewServer(
		// Order matters: Recovery is outermost so it also catches a panic
		// raised inside another interceptor; Logging then records the final
		// status code; Timeout bounds the handler.
		grpc.ChainUnaryInterceptor(
			interceptors.Recovery(),
			interceptors.Logging(),
			interceptors.Timeout(interceptors.TimeoutFromEnv()),
		),
		grpc.MaxRecvMsgSize(maxMessageSize),
		grpc.MaxSendMsgSize(maxMessageSize),
		grpc.MaxConcurrentStreams(maxConcurrentStreams),
		// Without keepalives, a connection idled out by an intermediate load
		// balancer looks alive to both ends until the next RPC fails.
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     5 * time.Minute,
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 30 * time.Second,
			Time:                  1 * time.Minute,
			Timeout:               20 * time.Second,
		}),
		// EnforcementPolicy rejects clients that ping more aggressively than
		// this, which would otherwise be a cheap way to burn server CPU.
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             30 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.ConnectionTimeout(10*time.Second),
	)
}

// setupLogging installs a structured JSON logger.
func setupLogging() {
	level := slog.LevelInfo
	if err := level.UnmarshalText([]byte(envOr("LOG_LEVEL", "info"))); err != nil {
		level = slog.LevelInfo
	}

	var handler slog.Handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	if strings.EqualFold(envOr("LOG_FORMAT", "json"), "text") {
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	}

	slog.SetDefault(slog.New(handler).With("service", serviceName, "version", version))
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// enabled parses a boolean feature flag, defaulting to false.
func enabled(key string) bool {
	value, err := strconv.ParseBool(strings.TrimSpace(os.Getenv(key)))
	return err == nil && value
}
