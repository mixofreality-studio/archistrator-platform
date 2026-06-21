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

	path := filepath.Join(spec.RepoRoot, statePath)
	raw, err := os.ReadFile(path) //nolint:gosec // path is RepoRoot-joined to a constant
	if err != nil {
		if os.IsNotExist(err) {
			t.Logf("methodcheck: no %s under %s — nothing to validate (clean pass)", statePath, spec.RepoRoot)
			return
		}
		t.Fatalf("methodcheck: cannot read %s: %v", path, err)
	}

	proj, ok, err := DecodeProject(raw)
	if err != nil {
		t.Fatalf("methodcheck: cannot decode %s: %v", statePath, err)
	}
	if !ok {
		t.Logf("methodcheck: %s is empty — nothing to validate (clean pass)", statePath)
		return
	}

	// ---- design rules ----
	findings, runErr := ValidateProject(proj)
	if runErr != nil {
		// A coherence fault (a dependent artifact committed without its prerequisite)
		// is a validation FAILURE in CI terms — the committed JSON is not a coherent
		// Method artifact set. Surface it as a failure with the engine message.
		var cm *ContractMisuseError
		if errors.As(runErr, &cm) {
			t.Errorf("methodcheck: committed state is not a coherent artifact set: %v", runErr)
		} else {
			t.Fatalf("methodcheck: %v", runErr)
		}
	}
	reportFindings(t, findings)

	// ---- arch layer rules + design↔code alignment (only when code is present) ----
	//
	// One Check call covers everything: the design-JSON rules above always run;
	// when the consuming module supplies an arch.Spec AND has actual Go code, this
	// block ALSO runs (a) the full arch LAYER rules (downward-only imports, no
	// sideways, Temporal isolation, interface naming/returns, dependency allowlist —
	// via arch.Check) and (b) the design↔code alignment pass. In the pure design
	// phase (no code yet) the package set is empty, so BOTH are skipped — the design
	// rules already ran, and arch.Check must NOT be called on a zero-package set (it
	// t.Fatalf's, treating an empty load as a vacuous-pass bug).
	committedSlots := proj.committedSlotCount()
	loadedPkgs := 0
	if len(spec.Arch.Patterns) > 0 {
		pkgs, lErr := loadClassifiedPackages(spec.Arch)
		if lErr != nil {
			t.Fatalf("methodcheck: load packages for alignment: %v", lErr)
		}
		loadedPkgs = len(pkgs)

		// Only run the layer + alignment checks when the module actually has code.
		// An empty classified-package set IS the pure design phase; skip both.
		if loadedPkgs > 0 {
			// (a) Full arch layer rules. arch.Check re-loads the module itself and
			// runs the structural suite; it is the authority on layering/naming/
			// Temporal/allowlist and reports its own violations via t.Errorf.
			arch.Check(t, spec.Arch)

			// (b) Design↔code alignment — only when a System is committed to align
			// against. With code but no committed System the layer rules above still
			// ran; the alignment pass simply has nothing to cross-reference.
			if sys, sysOK, sErr := proj.system(); sErr != nil {
				t.Fatalf("methodcheck: decode System for alignment: %v", sErr)
			} else if sysOK {
				alignFindings := alignSystemToCode(sys, pkgs, spec.NameNormalizer)
				reportFindings(t, alignFindings)
			}
		}
	}

	// ---- vacuous-pass guard ----
	// If the JSON decoded but carries zero committed slots AND no code packages were
	// loaded, nothing was actually checked. Don't fail (a clean empty repo is a legit
	// pass), but log a clear note so a vacuous run is never mistaken for a real one —
	// matching the CLI's clean-pass-on-empty while staying loud about it.
	if committedSlots == 0 && loadedPkgs == 0 {
		t.Logf("methodcheck: project.json decoded but has zero committed slots and no code packages were loaded — nothing was validated")
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
