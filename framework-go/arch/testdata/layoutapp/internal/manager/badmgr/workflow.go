package badmgr

import "example.com/layoutapp/internal/workflow"

// FooWorkflow and BarWorkflow are both workflow ENTRY funcs (name ends
// "Workflow" + workflow.Context param), so together they violate the
// one-entry-func-per-file rule (workflow-file-multiple-funcs). The filename
// "workflow.go" also does not match the expected "foo.go" derived from the
// first entry func's name (workflow-file-name).
func FooWorkflow(ctx workflow.Context) error { return nil }

func BarWorkflow(ctx workflow.Context) error { return nil }
