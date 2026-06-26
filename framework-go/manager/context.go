package manager

import (
	"context"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// Context is the Manager-layer call context every Manager interface method takes
// as its first parameter. It embeds the standard library context.Context (deadline,
// cancellation, request-scoped values) and carries the acting identity.
//
// Unlike the ResourceAccess context it carries NO idempotency key: a Manager
// orchestrates and MINTS idempotency keys for the ResourceAccess calls it makes;
// it is not itself handed one.
type Context struct {
	context.Context
	// Principal is the identity on whose behalf the workflow runs (authz + audit).
	Principal security.SecurityPrincipal
}
