// Package security is a MINIMAL stand-in for the framework-go security utility,
// used only so the generated sample tool package compiles in-module. The MCP
// generated code touches only PrincipalFrom + Principal. NOT for production use.
package security

import "context"

// Principal is the acting identity.
type Principal struct {
	Subject string
	Email   string
}

type ctxKey struct{}

// PrincipalFrom reads the principal an auth layer placed on the context.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}
