package badeng

import "example.com/layoutapp/internal/workflow"

// Run has a workflow.Context param but sits in the Engine layer, not Manager —
// workflow-func-outside-manager.
func Run(ctx workflow.Context) error { return nil }
