package resourceaccess

import (
	"context"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// Context is the ResourceAccess-layer call context every ResourceAccess interface
// method takes as its first parameter. It embeds the standard library
// context.Context (deadline, cancellation, request-scoped values), carries the
// acting identity, and — unique to this layer — the idempotency key that makes a
// write safe to retry.
type Context struct {
	context.Context
	// Principal is the identity on whose behalf the access is made (authz + audit).
	Principal security.Principal
	// IdempotencyKey deduplicates retried writes; required for mutating ops.
	IdempotencyKey IdempotencyKey
}
