// Package systemdesignmgr is a MINIMAL stand-in for the real manager package, declaring
// only the interface + I/O types the generated systemdesign code references, with
// signatures that match the server modelgen output (uuid.UUID, int enums,
// struct params). NOT for production use.
package systemdesignmgr

import (
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go-mcp-generator/internal/stub/manager"
)

type ProjectID string
type PhaseAdvanceResult struct{}
type ArtifactKind int
type SessionStateView struct{}
type ReviewFeedback struct{}
type SessionRef string
type ResearchInput struct{}
type Version int
type ReviewDecision int

// SystemDesignManager is the contract interface (manager layer).
type SystemDesignManager interface {
	AdvancePhase(rc fwmanager.Context, projectID ProjectID) (PhaseAdvanceResult, error)
	GetSessionState(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) (SessionStateView, error)
	RequestArtifactDraft(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, feedback *ReviewFeedback) (SessionRef, error)
	SetResearchInput(rc fwmanager.Context, projectID ProjectID, research ResearchInput) (Version, error)
	StartSystemDesign(rc fwmanager.Context, projectID ProjectID) (SessionRef, error)
	SubmitReviewDecision(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, decision ReviewDecision, feedback *ReviewFeedback) error
}
