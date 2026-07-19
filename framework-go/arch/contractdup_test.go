package arch

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// contractdup_test.go exercises the contract-duplication gate against the
// testdata contractdupapp module:
//   - cleanaccess: a generated contract + a legit narrow accepted-interface
//     consumer in the SAME package → no findings.
//   - dupaccess: an exported hand interface whose FULL method set duplicates
//     its own package's generated contract → rule c fires.
//   - sourceaccess/redeclengine: an exported interface in a DIFFERENT package
//     duplicates sourceaccess's generated contract → rule d fires.
//   - durableexecution/opsmanager: the real app's durableExecutionAccess
//     shape — a narrow 1-of-4 seam with a local mirror type and
//     context.Context — must NOT fire either rule (the regression case).
//   - strategyengine: an unexported internal strategy axis with the same
//     method COUNT as its own generated contract, different signatures/names
//     → must not fire (also protected by being unexported).
//   - cleanaccess.ExportedNarrowFetcher: an EXPORTED 1-of-2-method subset of
//     CleanAccess (same package, so it DOES reach ifaceMethodSetEqual as a
//     rule-c candidate) → must not fire, proving the method-NAME-SET
//     (count) gate discriminates even when visibility can't exclude it.
//   - dupaccess.ExportedMirrorTypeAccess: an EXPORTED same-method-names
//     interface using a local mirror type (MirrorItem) instead of the
//     generated Item type → must not fire rule c, proving types.Identical
//     signature equality — not a name/count-only compare — gates the match.
//   - redeclengine.ForeignMirrorTypeAccess: the same mirror-type near-miss as
//     above, but cross-package against sourceaccess's generated SourceAccess
//     contract → must not fire rule d, proving the signature-equality gate
//     also holds on the cross-package path.
//
// The pure core (contractDuplicationViolations) is tested directly so
// violations are OBSERVED rather than routed to a failing t.Errorf, matching
// the gensurface_test.go / filelayout_test.go pattern. The public
// CheckContractDuplication wiring is exercised on the passing (clean) path.

const contractdupPrefix = "example.com/contractdupapp/internal/"

func contractdupSpec() Spec {
	return Spec{
		ModuleRoot:   "testdata/contractdupapp",
		ModulePrefix: contractdupPrefix,
		Patterns:     []string{"./internal/..."},
	}
}

// loadContractdupPkgs loads the testdata module (a nested module → GOWORK=off)
// with the rich mode CheckContractDuplication uses.
func loadContractdupPkgs(t *testing.T) []*packages.Package {
	t.Helper()
	t.Setenv("GOWORK", "off")
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:   "testdata/contractdupapp",
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, "./internal/...")
	if err != nil {
		t.Fatalf("load contractdupapp: %v", err)
	}
	if n := packages.PrintErrors(pkgs); n > 0 {
		t.Fatalf("%d load error(s) in contractdupapp", n)
	}
	if len(pkgs) == 0 {
		t.Fatal("contractdupapp loaded zero packages; fixture missing")
	}
	return pkgs
}

func hasContractDupViolation(vs []contractDupViolation, pkgSuffix, iface, rule string) bool {
	for _, v := range vs {
		if strings.HasSuffix(v.Pkg, pkgSuffix) && v.Iface == iface && v.Rule == rule {
			return true
		}
	}
	return false
}

func TestContractDup_RuleCFiresOnSamePackageDuplicate(t *testing.T) {
	pkgs := loadContractdupPkgs(t)
	vs := contractDuplicationViolations(pkgs)
	if !hasContractDupViolation(vs, "resourceaccess/dupaccess", "HandDupAccess", ruleNoExportedHandIface) {
		t.Errorf("expected rule c on dupaccess.HandDupAccess, got %+v", vs)
	}
}

func TestContractDup_RuleDFiresOnForeignPackageDuplicate(t *testing.T) {
	pkgs := loadContractdupPkgs(t)
	vs := contractDuplicationViolations(pkgs)
	if !hasContractDupViolation(vs, "engine/redeclengine", "ForeignSourceAccess", ruleNoForeignContractRedecl) {
		t.Errorf("expected rule d on redeclengine.ForeignSourceAccess, got %+v", vs)
	}
}

func TestContractDup_CleanPackageProducesNoFindings(t *testing.T) {
	pkgs := loadContractdupPkgs(t)
	vs := contractDuplicationViolations(pkgs)
	for _, v := range vs {
		if strings.HasSuffix(v.Pkg, "resourceaccess/cleanaccess") {
			t.Errorf("cleanaccess (contract + legit narrow subset) must produce no findings, got %+v", v)
		}
	}
}

// TestContractDup_DurableExecutionSeamDoesNotFire is the regression test
// modeled on the real app's durableExecutionAccess seam: a narrow 1-of-4
// accepted interface using a local mirror scheduleSpec type and
// context.Context instead of the generated Context/ScheduleSpec. Neither rule
// c nor rule d may fire on it, in either the opsmanager package (the
// candidate) or the durableexecution package (the foreign contract it must
// not be conflated with).
func TestContractDup_DurableExecutionSeamDoesNotFire(t *testing.T) {
	pkgs := loadContractdupPkgs(t)
	vs := contractDuplicationViolations(pkgs)
	for _, v := range vs {
		if strings.HasSuffix(v.Pkg, "manager/opsmanager") || strings.HasSuffix(v.Pkg, "resourceaccess/durableexecution") {
			t.Errorf("the narrow durableExecutionAccess seam must not fire either rule, got %+v", v)
		}
	}
}

// TestContractDup_SelfPackageStrategyDoesNotFire is the self-package
// exclusion regression: an unexported internal strategy interface with the
// same method COUNT as its own package's generated contract, but different
// names/signatures, must not fire rule c.
func TestContractDup_SelfPackageStrategyDoesNotFire(t *testing.T) {
	pkgs := loadContractdupPkgs(t)
	vs := contractDuplicationViolations(pkgs)
	for _, v := range vs {
		if strings.HasSuffix(v.Pkg, "engine/strategyengine") {
			t.Errorf("strategyengine's own generated contract and internal strategy axis must not fire, got %+v", v)
		}
	}
}

// TestContractDup_ExportedNarrowSubsetDoesNotFireRuleC is the count/name-set
// discrimination proof: ExportedNarrowFetcher is EXPORTED and lives in
// cleanaccess (a hasGeneratedFile package), so it genuinely reaches
// ifaceMethodSetEqual as a rule-c candidate — unlike recordFetcher, it is not
// excluded by visibility. It has 1 of CleanAccess's 2 methods (same Record
// type, same method name), so it must not fire: exact method-NAME-SET
// equality (count included) is what protects it, not unexported status.
func TestContractDup_ExportedNarrowSubsetDoesNotFireRuleC(t *testing.T) {
	pkgs := loadContractdupPkgs(t)
	vs := contractDuplicationViolations(pkgs)
	for _, v := range vs {
		if v.Iface == "ExportedNarrowFetcher" {
			t.Errorf("exported narrow subset ExportedNarrowFetcher must not fire any rule, got %+v", v)
		}
	}
}

// TestContractDup_MirrorTypeSameNamesDoesNotFireRuleC is THE critical
// signature-equality proof for rule c: ExportedMirrorTypeAccess is EXPORTED,
// lives in dupaccess (same package as the generated DupAccess contract), and
// has the SAME method names/arity (Fetch/Store) as DupAccess — so it reaches
// ifaceMethodSetEqual and clears the name-set gate. It differs only in using
// MirrorItem (a local mirror) instead of the generated Item type. It must not
// fire: this is only possible if ifaceMethodSetEqual truly calls
// types.Identical on parameter/result types rather than comparing names or
// counts alone.
func TestContractDup_MirrorTypeSameNamesDoesNotFireRuleC(t *testing.T) {
	pkgs := loadContractdupPkgs(t)
	vs := contractDuplicationViolations(pkgs)
	for _, v := range vs {
		if v.Iface == "ExportedMirrorTypeAccess" {
			t.Errorf("exported mirror-type near-miss ExportedMirrorTypeAccess must not fire any rule, got %+v", v)
		}
	}
	// Sanity: the real rogue duplicate in the same package must still fire,
	// so this test cannot pass merely because dupaccess produces no findings.
	if !hasContractDupViolation(vs, "resourceaccess/dupaccess", "HandDupAccess", ruleNoExportedHandIface) {
		t.Fatalf("expected rule c on dupaccess.HandDupAccess (sanity check), got %+v", vs)
	}
}

// TestContractDup_MirrorTypeSameNamesDoesNotFireRuleD is the cross-package
// counterpart: ForeignMirrorTypeAccess (in redeclengine) has the same method
// name/arity (Read) as sourceaccess's generated SourceAccess contract — so it
// clears rule d's name-set gate and reaches ifaceMethodSetEqual — but uses
// LocalBlob (a local mirror) instead of the generated Blob type. It must not
// fire, proving types.Identical signature equality gates rule d's
// cross-package match too, not just rule c's same-package one.
func TestContractDup_MirrorTypeSameNamesDoesNotFireRuleD(t *testing.T) {
	pkgs := loadContractdupPkgs(t)
	vs := contractDuplicationViolations(pkgs)
	for _, v := range vs {
		if v.Iface == "ForeignMirrorTypeAccess" {
			t.Errorf("exported mirror-type near-miss ForeignMirrorTypeAccess must not fire any rule, got %+v", v)
		}
	}
	// Sanity: the real rogue cross-package duplicate must still fire, so this
	// test cannot pass merely because redeclengine produces no findings.
	if !hasContractDupViolation(vs, "engine/redeclengine", "ForeignSourceAccess", ruleNoForeignContractRedecl) {
		t.Fatalf("expected rule d on redeclengine.ForeignSourceAccess (sanity check), got %+v", vs)
	}
}

// TestCheckContractDuplicationPassesClean drives the public entry point on a
// passing path: restricting Patterns to only the clean fixture packages must
// produce zero t.Errorf calls.
func TestCheckContractDuplicationPassesClean(t *testing.T) {
	t.Setenv("GOWORK", "off")
	spec := contractdupSpec()
	spec.Patterns = []string{
		"./internal/resourceaccess/cleanaccess/...",
		"./internal/resourceaccess/durableexecution/...",
		"./internal/manager/opsmanager/...",
		"./internal/engine/strategyengine/...",
	}
	CheckContractDuplication(t, spec)
}
