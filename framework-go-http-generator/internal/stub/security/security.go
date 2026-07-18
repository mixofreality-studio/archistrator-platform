// Package security is a MINIMAL stand-in for the framework-go security utility,
// used only so the generated sample handler package compiles in-module without
// pulling the real framework (and its transitive deps). Its surface mirrors the
// real package exactly where the generated code touches it: PrincipalFrom, the
// Security.Authorize seam, and the Action/ResourceRef/Decision/Principal
// value types. NOT for production use.
package security

import "context"

// Principal is the acting identity.
type Principal struct {
	Subject string
	Email   string
}

type ctxKey struct{}

// PrincipalFrom reads the principal an auth middleware placed on the context.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

// Action is the verb being attempted.
type Action struct{ Verb string }

// ResourceRef is the target of an action.
type ResourceRef struct {
	Kind string
	ID   string
}

// Decision is an authorization outcome.
type Decision struct{ Permit bool }

// Security is the policy-decision seam.
type Security interface {
	Authorize(ctx context.Context, principal Principal, action Action, resource ResourceRef) (Decision, error)
}
