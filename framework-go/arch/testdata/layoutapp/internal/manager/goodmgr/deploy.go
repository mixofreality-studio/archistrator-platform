package goodmgr

import "example.com/layoutapp/internal/workflow"

// DeployWorkflow is the package's single workflow ENTRY func, correctly
// isolated in its own per-workflow file named after the func (minus the
// "Workflow" suffix, lowercased): deploy.go.
func (w *wfs) DeployWorkflow(ctx workflow.Context) error {
	return deployPrepare(ctx)
}

// deployPrepare is a non-entry, context-taking HELPER: its name does not end
// in "Workflow", so it is not a workflow entry func. It is legal here because
// this file has exactly one entry func (DeployWorkflow) — a workflow file may
// fold its entry point together with the context-taking helpers it calls.
func deployPrepare(ctx workflow.Context) error {
	return nil
}
