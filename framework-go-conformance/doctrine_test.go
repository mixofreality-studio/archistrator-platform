package conformance

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
)

// declaresTest reports whether a *_test.go file of dir declares `func <name>(t *testing.T)`.
func declaresTest(t *testing.T, dir, name string) bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^func ` + regexp.QuoteMeta(name) + `\(t \*testing\.T\)`)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if re.Match(b) {
			return true
		}
	}
	return false
}

// Every builtApp doctrine entry is enforced by a rule that exists and fires, or carries a waiver
// that has not expired. A methodcheck rule must be one the validator emits, an arch rule one the
// arch gate can carry, and every firing test must exist in the platform checkout: the firing test
// is the rule's violating fixture (GC-4), so a citation to a test that is not there is a claim with
// nothing behind it.
func TestRegistry_BuiltAppRulesEnforcedOrWaived(t *testing.T) {
	root, ok := platformRoot()
	if !ok {
		t.Skip("no platform checkout: the firing tests are resolved against the source tree")
	}
	entries, err := methodassets.DoctrineRegistry()
	if err != nil {
		t.Fatalf("the doctrine registry must decode: %v", err)
	}
	emitted := methodcheck.EmittedRuleIDs()
	archIDs := arch.RuleIDs()
	today := time.Now().Format("2006-01-02")

	for _, e := range entries {
		if e.Scope == methodassets.ScopeBuiltApp && len(e.EnforcedBy) == 0 && e.Waiver == nil {
			t.Errorf("%s: a built-app rule needs an enforcing rule or a dated waiver", e.ID)
		}
		if e.Waiver != nil && e.Waiver.Expires < today {
			t.Errorf("%s: the waiver expired on %s (owner %s): enforce the rule or renew the waiver with a new date",
				e.ID, e.Waiver.Expires, e.Waiver.Owner)
		}
		for _, r := range e.EnforcedBy {
			switch v := r.(type) {
			case methodassets.MethodcheckRule:
				if !slices.Contains(emitted, methodcheck.RuleID(v.Rule)) {
					t.Errorf("%s: methodcheck rule %s is not in methodcheck.EmittedRuleIDs", e.ID, v.Rule)
				}
				if !declaresTest(t, filepath.Join(root, "framework-go", "methodcheck"), v.FiredBy) {
					t.Errorf("%s: firing test %s is not declared in framework-go/methodcheck", e.ID, v.FiredBy)
				}
			case methodassets.ArchRule:
				if !slices.Contains(archIDs, v.Rule) {
					t.Errorf("%s: arch rule %s is not in arch.RuleIDs", e.ID, v.Rule)
				}
				if !declaresTest(t, filepath.Join(root, "framework-go", "arch"), v.FiredBy) {
					t.Errorf("%s: firing test %s is not declared in framework-go/arch", e.ID, v.FiredBy)
				}
			case methodassets.GeneratorInvariant:
				dir := filepath.Join(root, filepath.FromSlash(v.Module), filepath.FromSlash(v.Package))
				if _, err := os.Stat(dir); err != nil {
					t.Errorf("%s: invariant package %s/%s does not exist", e.ID, v.Module, v.Package)
					continue
				}
				if !declaresTest(t, dir, v.Test) {
					t.Errorf("%s: invariant test %s is not declared in %s/%s", e.ID, v.Test, v.Module, v.Package)
				}
			}
		}
	}
}
