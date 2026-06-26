package engine

import (
	"context"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// Context is the Engine-layer call context every Engine interface method takes as
// its first parameter. It embeds the standard library context.Context (deadline,
// cancellation, request-scoped values) and carries the acting identity explicitly.
//
// The Engine layer is pure compute, so most engines ignore the context; it is
// present for uniformity across all layers (so a port signature looks the same
// everywhere) and for tracing/audit. Engines MUST NOT perform I/O off it.
type Context struct {
	context.Context
	// Principal is the identity on whose behalf the call is made (authz + audit).
	Principal security.SecurityPrincipal
}
