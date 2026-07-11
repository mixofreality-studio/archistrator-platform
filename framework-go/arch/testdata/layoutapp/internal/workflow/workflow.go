// Package workflow is a LOCAL STUB standing in for go.temporal.io/sdk/workflow
// so the layoutapp fixture module carries no real Temporal SDK dependency. The
// file-layout checker matches a workflow function parameter by the AST
// selector's NAME ("workflow.Context"), not by resolved import path, so this
// stub satisfies the check identically to the real package.
package workflow

// Context stands in for workflow.Context. Its members are irrelevant — only
// the selector expression "workflow.Context" is inspected.
type Context interface{}
