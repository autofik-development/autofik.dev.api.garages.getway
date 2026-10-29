// Package auth is the gateway's gate. Every RPC except the health checks
// carries a garage owner's bearer token, and the gateway asks the garage SSO
// (autofik.dev.api.sso.garage) whether it is good before a handler runs.
// Nothing behind the gateway checks the caller - the CMS authenticates no
// one - so this is the only check there is.
//
// The logic is the SOS gateway's (middleware/user_auth.go and identity.go)
// for the one identity there is here, the garage owner: verify-token is the
// gate; nothing is cached, so a token logged out in SSO stops working on its
// next request; and an SSO that cannot answer fails the request closed. There
// is no profile call beside the gate, because nothing behind this gateway
// reads a profile. The token is never decoded here: SSO is the only thing
// that reads it.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"

	"github.com/autofik-development/autofik.dev.api.garages.getway/internal/interceptors"
)

var (
	// ErrUnauthenticated: SSO said no - a bad or expired token, a session
	// that was logged out, an account with no sessions left. The caller can
	// fix it by logging in again.
	ErrUnauthenticated = errors.New("sso: unauthenticated")
	// ErrNotVerified: the token is good, but the account has not finished OTP
	// or profile verification. Logging in again does not fix that, so it is
	// told apart from ErrUnauthenticated.
	ErrNotVerified = errors.New("sso: garage not verified")
	// ErrInvalidServiceCode: the token is good, but SSO refused the
	// service-code beside it (400 INVALID_SERVICE_CODE): the account last
	// logged in from another app.
	ErrInvalidServiceCode = errors.New("sso: invalid service code")
	// ErrUnavailable: verification did not complete (no answer, a 5xx, an
	// unreadable body). Nothing else checks the caller, so this fails closed.
	ErrUnavailable = errors.New("sso: verification unavailable")
)

// Verifier resolves a bearer token, presented with the caller's
// service-code, to the garage account SSO issued it to.
type Verifier interface {
	VerifyToken(ctx context.Context, token, serviceCode string) (VerifiedGarage, error)
}

// ServiceCodeHeader is the header, and the gRPC metadata key, a caller names
// its app in. SSO stamps the code on the account at login and verify-token
// answers 400 INVALID_SERVICE_CODE to any other, so only the caller knows the
// right one: it is required, and there is no default to fall back on.
const ServiceCodeHeader = "service-code"

// BearerToken is the token in an Authorization value. The scheme is matched
// in any case, as SSO's own /^Bearer (.+)$/i does: a case-sensitive trim
// would turn "bearer x" into "Bearer bearer x" on the way to SSO, which
// refuses it although SSO takes "bearer x" itself. A value with no scheme is
// taken as the token.
func BearerToken(authorization string) string {
	value := strings.TrimSpace(authorization)
	if scheme, token, ok := strings.Cut(value, " "); ok && strings.EqualFold(scheme, "Bearer") {
		return strings.TrimSpace(token)
	}
	if strings.EqualFold(value, "Bearer") {
		return ""
	}
	return value
}

const (
	// maxVerifyBodyBytes caps how much of a verify-token answer is read. It
	// holds one record per device session, a few hundred bytes each.
	maxVerifyBodyBytes = 1 << 20 // 1 MiB

	// maxRefusalBodyBytes caps how much of a refusal is read for its code.
	maxRefusalBodyBytes = 4 << 10 // 4 KiB
)

// HTTPVerifier asks the garage SSO's verify-token about each token. It is
// safe for concurrent use.
type HTTPVerifier struct {
	client  *http.Client
	url     string
	timeout time.Duration
}

// NewHTTPVerifier returns a verifier for config. A nil client gets one with
// a pooled transport. Either way each call is bounded by
// config.VerifyTimeout, inside the RPC's own deadline.
func NewHTTPVerifier(config Config, client *http.Client) *HTTPVerifier {
	if client == nil {
		client = newHTTPClient()
	}
	timeout := config.VerifyTimeout
	if timeout <= 0 {
		timeout = DefaultVerifyTimeout
	}
	return &HTTPVerifier{client: client, url: config.VerifyURL, timeout: timeout}
}

// VerifyToken asks SSO on every call. It logs one "sso verify" line - the
// outcome, the attempts and how long they took, never the token - which
// stands in for metrics. The error carries only SSO's status and code and
// this package's own wording, so the cause of a refusal shows there.
func (v *HTTPVerifier) VerifyToken(ctx context.Context, token, serviceCode string) (VerifiedGarage, error) {
	started := time.Now()
	attempts := 1
	garage, err := v.verify(ctx, token, serviceCode)
	if v.worthRetrying(ctx, err) {
		slog.Warn("sso verify got no answer, retrying", logAttrs(ctx, "error", err.Error())...)
		attempts++
		garage, err = v.verify(ctx, token, serviceCode)
	}

	attrs := []any{
		"outcome", outcome(err),
		"attempts", attempts,
		"duration_ms", time.Since(started).Milliseconds(),
	}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
	}
	slog.Info("sso verify", logAttrs(ctx, attrs...)...)
	return garage, err
}

// logAttrs adds the request's correlation id, when it has one.
func logAttrs(ctx context.Context, attrs ...any) []any {
	if requestID := interceptors.RequestIDFromContext(ctx); requestID != "" {
		attrs = append(attrs, "request_id", requestID)
	}
	return attrs
}

// noAnswer marks a verify-token call that got no HTTP answer at all: it timed
// out, or the connection failed. Its text is the transport's, prefixed with
// how far the call got.
type noAnswer struct{ err error }

func (e noAnswer) Error() string { return e.err.Error() }
func (e noAnswer) Unwrap() error { return e.err }

// worthRetrying reports whether a failed verification gets one more attempt.
// Only when verify-token got no answer at all: one stall past the per-call
// limit (a slow SSO moment, a pooled connection to a pod that went away)
// otherwise fails the RPC with most of its budget unused. The timed-out
// call's connection is closed, so the retry goes out on another one, possibly
// to another SSO pod.
//
// And only when the RPC's own deadline still holds a whole second attempt
// with as long again left for the CMS call after it. An SSO that answered -
// a 4xx, a 5xx, an unreadable body - is not asked again, and neither is one
// called with no deadline, whose budget is unknown.
func (v *HTTPVerifier) worthRetrying(ctx context.Context, err error) bool {
	if !errors.As(err, new(noAnswer)) || ctx.Err() != nil {
		return false
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return false
	}
	return time.Until(deadline) >= 2*v.timeout
}

// The outcomes of a verification, in the "sso verify" log line. sso_error is
// the only failure on the gateway's side; the other refusals are the
// caller's, and expected.
const (
	outcomeOK                 = "ok"
	outcomeUnauthenticated    = "unauthenticated"
	outcomeNotVerified        = "not_verified"
	outcomeInvalidServiceCode = "invalid_service_code"
	outcomeSSOError           = "sso_error"
)

func outcome(err error) string {
	switch {
	case err == nil:
		return outcomeOK
	case errors.Is(err, ErrUnauthenticated):
		return outcomeUnauthenticated
	case errors.Is(err, ErrNotVerified):
		return outcomeNotVerified
	case errors.Is(err, ErrInvalidServiceCode):
		return outcomeInvalidServiceCode
	default:
		return outcomeSSOError
	}
}

// verify is one verify-token call. Every outcome other than a clean yes is
// an error, and a failing gate never reaches a handler.
func (v *HTTPVerifier) verify(ctx context.Context, token, serviceCode string) (VerifiedGarage, error) {
	ctx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()

	resp, err := v.get(ctx, token, serviceCode)
	if err != nil {
		return VerifiedGarage{}, fmt.Errorf("%w: verify-token: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()

	switch {
	// SSO, or a proxy in front of it, did not get to answering: not a no.
	case resp.StatusCode == http.StatusRequestTimeout, resp.StatusCode == http.StatusTooManyRequests:
		return VerifiedGarage{}, fmt.Errorf("%w: verify-token status %d", ErrUnavailable, resp.StatusCode)
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return VerifiedGarage{}, refusal(resp)
	case resp.StatusCode != http.StatusOK:
		return VerifiedGarage{}, fmt.Errorf("%w: verify-token status %d", ErrUnavailable, resp.StatusCode)
	}

	var body struct {
		Data []verifyRecord `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxVerifyBodyBytes)).Decode(&body); err != nil || len(body.Data) == 0 {
		return VerifiedGarage{}, fmt.Errorf("%w: verify-token body unreadable", ErrUnavailable)
	}
	return resolve(body.Data)
}

// The refusals verify-token tells apart by code. The message beside the code
// is translated (x-lang), so it is never matched on.
const (
	// codeInvalidServiceCode comes with a 400: the token passed, but the
	// service-code is not the one the account last logged in from.
	codeInvalidServiceCode = "INVALID_SERVICE_CODE"
	// codeIncompleteProfile comes with the 401 verify-token answers, "garage
	// not complete profile", when a session of the account has not finished
	// OTP or profile verification. It is lower case, unlike UNAUTHORIZED and
	// INVALID_TOKEN, the 401s for a bad token.
	codeIncompleteProfile = "unauthorized"
)

// refusal reads a 4xx from verify-token. Any 4xx is SSO answering no, not SSO
// being down; its code says which no.
func refusal(resp *http.Response) error {
	switch code := ssoErrorCode(resp.Body); code {
	case codeInvalidServiceCode:
		return fmt.Errorf("%w: verify-token status %d", ErrInvalidServiceCode, resp.StatusCode)
	case codeIncompleteProfile:
		return fmt.Errorf("%w: verify-token status %d (%s)", ErrNotVerified, resp.StatusCode, code)
	case "":
		return fmt.Errorf("%w: verify-token status %d", ErrUnauthenticated, resp.StatusCode)
	default:
		return fmt.Errorf("%w: verify-token status %d (%s)", ErrUnauthenticated, resp.StatusCode, code)
	}
}

// ssoErrorCode is the code from an SSO refusal's body,
// {"status":"failure","message":...,"code":"INVALID_SERVICE_CODE"}, or "" if
// there is none.
func ssoErrorCode(body io.Reader) string {
	var refusal struct {
		Code string `json:"code"`
	}
	if json.NewDecoder(io.LimitReader(body, maxRefusalBodyBytes)).Decode(&refusal) != nil {
		return ""
	}
	return refusal.Code
}

// verifyRecord is one session in verify-token's data. The pointers tell an
// absent flag from false.
type verifyRecord struct {
	// UUID is the account's id. SSO builds it from the session row's
	// garage_id column, which it calls garageAccountId internally.
	UUID string `json:"uuid"`
	// GarageAccountID is the account's 8-digit public number - not the uuid,
	// despite the internal column of that name.
	GarageAccountID string `json:"garageAccountId"`
	DeviceID        string `json:"deviceId"`
	ServiceCode     string `json:"serviceCode"`
	OTPVerify       *bool  `json:"otpVerify"`
	ProfileVerify   *bool  `json:"profileVerify"`
	IsLogout        *bool  `json:"isLogout"`
}

// resolve picks the session a 200 lets the request through on.
//
// A 200 means SSO has accepted the bearer - a JWT it signed whose session is
// neither revoked nor expired, or the id of a session that has not expired -
// and the service-code. data is then every session the account has, one per
// device, in no set order, and a logged-out device keeps its row. The gateway
// never decodes the token, so it cannot tell which row is this token's: any
// session still logged in will do, and an account with none left is refused.
func resolve(records []verifyRecord) (VerifiedGarage, error) {
	var rec *verifyRecord
	for i := range records {
		// An absent flag is an unknown shape, not "false": fail closed either
		// way, but 503 tells the operator the contract moved.
		if records[i].IsLogout == nil {
			return VerifiedGarage{}, fmt.Errorf("%w: verify-token record lacks isLogout", ErrUnavailable)
		}
		if rec == nil && !*records[i].IsLogout {
			rec = &records[i]
		}
	}
	if rec == nil {
		return VerifiedGarage{}, fmt.Errorf("%w: every session of the account is logged out", ErrUnauthenticated)
	}
	if rec.OTPVerify == nil || rec.ProfileVerify == nil {
		return VerifiedGarage{}, fmt.Errorf("%w: verify-token record lacks otpVerify/profileVerify", ErrUnavailable)
	}
	// SSO already answers codeIncompleteProfile for an account with an
	// unverified session. The same rule is kept here, so a change on SSO's
	// side cannot quietly open the gate.
	if !*rec.OTPVerify || !*rec.ProfileVerify {
		return VerifiedGarage{}, ErrNotVerified
	}
	if rec.UUID == "" {
		return VerifiedGarage{}, fmt.Errorf("%w: verify-token record has no uuid", ErrUnavailable)
	}

	return VerifiedGarage{
		UUID:            rec.UUID,
		GarageAccountID: rec.GarageAccountID,
		DeviceID:        rec.DeviceID,
		ServiceCode:     rec.ServiceCode,
	}, nil
}

// get sends one verify-token call. A call that got no answer fails with a
// noAnswer, whose text starts with how far it got (see callProgress).
func (v *HTTPVerifier) get(ctx context.Context, token, serviceCode string) (*http.Response, error) {
	progress := &callProgress{}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, progress.trace()), http.MethodGet, v.url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set(ServiceCodeHeader, serviceCode)
	req.Header.Set("Accept", "application/json")
	if requestID := interceptors.RequestIDFromContext(ctx); requestID != "" {
		req.Header.Set("X-Request-Id", requestID)
	}

	resp, err := v.client.Do(req)
	if err != nil {
		return nil, noAnswer{fmt.Errorf("%s: %w", progress, err)}
	}
	return resp, nil
}

// callProgress records how far one SSO call got. "context deadline exceeded"
// alone reads the same whether nothing answers at the address - a port the
// Service does not publish, a NetworkPolicy - or SSO took the request and
// never replied, and which one it is decides where to look. The hooks can
// run on the transport's dial goroutine, even after Do has returned, hence
// the lock.
type callProgress struct {
	mu        sync.Mutex
	addr      string // the address dialled, or the reused connection's peer
	connected bool
	reused    bool // a pooled connection: its peer may be a pod that is gone
	sent      bool
}

func (p *callProgress) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		ConnectStart: func(_, addr string) {
			p.mu.Lock()
			defer p.mu.Unlock()
			p.addr = addr
		},
		GotConn: func(info httptrace.GotConnInfo) {
			p.mu.Lock()
			defer p.mu.Unlock()
			p.connected, p.reused, p.addr = true, info.Reused, info.Conn.RemoteAddr().String()
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			p.mu.Lock()
			defer p.mu.Unlock()
			p.sent = info.Err == nil
		},
	}
}

// String names the step the call did not get past.
func (p *callProgress) String() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case p.sent && p.reused:
		return "request sent to " + p.addr + ", no response (reused connection)"
	case p.sent:
		return "request sent to " + p.addr + ", no response"
	case p.connected:
		return "connected to " + p.addr + ", request not sent"
	case p.addr != "":
		return "no TCP connection to " + p.addr
	}
	return "address not resolved"
}

// newHTTPClient pools connections to SSO. It sets no Timeout of its own:
// every call carries the verify-token limit on its context.
func newHTTPClient() *http.Client {
	return &http.Client{
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
