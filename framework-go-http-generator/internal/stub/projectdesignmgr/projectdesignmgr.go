// Package projectdesignmgr is a MINIMAL stand-in for the real manager package, declaring
// only the interface + I/O types the generated projectdesign code references, with
// signatures that match the server modelgen output (uuid.UUID, int enums,
// struct params). NOT for production use.
package projectdesignmgr

import (
	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/internal/stub/manager"
)

type ProjectID string
type PhaseAdvanceResult struct{}
type ArtifactKind int
type SessionStateView struct{}
type ReviewFeedback struct{}
type SessionRef string
type ReviewDecision int
type SDPDecision int
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
