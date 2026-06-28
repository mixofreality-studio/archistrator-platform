// Package security is a MINIMAL stand-in for the framework-go security utility,
// used only so the generated sample tool package compiles in-module. The MCP
// generated code touches only PrincipalFrom + SecurityPrincipal. NOT for
// production use.
package security

import "context"

// SecurityPrincipal is the acting identity.
type SecurityPrincipal struct {
	Subject string
	Email   string
}

type ctxKey struct{}

// PrincipalFrom reads the principal an auth layer placed on the context.
func PrincipalFrom(ctx context.Context) (SecurityPrincipal, bool) {
	p, ok := ctx.Value(ctxKey{}).(SecurityPrincipal)
	return p, ok
}
