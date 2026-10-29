package auth

import "context"

// VerifiedGarage is the garage account a request's bearer token belongs to,
// as SSO's verify-token reported it. Every RPC that reaches a handler, other
// than the health checks, carries one: see GarageFromContext.
type VerifiedGarage struct {
	// UUID is SSO's id for the account (the garage SSO's Garage.id), never
	// empty. It names the owner's account, not one of their garages: an
	// owner can have several.
	UUID string
	// GarageAccountID is the 8-digit public number SSO shows the owner.
	// Accounts created before SSO had one carry none.
	GarageAccountID string
	// DeviceID is SSO's device row for a session of the account that is
	// still logged in. SSO lists one session per device and the gateway
	// cannot tell which is this token's, so with several logged in it may be
	// another device's.
	DeviceID string
	// ServiceCode is the caller's service-code, which SSO accepted for this
	// account.
	ServiceCode string
}

// garageKey is unexported, so nothing outside this package can put a value
// there that a handler would then trust as verified: only the interceptors
// do, once SSO has said yes.
type garageKey struct{}

func newContext(ctx context.Context, garage VerifiedGarage) context.Context {
	return context.WithValue(ctx, garageKey{}, garage)
}

// GarageFromContext returns the garage the auth interceptor verified. ok is
// false only when the request did not pass the gate: a health check, or a
// handler wired without the interceptor in front of it, since the gate
// refuses every unverified caller before a handler runs.
func GarageFromContext(ctx context.Context) (VerifiedGarage, bool) {
	garage, ok := ctx.Value(garageKey{}).(VerifiedGarage)
	return garage, ok
}
