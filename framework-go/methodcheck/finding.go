// Package methodcheck is a go-test-runnable validator for a Method project that
// checks BOTH the committed `.aiarch/state/project.json` design JSON (the ~24
// Method-invariant cross-artifact rules) AND that the built Go code aligns with
// the design's System component model. A consuming module runs Check from an
// ordinary go test, supplying a ProjectSpec pointing at its repo root + the
// arch.Spec that maps its directories to Method layers.
//
// It MIRRORS the sibling arch package (arch.Check(t, spec)): same posture, same
// go/packages-based code walk reused for the design↔code alignment pass.
//
// framework-go sits BELOW aiarch: this package imports NOTHING from
// github.com/daveandamira/archistrator/server. The rule predicates are PORTED
// faithfully (same rule IDs / severities / messages — they are the contract the
// CI check + diagnostics reference) over lightweight structural structs that
// mirror the committed JSON shape, NOT the server's typed models.
package methodcheck

// Verdict is the validation verdict. Verdict == VerdictPass ⟺ the Findings carry
// zero SeverityError entries. Ported verbatim from the aiarch
// artifactValidationEngine.
type Verdict int

const (
	VerdictUnknown Verdict = iota
	VerdictPass            // zero Error-severity findings
	VerdictFail            // ≥1 Error-severity finding
)

// Severity is a finding severity. Only SeverityError fails the verdict;
// Warning/Info ride along advisory. Ported verbatim.
type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarning
	SeverityError
)

// RuleID is the stable, namespaced id of a validation rule. The ids (VOL-TRACE,
// SYS-NOUP, ALIGN-MISSING-PKG, …) are part of the contract — kept byte-identical
// to the aiarch originals for the ported rules.
type RuleID string

// Location locates a finding within a typed model. NO Line field: the input is a
// decoded model, not bytes. Ported verbatim.
type Location struct {
	Ordinal int    `json:"ordinal"` // stable position used for deterministic finding ordering
	Section string `json:"section"` // human-readable locus, e.g. "core use case 3", "Objective 4"
}

// Finding is a single machine-checkable rule violation. Ported verbatim.
type Finding struct {
	RuleID   RuleID    `json:"ruleId"`
	Severity Severity  `json:"severity"`
	Message  string    `json:"message"`
	Location *Location `json:"location,omitempty"`
}

// ValidationResult is the whole output of a verb. A model that fails validation is
// a VerdictFail, NOT an error. Ported verbatim.
type ValidationResult struct {
	Verdict  Verdict
	Findings []Finding
}

// loc is a small constructor for an optional Location.
func loc(ordinal int, section string) *Location {
	return &Location{Ordinal: ordinal, Section: section}
}

// severityLabel renders a severity for diagnostics.
func severityLabel(s Severity) string {
	switch s {
	case SeverityError:
		return "ERROR"
	case SeverityWarning:
		return "WARNING"
	default:
		return "INFO"
	}
}
