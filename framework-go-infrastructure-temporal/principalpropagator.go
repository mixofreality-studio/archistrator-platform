// Package temporal is the Temporal infrastructure satellite's production code.
// Today it carries the security-principal context propagator: the bridge that
// flows a framework-go [security.Principal] from an HTTP request, through
// a Temporal workflow, and into its activities, so authorization and audit see
// "who" initiated durable work.
//
// It lives in the Temporal satellite (not in framework-go/utilities/security)
// because a [security.Validator]-style Utility imports NO Temporal — the
// principal TYPE is framework-core, but the machinery that pushes it across the
// Temporal control plane belongs with the Temporal SDK dependency. This package
// depends on framework-go for the principal type; framework-go does not depend on
// it.
package temporal

import (
	"context"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/workflow"
)

// principalHeaderKey is the Temporal header slot the principal travels in. It is
// distinct from any business header; one propagator owns exactly this key.
const principalHeaderKey = "aiarch-security-principal"

// wfPrincipalKey is the workflow.Context key the principal is stored under inside
// workflow code. The framework's [security.Principal] context key (for
// plain context.Context, used by HTTP handlers and activities) is unexported in
// framework-go and reached via [security.WithPrincipal] / [security.PrincipalFrom];
// workflow.Context is a Temporal type framework-go cannot reference, so the
// workflow-side key and its accessors live here.
type wfPrincipalKey struct{}

// WithPrincipalWorkflow returns a workflow.Context carrying p. Workflow code
// rarely calls this directly — [PrincipalPropagator.ExtractToWorkflow] populates
// the principal automatically when a workflow task starts — but it is exported
// for tests and for workflows that derive child contexts.
func WithPrincipalWorkflow(ctx workflow.Context, p security.Principal) workflow.Context {
	return workflow.WithValue(ctx, wfPrincipalKey{}, p)
}

// PrincipalFromWorkflow returns the principal carried by a workflow.Context and
// whether one was present. The workflow-code analogue of [security.PrincipalFrom].
func PrincipalFromWorkflow(ctx workflow.Context) (security.Principal, bool) {
	p, ok := ctx.Value(wfPrincipalKey{}).(security.Principal)
	return p, ok
}

// PrincipalPropagator is a Temporal [workflow.ContextPropagator] that carries a
// [security.Principal] across every hop: starter→workflow,
// workflow→activity, workflow→child. Register it on the Temporal client's
// ContextPropagators (the worker inherits it from the client); both the process
// that starts workflows and the worker process must register it.
//
// It degrades gracefully: a request/workflow with no principal propagates nothing
// (rather than a null payload), and a header that fails to deserialize (e.g. a
// workflow started before this propagator existed, or after a schema change)
// leaves the context principal-less rather than failing the task — important for
// deterministic replay of old histories.
type PrincipalPropagator struct{}

var _ workflow.ContextPropagator = PrincipalPropagator{}

// NewPrincipalPropagator returns the security-principal context propagator to
// register on client.Options.ContextPropagators.
func NewPrincipalPropagator() workflow.ContextPropagator { return PrincipalPropagator{} }

// Inject writes the principal from an outbound context.Context (a starter, e.g.
// an HTTP handler calling ExecuteWorkflow) into the Temporal header.
func (PrincipalPropagator) Inject(ctx context.Context, writer workflow.HeaderWriter) error {
	p, ok := security.PrincipalFrom(ctx)
	if !ok {
		return nil // nothing to propagate
	}
	payload, err := converter.GetDefaultDataConverter().ToPayload(p)
	if err != nil {
		return err
	}
	writer.Set(principalHeaderKey, payload)
	return nil
}

// InjectFromWorkflow writes the principal from workflow.Context into the header
// on an outbound call from workflow code (scheduling an activity or child).
func (PrincipalPropagator) InjectFromWorkflow(ctx workflow.Context, writer workflow.HeaderWriter) error {
	p, ok := PrincipalFromWorkflow(ctx)
	if !ok {
		return nil
	}
	payload, err := converter.GetDefaultDataConverter().ToPayload(p)
	if err != nil {
		return err
	}
	writer.Set(principalHeaderKey, payload)
	return nil
}

// Extract reads the principal from the header into a context.Context — the
// inbound path for activity code, where it becomes readable via
// [security.PrincipalFrom].
func (PrincipalPropagator) Extract(ctx context.Context, reader workflow.HeaderReader) (context.Context, error) {
	if payload, ok := reader.Get(principalHeaderKey); ok {
		var p security.Principal
		if err := converter.GetDefaultDataConverter().FromPayload(payload, &p); err != nil {
			return ctx, nil // graceful degrade — leave context principal-less
		}
		ctx = security.WithPrincipal(ctx, p)
	}
	return ctx, nil
}

// ExtractToWorkflow reads the principal from the header into a workflow.Context —
// the inbound path for workflow code (runs on every workflow task, including
// replays), where it becomes readable via [PrincipalFromWorkflow].
func (PrincipalPropagator) ExtractToWorkflow(ctx workflow.Context, reader workflow.HeaderReader) (workflow.Context, error) {
	if payload, ok := reader.Get(principalHeaderKey); ok {
		var p security.Principal
		if err := converter.GetDefaultDataConverter().FromPayload(payload, &p); err != nil {
			return ctx, nil
		}
		ctx = WithPrincipalWorkflow(ctx, p)
	}
	return ctx, nil
}
