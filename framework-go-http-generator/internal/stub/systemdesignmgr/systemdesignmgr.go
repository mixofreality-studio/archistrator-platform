// Package systemdesignmgr is a MINIMAL stand-in for the real manager package, declaring
// only the interface + I/O types the generated systemdesign code references, with
// signatures that match the server modelgen output (uuid.UUID, int enums,
// struct params). NOT for production use.
package systemdesignmgr

import (
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/internal/stub/manager"
)

// ProjectID identifies a project.
type ProjectID string

// PhaseAdvanceResult is a placeholder I/O type for AdvancePhase.
type PhaseAdvanceResult struct{}

// ArtifactKind is a placeholder I/O type identifying an artifact kind.
type ArtifactKind int

// SessionStateView is a placeholder I/O type for GetSessionState.
type SessionStateView struct{}

// ReviewFeedback is a placeholder I/O type carrying review feedback.
type ReviewFeedback struct{}

// SessionRef is a placeholder I/O type referencing a draft session.
type SessionRef string

// ResearchInput is a placeholder I/O type for SetResearchInput.
type ResearchInput struct{}

// Version is a placeholder I/O type for SetResearchInput's result.
type Version int

// ReviewDecision is a placeholder I/O type for SubmitReviewDecision.
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
