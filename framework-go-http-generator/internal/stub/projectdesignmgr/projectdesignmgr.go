// Package projectdesignmgr is a MINIMAL stand-in for the real manager package, declaring
// only the interface + I/O types the generated projectdesign code references, with
// signatures that match the server modelgen output (uuid.UUID, int enums,
// struct params). NOT for production use.
package projectdesignmgr

import (
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/internal/stub/manager"
)

// ProjectID identifies a project.
type ProjectID string

// PhaseAdvanceResult is a placeholder I/O type for AdvanceToConstruction.
type PhaseAdvanceResult struct{}

// ArtifactKind is a placeholder I/O type identifying an artifact kind.
type ArtifactKind int

// SessionStateView is a placeholder I/O type for GetSessionState.
type SessionStateView struct{}

// ReviewFeedback is a placeholder I/O type carrying review feedback.
type ReviewFeedback struct{}

// SessionRef is a placeholder I/O type referencing a draft session.
type SessionRef string

// ReviewDecision is a placeholder I/O type for SubmitReviewDecision.
type ReviewDecision int

// SDPDecision is a placeholder I/O type for SubmitSDPDecision.
type SDPDecision int

// OptionID identifies an SDP option.
type OptionID string

// ProjectDesignManager is the contract interface (manager layer).
type ProjectDesignManager interface {
	AdvanceToConstruction(rc fwmanager.Context, projectID ProjectID) (PhaseAdvanceResult, error)
	GetSessionState(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind) (SessionStateView, error)
	RequestArtifactDraft(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, feedback *ReviewFeedback) (SessionRef, error)
	RequestSDPCommit(rc fwmanager.Context, projectID ProjectID) (SessionRef, error)
	SubmitReviewDecision(rc fwmanager.Context, projectID ProjectID, kind ArtifactKind, decision ReviewDecision, feedback *ReviewFeedback) error
	SubmitSDPDecision(rc fwmanager.Context, projectID ProjectID, decision SDPDecision, optionID *OptionID, feedback *ReviewFeedback) error
}
