package badmgr

import "example.com/layoutapp/internal/workflow"

// orphanHelper takes a workflow.Context but this file declares no workflow
// ENTRY func (no func here is *Workflow-suffixed) — it is not a workflow
// file, so it falls through to file-not-allowed rather than being silently
// passed just because it carries a context-taking func.
func orphanHelper(ctx workflow.Context) error { return nil }
