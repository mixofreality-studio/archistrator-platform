package methodcheck

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
)

// statePath is the committed typed-state document, relative to the repo root, that
// projectStateAccess's git store writes (gitstore.go statePathPrefix + projectFile).
// methodcheck reads it directly off the checked-out working tree — no git, no
// provider I/O — exactly as the aiarch-validate CLI does.
const statePath = ".aiarch/state/project.json"

// ProjectSpec parameterizes Check for one consuming module. It pairs the repo root
// (where .aiarch/state/project.json lives) with the arch.Spec that maps the module's
// directories to Method layers (reused for the code walk in the alignment pass) and
// an optional component↔package name normalizer.
type ProjectSpec struct {
	// RepoRoot is the directory containing the .aiarch/ tree (the checked-out repo
	// root). statePath is joined onto it.
	RepoRoot string

	// Arch is the layer model for BOTH the arch layer-rule pass and the alignment
	// code walk — the SAME Spec a module would otherwise pass to arch.Check. When
	// zero (empty Patterns), or when the module has no Go code yet (the pure design
	// phase), the arch layer rules + the alignment pass are skipped and only the
	// design rules run. With code present, Check runs arch.Check(t, Arch) so a single
	// methodcheck.Check call is the one-stop gate (design rules + layer rules +
	// alignment) and the consuming repo needs no separate arch test.
	Arch arch.Spec

	// NameNormalizer overrides the default component↔package match key derivation
	// (lowercase + strip non-alphanumeric). Optional.
	NameNormalizer func(string) string

	// EncapsulationAllowlist opts the consuming module INTO the generated-surface
	// encapsulation gate (arch.CheckGeneratedSurface): with Go code present, every
	// exported symbol in a generated-contract package (one carrying a *.gen.go file)
	// must be part of the generated surface or listed here. The map is keyed by the
	// package path relative to Arch.ModulePrefix (e.g. "engine/validatingengine")
	// → its allowed exported identifiers. A NON-NIL value (even an empty map) opts in;
	// nil (the default) leaves the gate off, so existing consumers are unaffected. The
	// app keeps its allowlist as data and passes it here.
	EncapsulationAllowlist map[string][]string
}

// Check is the all-in-one Method gate a consuming repo runs from one go test. It
// reads RepoRoot/.aiarch/state/project.json and runs, in one call:
//
//   - the Method-invariant DESIGN rules over the committed JSON (always);
//   - when the module has Go code, the full arch LAYER rules (downward-only imports,
//     no sideways, Temporal isolation, interface naming/returns, dependency
//     allowlist — via arch.Check); and
//   - when the module has Go code AND a committed System, the design↔code ALIGNMENT
//     pass.
//
// In the pure design phase (no code yet) the layer + alignment passes are skipped —
// the design rules already ran. It reports every Error-severity finding via
// t.Errorf; Warnings/Info are logged via t.Log and NEVER fail — matching the
// Engine's verdict rule (Verdict==Pass ⟺ zero Error findings) and the CLI's
// Warnings-ride-along behavior.
//
// A missing/empty project.json is NOT a failure (a repo that touches no .aiarch
// state must not be blocked) — it is logged and Check returns. It mirrors arch.Check
// in posture but is deliberately more forgiving about an absent design (the design
// may legitimately not exist yet).
func Check(t *testing.T, spec ProjectSpec) {
	t.Helper()
	proj, ok := readAndDecodeProject(t, spec)
	if !ok {
		return
	}
	findings, runErr := ValidateProject(proj)
	handleValidationError(t, runErr)
	reportFindings(t, findings)
	committedSlots := proj.committedSlotCount()
	loadedPkgs := runArchPhase(t, spec, proj)
	if committedSlots == 0 && loadedPkgs == 0 {
		t.Logf("methodcheck: project.json decoded but has zero committed slots and no code packages were loaded — nothing was validated")
	}
}

func readAndDecodeProject(t *testing.T, spec ProjectSpec) (Project, bool) {
	t.Helper()
	path := filepath.Join(spec.RepoRoot, statePath)
	raw, err := os.ReadFile(path) //nolint:gosec // path is RepoRoot-joined to a constant
	if err != nil {
		if os.IsNotExist(err) {
			t.Logf("methodcheck: no %s under %s — nothing to validate (clean pass)", statePath, spec.RepoRoot)
			return Project{}, false
		}
		t.Fatalf("methodcheck: cannot read %s: %v", path, err)
	}
	proj, ok, err := DecodeProject(raw)
	if err != nil {
		t.Fatalf("methodcheck: cannot decode %s: %v", statePath, err)
	}
	if !ok {
		t.Logf("methodcheck: %s is empty — nothing to validate (clean pass)", statePath)
	}
	return proj, ok
}

func handleValidationError(t *testing.T, runErr error) {
	t.Helper()
	if runErr == nil {
		return
	}
	// A coherence fault (a dependent artifact committed without its prerequisite)
	// is a validation FAILURE in CI terms — surface it as t.Errorf so the run
	// continues and reports all findings; everything else is t.Fatalf.
	var cm *ContractMisuseError
	if errors.As(runErr, &cm) {
		t.Errorf("methodcheck: committed state is not a coherent artifact set: %v", runErr)
	} else {
		t.Fatalf("methodcheck: %v", runErr)
	}
}

// runArchPhase runs (a) the arch LAYER rules and (b) the design↔code alignment
// when the module has Go code. Returns the number of classified packages loaded
// (0 = pure design phase, both passes skipped).
func runArchPhase(t *testing.T, spec ProjectSpec, proj Project) int {
	t.Helper()
	if len(spec.Arch.Patterns) == 0 {
		return 0
	}
	pkgs, lErr := loadClassifiedPackages(spec.Arch)
	if lErr != nil {
		t.Fatalf("methodcheck: load packages for alignment: %v", lErr)
	}
	if len(pkgs) > 0 {
		runLayerAndAlignmentChecks(t, spec, proj, pkgs)
	}
	return len(pkgs)
}

// runLayerAndAlignmentChecks runs (a) the full arch layer rules and (b) the
// design↔code alignment pass when the module has Go code. Separated from Check
// to reduce nesting depth.
func runLayerAndAlignmentChecks(t *testing.T, spec ProjectSpec, proj Project, pkgs []classifiedPackage) {
	t.Helper()
	// (a) Full arch layer rules. arch.Check re-loads the module itself and
	// runs the structural suite; it is the authority on layering/naming/
	// Temporal/allowlist and reports its own violations via t.Errorf.
	arch.Check(t, spec.Arch)
	// (a′) Encapsulation gate — only when the spec opts in with an allowlist.
	// arch.CheckGeneratedSurface re-loads the module and fails any exported symbol
	// outside the generated contract surface + allowlist.
	if spec.EncapsulationAllowlist != nil {
		arch.CheckGeneratedSurface(t, spec.Arch, spec.EncapsulationAllowlist)
	}
	// (b) Design↔code alignment — only when a System is committed to align
	// against. With code but no committed System the layer rules above still
	// ran; the alignment pass simply has nothing to cross-reference.
	sys, sysOK, sErr := proj.system()
	if sErr != nil {
		t.Fatalf("methodcheck: decode System for alignment: %v", sErr)
	}
	if sysOK {
		reportFindings(t, alignSystemToCode(sys, pkgs, spec.NameNormalizer))
		reportFindings(t, conformanceCheck(sys, pkgs, spec.NameNormalizer))
	}
}

// reportFindings routes findings to the test: Error-severity → t.Errorf (fails the
// test), Warning/Info → t.Logf (advisory, never fails). This IS the Engine verdict
// rule, applied at the test boundary.
func reportFindings(t *testing.T, findings []Finding) {
	t.Helper()
	for _, f := range findings {
		msg := formatFinding(f)
		if f.Severity == SeverityError {
			t.Errorf("%s", msg)
		} else {
			t.Logf("%s", msg)
		}
	}
}

func formatFinding(f Finding) string {
	at := ""
	if f.Location != nil && f.Location.Section != "" {
		at = "  (at " + f.Location.Section + ")"
	}
	return severityLabel(f.Severity) + "  [" + string(f.RuleID) + "]  " + f.Message + at
}
