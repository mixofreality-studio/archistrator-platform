// Package operationsmgr is a MINIMAL stand-in for the real manager package, declaring
// only the interface + I/O types the generated operations code references, with
// signatures that match the server modelgen output (uuid.UUID, int enums,
// struct params). NOT for production use.
package operationsmgr

import (
	"github.com/google/uuid"

	fwmanager "github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/internal/stub/manager"
)

// DelinquencyContext is a placeholder I/O type for ApplyDelinquencyPolicy.
type DelinquencyContext struct{}

// DesiredStateChange is a placeholder I/O type for DeployAfterConstruction.
type DesiredStateChange struct{}

// DeployResult is a placeholder I/O type for DeployAfterConstruction.
type DeployResult struct{}

// ScaleWhatIfPoints is a placeholder I/O type for QueryCostProjection.
type ScaleWhatIfPoints struct{}

// CostProjectionSeam is a placeholder I/O type for QueryCostProjection.
type CostProjectionSeam struct{}

// OperatedSystemView is a placeholder I/O type for QueryOperatedSystemView.
type OperatedSystemView struct{}

// ReconcileScope is a placeholder I/O type for ReconcileOperatedState.
type ReconcileScope struct{}

// ReconcileResult is a placeholder I/O type for ReconcileOperatedState.
type ReconcileResult struct{}

// WithdrawReason is a placeholder I/O type for WithdrawSystem.
type WithdrawReason struct{}

// WithdrawResult is a placeholder I/O type for WithdrawSystem.
type WithdrawResult struct{}

// OperationsManager is the contract interface (manager layer).
type OperationsManager interface {
	ApplyDelinquencyPolicy(rc fwmanager.Context, customerID uuid.UUID, delinquencyContext DelinquencyContext) error
	DeployAfterConstruction(rc fwmanager.Context, operatedAppID uuid.UUID, change DesiredStateChange) (DeployResult, error)
	QueryCostProjection(rc fwmanager.Context, operatedAppID uuid.UUID, requestID string, points *ScaleWhatIfPoints) (CostProjectionSeam, error)
	QueryOperatedSystemView(rc fwmanager.Context, operatedAppID uuid.UUID, requestID string) (OperatedSystemView, error)
	ReconcileOperatedState(rc fwmanager.Context, tickID string, scope *ReconcileScope) (ReconcileResult, error)
	WithdrawSystem(rc fwmanager.Context, operatedAppID uuid.UUID, changeID string, reason WithdrawReason) (WithdrawResult, error)
}
