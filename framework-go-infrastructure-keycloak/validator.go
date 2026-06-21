package keycloak

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"github.com/golang-jwt/jwt/v5"
)

// Config parameterizes the Keycloak [security.Validator].
type Config struct {
	// JWKSURL is the endpoint signing keys are fetched from — typically the
	// realm's internal cluster URL, e.g.
	// "http://keycloak:8080/realms/<realm>/protocol/openid-connect/certs". It is
	// independent of Issuer so keys can be fetched in-cluster while the externally
	// minted issuer is validated.
	JWKSURL string
	// Issuer is the exact "iss" value a valid token must carry — typically the
	// realm's external URL, e.g. "https://keycloak.example.com/realms/<realm>".
	Issuer string
	// Leeway is the permitted clock skew for exp/nbf/iat validation. Zero disables
	// leeway (exact expiry). A few seconds is typical.
	Leeway time.Duration
}

// validator implements [security.Validator] for Keycloak access tokens.
type validator struct {
	keyfunc jwt.Keyfunc
	issuer  string
	leeway  time.Duration
}

var _ security.Validator = (*validator)(nil)

// NewValidator builds a Keycloak [security.Validator]. It immediately fetches the
// JWKS from cfg.JWKSURL (failing if unreachable) and then keeps it refreshed in
// the background for the lifetime of ctx, so key rotation is picked up without a
// per-request fetch. ctx therefore governs the validator's background refresh and
// should live as long as the server.
func NewValidator(ctx context.Context, cfg Config) (security.Validator, error) {
	if cfg.JWKSURL == "" {
		return nil, errors.New("keycloak: Config.JWKSURL is required")
	}
	if cfg.Issuer == "" {
		return nil, errors.New("keycloak: Config.Issuer is required")
	}
	k, err := keyfunc.NewDefaultCtx(ctx, []string{cfg.JWKSURL})
	if err != nil {
		return nil, fmt.Errorf("keycloak: initialize JWKS from %q: %w", cfg.JWKSURL, err)
	}
	return &validator{keyfunc: k.Keyfunc, issuer: cfg.Issuer, leeway: cfg.Leeway}, nil
}

// ValidateAccessToken verifies the token's RS256 signature against the JWKS,
// checks issuer and expiry, and maps the Keycloak claims to a principal. Any
// verification failure yields [security.ErrUnauthenticated]; a verified token
// with no subject yields [security.ErrPrincipalUnknown]. Audience is not checked
// (Keycloak access-token semantics).
//
// ctx is unused: the signing keys are kept fresh by the background refresh bound
// to the context passed to [NewValidator], and token verification is pure CPU.
func (v *validator) ValidateAccessToken(_ context.Context, rawToken string) (security.SecurityPrincipal, error) {
	claims := jwt.MapClaims{}
	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(v.issuer),
		jwt.WithExpirationRequired(),
	}
	if v.leeway > 0 {
		opts = append(opts, jwt.WithLeeway(v.leeway))
	}
	if _, err := jwt.ParseWithClaims(rawToken, claims, v.keyfunc, opts...); err != nil {
		return security.SecurityPrincipal{}, security.WrapError(security.ErrUnauthenticated, err)
	}
	p := mapPrincipal(claims)
	if p.Subject == "" {
		return security.SecurityPrincipal{}, security.NewError(security.ErrPrincipalUnknown)
	}
	return p, nil
}

// mapPrincipal translates a verified Keycloak claim set into the platform's
// principal. It reads the keys the platform maps to identity and carries the rest
// through verbatim in [security.SecurityPrincipal.Claims].
func mapPrincipal(claims jwt.MapClaims) security.SecurityPrincipal {
	username := claimString(claims, "preferred_username")
	return security.SecurityPrincipal{
		Kind:          principalKind(username),
		Subject:       claimString(claims, "sub"),
		Username:      username,
		Email:         claimString(claims, "email"),
		Name:          claimString(claims, "name"),
		Issuer:        claimString(claims, "iss"),
		Roles:         realmRoles(claims),
		Organizations: organizations(claims),
		Claims:        map[string]any(claims),
	}
}

// serviceAccountPrefix is the Keycloak convention for a service-account token's
// preferred_username ("service-account-<client-id>"), the platform's signal that
// the principal is a non-interactive application rather than a user.
const serviceAccountPrefix = "service-account-"

func principalKind(username string) security.PrincipalKind {
	if strings.HasPrefix(username, serviceAccountPrefix) {
		return security.PrincipalApplication
	}
	return security.PrincipalUser
}

// realmRoles reads realm_access.roles (Keycloak realm-level roles).
func realmRoles(claims jwt.MapClaims) []string {
	realmAccess, ok := claims["realm_access"].(map[string]any)
	if !ok {
		return nil
	}
	rawRoles, ok := realmAccess["roles"].([]any)
	if !ok {
		return nil
	}
	roles := make([]string, 0, len(rawRoles))
	for _, r := range rawRoles {
		if s, ok := r.(string); ok && s != "" {
			roles = append(roles, s)
		}
	}
	return roles
}

// organizations reads the Keycloak "organization" claim, a map of
// organization-name → object carrying at least an "id" field.
func organizations(claims jwt.MapClaims) []security.Organization {
	orgClaim, ok := claims["organization"].(map[string]any)
	if !ok {
		return nil
	}
	orgs := make([]security.Organization, 0, len(orgClaim))
	for name, raw := range orgClaim {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, _ := entry["id"].(string)
		if id == "" {
			continue
		}
		orgs = append(orgs, security.Organization{ID: id, Name: name})
	}
	if len(orgs) == 0 {
		return nil
	}
	return orgs
}

func claimString(claims jwt.MapClaims, key string) string {
	s, _ := claims[key].(string)
	return s
}
