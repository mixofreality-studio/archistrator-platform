package goodmgr

import "example.com/layoutapp/internal/workflow"

// DeployWorkflow is the package's single workflow func, correctly isolated in
// its own per-workflow file named after the func (minus the "Workflow" suffix,
// lowercased): deploy.go.
func (w *wfs) DeployWorkflow(ctx workflow.Context) error {
	return nil
}
