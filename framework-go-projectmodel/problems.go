package projectmodel

import (
	"fmt"
	"sort"
	"strings"
)

// Problem codes: the closed set of loadability problems projectmodel reports. A code is the
// stable handle a gate, a test and a human message all share; the Message is for people.
const (
	// CodeOpUnexported: a Go-emitting contract names an operation with a lowercase (or empty) name.
	CodeOpUnexported = "PM-OP-UNEXPORTED"
	// CodeOpsEmpty: a contract has zero operations (absent, null or []).
	CodeOpsEmpty = "PM-OPS-EMPTY"
	// CodeGoPackageMissing: a built Manager/Engine/ResourceAccess contract has no goPackage.
	CodeGoPackageMissing = "PM-GOPACKAGE-MISSING"
	// CodeFieldUnknown: a contract object carries a field the schema does not define.
	CodeFieldUnknown = "PM-FIELD-UNKNOWN"
	// CodeLayerUnknown: a contract's layer is not one of the known layers.
	CodeLayerUnknown = "PM-LAYER-UNKNOWN"
	// CodeDepTarget: a dep names an unknown component or breaks the downward-only layer rule.
	CodeDepTarget = "PM-DEP-TARGET"
	// CodeRelEnds: a systemDesign relationship endpoint names no component.
	CodeRelEnds = "PM-REL-ENDS"
	// CodeDeployRef: a deployment declaration references something that is not declared.
	CodeDeployRef = "PM-DEPLOY-REF"
	// CodeDecode: a document that is not decodable at all (malformed JSON, wrong JSON type,
	// no interface descriptor). Added beside the plan's set: a decode failure has no other home.
	CodeDecode = "PM-DECODE"
)

// Problem is one loadability problem. Path locates it in project.json, for example
// "serviceContracts.messageBus.interface.operations[0].name".
type Problem struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Problems is every problem found at once. It is an error whenever it is non-empty.
type Problems []Problem

// Error renders one line per problem, sorted by Path then Code (then Message, for a total order).
func (p Problems) Error() string {
	sorted := make(Problems, len(p))
	copy(sorted, p)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
	lines := make([]string, len(sorted))
	for i, q := range sorted {
		lines[i] = fmt.Sprintf("projectmodel: %s %s: %s", q.Code, q.Path, q.Message)
	}
	return strings.Join(lines, "\n")
}

// Has reports whether any problem carries code.
func (p Problems) Has(code string) bool {
	for _, q := range p {
		if q.Code == code {
			return true
		}
	}
	return false
}

// err is the problems as an error, and an untyped nil when there are none (a nil Problems
// stored in an error interface is non-nil, the typed-nil trap).
func (p Problems) err() error {
	if len(p) == 0 {
		return nil
	}
	return p
}

func problem(path, code, format string, args ...any) Problem {
	return Problem{Path: path, Code: code, Message: fmt.Sprintf(format, args...)}
}
