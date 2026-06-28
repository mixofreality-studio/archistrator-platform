// Package projectmgr is a MINIMAL stand-in for a real manager package
// (internal/manager/project), used only so the generated sample handler package
// compiles in-module. It declares just the interface + I/O types the generated
// code from testdata/project.contract.schema.json references. NOT for production.
package projectmgr

import fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/internal/stub/manager"

// ProjectID is a named string scalar (an ID-ish type -> path param).
type ProjectID string

// OwnerScope is a named string scalar that is NOT ID-ish (-> body/query).
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
