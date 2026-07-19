package manager

import (
	"time"

	eng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
	ra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// ActivityPreset describes one Activity call site's invocation parameters,
// collapsing the StartToCloseTimeout + nested RetryPolicy struct literal every
// Manager hand-rolls today into a single value. Call Options to produce the
// workflow.ActivityOptions.
//
// The terminal error set is always caller-specified per preset — it is
// deliberately never defaulted to NonRetryableErrorTypes(), because which
// kinds are terminal for a given Activity is a per-Activity decision. For
// example resourceaccess.Conflict is terminal for most write Activities but
// must stay retryable for the rare one that re-reads and retries inside its
// own workflow-level loop.
type ActivityPreset struct {
	// Timeout is the Activity's StartToCloseTimeout.
	Timeout time.Duration
	// MaxAttempts caps RetryPolicy.MaximumAttempts. Zero (the default) leaves
	// it unset, which Temporal treats as unlimited attempts.
	MaxAttempts int32
	// TerminalRA lists the ResourceAccess kinds that are non-retryable for
	// this Activity.
	TerminalRA []ra.Kind
	// TerminalEngine lists the Engine kinds that are non-retryable for this
	// Activity.
	TerminalEngine []eng.Kind
	// TerminalManager lists the Manager kinds that are non-retryable for this
	// Activity.
	TerminalManager []Kind
}

// Options builds the workflow.ActivityOptions this preset describes: the
// configured StartToCloseTimeout, and a RetryPolicy whose NonRetryableErrorTypes
// is exactly TerminalRA + TerminalEngine + TerminalManager (in that order,
// via RAErrType/EngineErrType/ErrType) and whose MaximumAttempts is set only
// when MaxAttempts is non-zero.
func (p ActivityPreset) Options() workflow.ActivityOptions {
	var nonRetryable []string
	for _, k := range p.TerminalRA {
		nonRetryable = append(nonRetryable, RAErrType(k))
	}
	for _, k := range p.TerminalEngine {
		nonRetryable = append(nonRetryable, EngineErrType(k))
	}
	for _, k := range p.TerminalManager {
		nonRetryable = append(nonRetryable, ErrType(k))
	}

	retry := &temporal.RetryPolicy{NonRetryableErrorTypes: nonRetryable}
	if p.MaxAttempts != 0 {
		retry.MaximumAttempts = p.MaxAttempts
	}

	return workflow.ActivityOptions{
		StartToCloseTimeout: p.Timeout,
		RetryPolicy:         retry,
	}
}
