// Package keycloak is the concrete token [security.Validator] for systems whose
// edge IdP is Keycloak. It is an infrastructure SATELLITE of
// github.com/davidmarne/archistrator-platform/framework-go: it implements the framework's
// mechanism-neutral [security.Validator] port using the heavy verification
// dependencies (a JWKS client and a JWT parser) so those dependencies never
// enter the framework core or a deployment that authenticates differently.
//
// It validates Keycloak ACCESS tokens (not ID tokens): it verifies the RS256
// signature against the realm's JWKS, checks the issuer and expiry, and maps the
// Keycloak claim shape (preferred_username, email, realm_access.roles, the
// organization claim, the service-account convention for application principals)
// onto a framework [security.SecurityPrincipal]. Audience is deliberately NOT
// checked — Keycloak access tokens carry the client in azp, and aud is an
// array/absent — matching how the platform's other servers treat these tokens.
//
// The JWKS fetch URL and the issuer string are configured independently
// ([Config.JWKSURL] vs [Config.Issuer]) so a deployment can fetch keys from an
// internal cluster URL while validating the external issuer the token carries.
package keycloak
