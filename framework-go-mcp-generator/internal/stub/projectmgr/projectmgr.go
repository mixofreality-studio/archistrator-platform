// Package projectmgr is a MINIMAL stand-in for a real manager package, used only
// so the generated sample tool package compiles in-module against the real MCP
// SDK. It declares just the interface + I/O types referenced by the generated
// code from testdata/project.contract.schema.json. NOT for production use.
package projectmgr

import fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go-mcp-generator/internal/stub/manager"

// ProjectID is a named string scalar.
type ProjectID string

// OwnerScope is a named string scalar.
type OwnerScope string

// ProjectState is the full project head-state.
type ProjectState struct {
	ProjectID ProjectID
	Name      string
}

// ProjectSummary is a catalog row.
type ProjectSummary struct {
	ProjectID ProjectID
	Name      string
}

// ProjectManager is the contract interface (manager layer).
type ProjectManager interface {
	CreateProject(rc fwmanager.Context, owner OwnerScope, name string) (ProjectID, error)
	GetProject(rc fwmanager.Context, projectID ProjectID) (ProjectState, error)
	ListProjects(rc fwmanager.Context, owner OwnerScope) ([]ProjectSummary, error)
}
