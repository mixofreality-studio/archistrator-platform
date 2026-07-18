package security

import "context"

// Validator is the authentication seam: it turns the raw bearer access token a
// request carried into a typed [Principal]. It is the ONE place a raw
// token is parsed and cryptographically verified.
//
// The name is mechanism-neutral on purpose. A concrete Validator verifies the
// token signature against the issuer's keys, checks the issuer and expiry, and
// maps the token claims to a principal — but none of that vocabulary (JWT, JWKS,
// OIDC, Keycloak) appears here. The concrete implementation lives in a satellite
// module so its verification dependencies never enter a deployment that fronts
// authentication differently.
//
// A Validator MUST fail closed: any token it cannot fully verify yields a
// *[Error] of kind [ErrUnauthenticated] (or [ErrPrincipalUnknown] when a
// well-formed, verified token maps to no provisioned principal), never a
// partially-trusted principal.
type Validator interface {
	ValidateAccessToken(ctx context.Context, rawToken string) (Principal, error)
}
