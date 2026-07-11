package badmgr

import "example.com/layoutapp/internal/workflow"

// FooWorkflow and BarWorkflow together violate the one-workflow-func-per-file
// rule (workflow-file-multiple-funcs). The filename "workflow.go" also does not
// match the expected "foo.go" derived from the first func name
// (workflow-file-name).
func FooWorkflow(ctx workflow.Context) error { return nil }

func BarWorkflow(ctx workflow.Context) error { return nil }
