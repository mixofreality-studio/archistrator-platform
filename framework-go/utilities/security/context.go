package security

import "context"

// principalCtxKey is the unexported context key under which a validated
// [SecurityPrincipal] is stored. Using an unexported zero-size struct type as
// the key prevents collisions with any other package's context values and keeps
// the key itself off the package surface — callers go through [WithPrincipal] /
// [PrincipalFrom], never the raw key.
//
// This key is also the anchor for cross-process propagation: the Temporal
// infrastructure satellite reads and writes the principal under THIS key so a
// principal placed on an HTTP request context flows, unchanged, into workflow and
// activity contexts.
type principalCtxKey struct{}

// WithPrincipal returns a copy of ctx carrying the validated principal. The
// [Middleware] calls this after a successful [Validator.ValidateAccessToken];
// downstream handlers read it back with [PrincipalFrom].
func WithPrincipal(ctx context.Context, principal SecurityPrincipal) context.Context {
	return context.WithValue(ctx, principalCtxKey{}, principal)
}

// PrincipalFrom returns the validated principal carried by ctx and whether one
// was present. A false second result means the request reached this point
// without an authenticated principal (the route was not behind [Middleware], or
// the principal was never propagated) — the caller must treat that as
// unauthenticated, never as an anonymous allow.
func PrincipalFrom(ctx context.Context) (SecurityPrincipal, bool) {
	p, ok := ctx.Value(principalCtxKey{}).(SecurityPrincipal)
	return p, ok
}
