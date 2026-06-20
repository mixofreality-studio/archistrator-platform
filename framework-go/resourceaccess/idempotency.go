package resourceaccess

// IdempotencyKey is the shared, caller-supplied, stable-per-logical-mutation
// dedup token every RA component accepts. A Manager derives it from
// "${workflowId}:${activityId}" and passes it down; the RA layer treats it as an
// opaque string. (projectStateAccess.md §3.0, artifactAccess.md §0, workerAccess.md §3.0)
type IdempotencyKey string

// IsZero reports whether the key is unset (the empty string).
func (k IdempotencyKey) IsZero() bool { return k == "" }
