package auth

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	// EnvSSOGarageServiceURL is the garage SSO's base URL
	// (autofik.dev.api.sso.garage). It is its own variable, not the
	// SSO_SERVICE_URL the SOS and billing gateways read: that one is the user
	// SSO, a different service that knows no garage tokens.
	EnvSSOGarageServiceURL = "SSO_GARAGE_SERVICE_URL"
	// EnvSSOGarageVerifyURL is the whole verify-token URL. Set, it wins over
	// the base URL, for the day the endpoint moves off its default path.
	EnvSSOGarageVerifyURL = "SSO_GARAGE_VERIFY_URL"
	// EnvSSOVerifyTimeout bounds one verify-token call, as a Go duration
	// ("3s"). Same name and meaning as in the SOS and billing gateways.
	EnvSSOVerifyTimeout = "SSO_VERIFY_TIMEOUT"
)

const (
	// DefaultSSOURL is the garage SSO's own default port (its PORT).
	DefaultSSOURL = "http://localhost:4000"
	// DefaultVerifyPath is where the garage SSO mounts verify-token.
	DefaultVerifyPath = "/api/garage/verify-token"
	// DefaultVerifyTimeout is the SOS gateway's: verify-token answers in
	// about 2s on testing, so a tighter limit turns most verifications into
	// a 503.
	DefaultVerifyTimeout = 3 * time.Second
)

// Config is how the gateway asks SSO about a bearer token.
type Config struct {
	// VerifyURL is the full verify-token URL.
	VerifyURL string
	// VerifyTimeout bounds one verify-token call, inside the RPC's own
	// deadline. Zero means DefaultVerifyTimeout.
	VerifyTimeout time.Duration
}

// ConfigFromEnv reads SSO_GARAGE_VERIFY_URL, or SSO_GARAGE_SERVICE_URL plus
// the default path, and SSO_VERIFY_TIMEOUT. A malformed URL or duration is an
// error rather than a default: a gateway that cannot verify anyone answers
// every RPC with 503, and should not start instead.
func ConfigFromEnv() (Config, error) {
	verifyURL := strings.TrimSpace(os.Getenv(EnvSSOGarageVerifyURL))
	source := EnvSSOGarageVerifyURL
	if verifyURL == "" {
		base := strings.TrimRight(strings.TrimSpace(os.Getenv(EnvSSOGarageServiceURL)), "/")
		if base == "" {
			base = DefaultSSOURL
		}
		verifyURL = base + DefaultVerifyPath
		source = EnvSSOGarageServiceURL
	}
	if u, err := url.Parse(verifyURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Config{}, fmt.Errorf("%s: want an absolute http(s) URL such as %s, got %q",
			source, DefaultSSOURL, verifyURL)
	}

	timeout := DefaultVerifyTimeout
	if raw := strings.TrimSpace(os.Getenv(EnvSSOVerifyTimeout)); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return Config{}, fmt.Errorf("%s: want a positive Go duration such as 3s, got %q", EnvSSOVerifyTimeout, raw)
		}
		timeout = parsed
	}

	return Config{VerifyURL: verifyURL, VerifyTimeout: timeout}, nil
}
