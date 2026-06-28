// Package manager is a MINIMAL stand-in for framework-go/manager, used only so
// the generated sample tool package compiles in-module. NOT for production use.
package manager

import (
	"context"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-mcp-generator/internal/stub/security"
)

// Context is the Manager-layer call context.
type Context struct {
	context.Context
	Principal security.SecurityPrincipal
}

// Kind is the façade error classification.
type Kind int

const (
	Unknown Kind = iota
	ContractMisuse
	NotFound
	Unauthorized
	FailedPrecondition
	Infrastructure
)

var kindNames = map[Kind]string{
	Unknown: "Unknown", ContractMisuse: "ContractMisuse", NotFound: "NotFound",
	Unauthorized: "Unauthorized", FailedPrecondition: "FailedPrecondition",
	Infrastructure: "Infrastructure",
}

// String returns the stable Kind name.
func (k Kind) String() string {
	if n, ok := kindNames[k]; ok {
		return n
	}
	return "Unknown"
}

// Error is the uniform Manager façade error.
type Error struct {
	Kind   Kind
	Detail string
	Cause  error
}

func (e *Error) Error() string { return "manager: " + e.Detail }
func (e *Error) Unwrap() error { return e.Cause }
