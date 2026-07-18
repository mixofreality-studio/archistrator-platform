// Package security is the shared security Utility for systems constructed and
// operated by aiarch following Juval Löwy's The Method. It is a stable,
// low-volatility, cross-cutting in-process facet callable from any layer (the
// Utility-bar exception to closed layering, [[the-method-layers]]).
//
// It presents an infrastructure-OPAQUE surface over the platform's identity,
// authorization, webhook-signature and service-identity concerns. Two halves:
//
//   - Authentication (the edge half). A [Validator] turns the bearer access
//     token an HTTP request carries into a typed [Principal]; the
//     [Middleware] wraps any net/http handler to do that and stash the principal
//     on the request context (401 on failure). The concrete token-validation
//     mechanism (JWKS signature check, issuer/expiry verification, IdP claim
//     mapping) lives behind the [Validator] port in a separate satellite module
//     so its heavy dependencies never enter a deployment that does not need them.
//   - Authorization & service infrastructure (the in-process half). [Security]
//     bundles [Security.Authorize] (the policy-decision point), webhook signature
//     verification and short-lived service-identity minting. Each volatile
//     mechanism sits behind an exported, mechanism-NEUTRAL port —
//     [PolicyDecisionPoint], [WebhookVerifier], [ServiceIdentitySource] — so a
//     heavy or alternate implementation (a Cedar PDP, a SPIFFE identity source)
//     can be supplied from a satellite module without a surface change. This
//     package ships working stdlib-only DEFAULTS for all three, so it is fully
//     usable with zero heavy dependencies.
//
// Infrastructure opacity is load-bearing: no security-infrastructure lexeme
// (JWT/OIDC/Keycloak/Cedar/HMAC/Vault) appears on any exported name. The port
// names describe the ROLE, never the mechanism, so swapping the mechanism
// changes no caller and no surface.
//
// Layer rules ([[the-method-layers]]): a Utility imports NO Temporal, publishes
// and subscribes to NO events, and is synchronous/in-process. It is called from
// Client request-handling code before any workflow is started. Propagating a
// [Principal] THROUGH Temporal is therefore not this package's concern —
// that lives in the Temporal infrastructure satellite, which depends on this
// package for the principal type but keeps the Temporal SDK out of here.
package security
