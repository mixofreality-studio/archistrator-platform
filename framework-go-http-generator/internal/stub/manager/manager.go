// Package manager is a MINIMAL stand-in for framework-go/manager, used only so
// the generated sample handler package compiles in-module. Its Context, Error,
// and Kind surface mirror the real framework where the generated code touches
// them. NOT for production use.
package manager

import (
	"context"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/internal/stub/security"
)

// Context is the Manager-layer call context.
type Context struct {
	context.Context
	Principal security.Principal
}

// Kind is the façade error classification.
type Kind int

// Unknown is the zero-value Kind; a real error should never carry it.
const (
	Unknown Kind = iota
	ContractMisuse
	NotFound
	Unauthorized
	FailedPrecondition
	Infrastructure
)

// Error is the uniform Manager façade error.
type Error struct {
	Kind   Kind
	Detail string
	Cause  error
}

func (e *Error) Error() string { return "manager: " + e.Detail }
func (e *Error) Unwrap() error { return e.Cause }
