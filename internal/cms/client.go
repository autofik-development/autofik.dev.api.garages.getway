// Package cms calls the garages CMS (autofik.dev.api.garages.cms) through
// its GraphQL API. Each method sends one fixed document from operations.go
// and returns either the decoded result or an error that is already a gRPC
// status, so handlers can return it unchanged.
//
// The CMS resolvers carry no auth of their own. They read the optional
// x-mysabay-user-id and x-hash/x-signature/x-timestamp headers but enforce
// none of them, so GARAGE_CMS_SERVICE_URL must be an internal address. The
// gateway forwards none of those headers from its own callers, who could set
// them to anything.
package cms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/interceptors"
)

const (
	// DefaultURL is the CMS GraphQL endpoint used when GARAGE_CMS_SERVICE_URL
	// is unset.
	DefaultURL = "http://localhost:4042/graphql"

	// DefaultTimeout bounds one CMS call when HTTP_CLIENT_TIMEOUT_SECONDS is
	// unset. It sits under interceptors.DefaultRequestTimeout so a slow CMS
	// surfaces as a timeout from this client, not as the RPC's own deadline.
	DefaultTimeout = 10 * time.Second

	// maxResponseBytes caps how much of a CMS response is buffered, so a
	// misbehaving upstream cannot exhaust this process's memory.
	maxResponseBytes = 8 << 20 // 8 MiB

	// maxLoggedBodyBytes caps how much of an unexpected response body goes
	// into a log line.
	maxLoggedBodyBytes = 512
)

type Client struct {
	url        string
	httpClient *http.Client
}

// New returns a client for the CMS GraphQL endpoint at url. A nil httpClient
// gets a timeout-bounded default: a zero-value &http.Client{} has no Timeout,
// so one hung CMS socket would pin a goroutine and a file descriptor forever.
func New(url string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = newHTTPClient(DefaultTimeout)
	}
	return &Client{url: url, httpClient: httpClient}
}

// NewFromEnv reads GARAGE_CMS_SERVICE_URL and HTTP_CLIENT_TIMEOUT_SECONDS.
func NewFromEnv() *Client {
	url := strings.TrimSpace(os.Getenv("GARAGE_CMS_SERVICE_URL"))
	if url == "" {
		url = DefaultURL
	}
	return New(url, newHTTPClient(timeoutFromEnv()))
}

type request struct {
	OperationName string         `json:"operationName"`
	Query         string         `json:"query"`
	Variables     map[string]any `json:"variables"`
}

type response[T any] struct {
	Data *struct {
		Result *T `json:"result"`
	} `json:"data"`
	Errors []graphQLError `json:"errors"`
}

type graphQLError struct {
	Message    string                 `json:"message"`
	Extensions graphQLErrorExtensions `json:"extensions"`
}

type graphQLErrorExtensions struct {
	Code string `json:"code"`
	// Status and OriginalError are what @nestjs/apollo attaches for a Nest
	// HttpException; see errorFromGraphQL. They are decoded as any so an
	// unexpected shape cannot make the whole response unreadable.
	Status        any `json:"status"`
	OriginalError *struct {
		Message    any `json:"message"`
		StatusCode any `json:"statusCode"`
	} `json:"originalError"`
}

// do sends one operation and decodes its `result`. Every error it returns is
// already a gRPC status.
func do[T any](ctx context.Context, c *Client, op Operation, variables map[string]any) (T, error) {
	var zero T

	encoded, err := json.Marshal(request{OperationName: op.Name, Query: op.Document, Variables: variables})
	if err != nil {
		return zero, status.Errorf(codes.Internal, "encode cms request: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(encoded))
	if err != nil {
		return zero, status.Errorf(codes.Internal, "build cms request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if requestID := interceptors.RequestIDFromContext(ctx); requestID != "" {
		req.Header.Set("X-Request-Id", requestID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		slog.Error("cms request failed", "operation", op.Name, "error", err)
		return zero, transportError(err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		slog.Error("reading cms response failed", "operation", op.Name, "error", err)
		return zero, transportError(err)
	}

	var decoded response[T]
	decodeErr := json.Unmarshal(raw, &decoded)

	// Apollo answers a resolver error with 200 and a request-level one (such
	// as a variable that fails coercion) with 400, both carrying an errors
	// array, so the errors are read before the HTTP status.
	if decodeErr == nil && len(decoded.Errors) > 0 {
		return zero, errorFromGraphQL(op.Name, decoded.Errors[0])
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slog.Error("cms returned an http error",
			"operation", op.Name, "status", resp.StatusCode, "body", truncate(raw))
		return zero, httpError(resp.StatusCode)
	}
	if decodeErr != nil || decoded.Data == nil || decoded.Data.Result == nil {
		slog.Error("decoding cms response failed",
			"operation", op.Name, "error", decodeErr, "body", truncate(raw))
		return zero, status.Error(codes.Internal, "garages cms returned an unreadable response")
	}

	return *decoded.Data.Result, nil
}

// errorFromGraphQL maps a GraphQL error onto a gRPC status, which
// grpc-gateway renders back as the matching HTTP status.
//
// The HTTP status is read first. @nestjs/apollo gives a Nest HttpException
// its own extensions.code only for 400, 401, 403 and 422; anything else,
// including the CMS's NotFoundException and ConflictException, arrives as
// INTERNAL_SERVER_ERROR with the real status in
// extensions.originalError.statusCode. Going by the code alone would turn
// "Garage not found" into a 500.
func errorFromGraphQL(operation string, gqlErr graphQLError) error {
	ext := gqlErr.Extensions

	if httpStatus := ext.httpStatus(); httpStatus != 0 {
		message := ext.originalMessage(gqlErr.Message)
		if httpStatus >= 500 {
			slog.Error("cms returned an error", "operation", operation, "status", httpStatus, "error", message)
		} else {
			slog.Warn("cms rejected the request", "operation", operation, "status", httpStatus, "error", message)
		}
		return upstreamError(httpStatus, message)
	}

	switch ext.Code {
	case "BAD_USER_INPUT", "BAD_REQUEST":
		// A variable failed coercion against the schema, typically an
		// unknown enum value. The message names the field and the value,
		// which is what the caller needs to fix the request.
		slog.Warn("cms rejected the request", "operation", operation, "code", ext.Code, "error", gqlErr.Message)
		return status.Error(codes.InvalidArgument, gqlErr.Message)
	case "UNAUTHENTICATED":
		return status.Error(codes.Unauthenticated, gqlErr.Message)
	case "FORBIDDEN":
		return status.Error(codes.PermissionDenied, gqlErr.Message)
	case "GRAPHQL_PARSE_FAILED", "GRAPHQL_VALIDATION_FAILED":
		// The documents in operations.go are fixed, so this means the CMS
		// schema has moved away from them.
		slog.Error("cms rejected the gateway's graphql document; the cms schema has changed",
			"operation", operation, "error", gqlErr.Message)
		return status.Error(codes.Internal, "garages cms request failed")
	default:
		slog.Error("cms returned an error", "operation", operation, "code", ext.Code, "error", gqlErr.Message)
		return status.Error(codes.Internal, "garages cms request failed")
	}
}

// httpStatus returns the Nest HttpException status behind a GraphQL error,
// or 0 when there is none.
func (ext graphQLErrorExtensions) httpStatus() int {
	candidates := []any{ext.Status}
	if ext.OriginalError != nil {
		candidates = []any{ext.OriginalError.StatusCode, ext.Status}
	}
	for _, candidate := range candidates {
		if number, ok := candidate.(float64); ok && number >= 400 && number < 600 {
			return int(number)
		}
	}
	return 0
}

// originalMessage returns the HttpException's own message. For a
// ValidationPipe failure the GraphQL message is just "Bad Request
// Exception"; the field-level reasons are in originalError.message.
func (ext graphQLErrorExtensions) originalMessage(fallback string) string {
	if ext.OriginalError == nil {
		return fallback
	}
	switch message := ext.OriginalError.Message.(type) {
	case string:
		if message != "" {
			return message
		}
	case []any:
		parts := make([]string, 0, len(message))
		for _, part := range message {
			if text, ok := part.(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, ", ")
		}
	}
	return fallback
}

// upstreamError maps a CMS HttpException onto a gRPC status.
//
// A 4xx message is passed through: the CMS writes those for the caller
// ("Garage not found", "closeTime must be later than openTime"). A 5xx
// message is not, because it can carry internal detail.
func upstreamError(httpStatus int, message string) error {
	if httpStatus >= 500 || message == "" {
		message = "garages cms request failed with status " + strconv.Itoa(httpStatus)
	}
	return status.Error(grpcCode(httpStatus), message)
}

func grpcCode(httpStatus int) codes.Code {
	switch httpStatus {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return codes.InvalidArgument
	case http.StatusUnauthorized:
		return codes.Unauthenticated
	case http.StatusForbidden:
		return codes.PermissionDenied
	case http.StatusNotFound:
		return codes.NotFound
	case http.StatusConflict:
		return codes.AlreadyExists
	case http.StatusTooManyRequests:
		return codes.ResourceExhausted
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return codes.DeadlineExceeded
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return codes.Unavailable
	}
	if httpStatus >= 500 {
		return codes.Internal
	}
	return codes.InvalidArgument
}

// httpError maps a non-2xx answer that carries no GraphQL errors - a proxy's
// error page, or a GARAGE_CMS_SERVICE_URL with the wrong path - onto a gRPC
// status. That is a fault on the gateway's side of the hop, not a verdict on
// the caller's request, so a 404 here must not read as "garage not found".
func httpError(httpStatus int) error {
	message := "garages cms request failed with status " + strconv.Itoa(httpStatus)
	switch httpStatus {
	case http.StatusTooManyRequests:
		return status.Error(codes.ResourceExhausted, message)
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return status.Error(codes.DeadlineExceeded, message)
	case http.StatusBadGateway, http.StatusServiceUnavailable:
		return status.Error(codes.Unavailable, message)
	default:
		return status.Error(codes.Internal, message)
	}
}

// transportError maps a failed round trip (refused dial, timeout, cancelled
// request) onto a gRPC code rather than codes.Unknown.
func transportError(err error) error {
	var timeout interface{ Timeout() bool }
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &timeout) && timeout.Timeout():
		return status.Error(codes.DeadlineExceeded, "garages cms timed out")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request cancelled before garages cms responded")
	default:
		return status.Error(codes.Unavailable, "garages cms is unreachable")
	}
}

func truncate(raw []byte) string {
	body := strings.TrimSpace(string(raw))
	if len(body) > maxLoggedBodyBytes {
		return fmt.Sprintf("%s... (%d bytes)", body[:maxLoggedBodyBytes], len(body))
	}
	return body
}

func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   3 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   32,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ForceAttemptHTTP2:     true,
		},
	}
}

// timeoutFromEnv reads HTTP_CLIENT_TIMEOUT_SECONDS, falling back to
// DefaultTimeout.
func timeoutFromEnv() time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(os.Getenv("HTTP_CLIENT_TIMEOUT_SECONDS")))
	if err != nil || seconds <= 0 {
		return DefaultTimeout
	}
	return time.Duration(seconds) * time.Second
}
