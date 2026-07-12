package methodcheck

import "testing"

// align_sharedpkg_test.go exercises the shared-goPackage secondary alignment: a
// component whose OWN normalized name matches no code package, but whose slot-5
// contractKey resolves (via .serviceContracts[contractKey].goPackage) to a package
// another component already owns. This is the RA-promotion shape: several
// ResourceAccess components front one git-as-DB aggregate package (e.g.
// ConstructionTransitionAccess, GitActivityStatusAccess, DesignSessionAccess and the
// primary ProjectStateAccess all share internal/resourceaccess/projectstate).
//
// Name-matched alignment stays PRIMARY and unconditional; the contractKey join is
// consulted ONLY when the primary name match fails. A contractKey naming no
// .serviceContracts entry, or a goPackage naming no loaded code package, must still
// be a loud ALIGN-MISSING-PKG — never a silent pass.

// sharedPkgSystem returns a System with a PRIMARY ResourceAccess component
// (ProjectStateAccess, name-matches the "projectstate" package) plus a SECONDARY
// ResourceAccess component (ConstructionTransitionAccess) that owns no package of its
// own but carries contractKey "constructionTransitionAccess".
func sharedPkgSystem(t *testing.T) System {
	t.Helper()
	primary := comp(t, "ProjectStateAccess", kindResourceAccess)
	secondary := comp(t, "ConstructionTransitionAccess", kindResourceAccess)
	secondary.ContractKey = "constructionTransitionAccess"
	return System{Components: []Component{primary, secondary}}
}

// sharedPkgContracts is the .serviceContracts fixture backing sharedPkgSystem: the
// secondary's contract shares goPackage with the primary's real package.
func sharedPkgContracts() map[string]ServiceContract {
	return map[string]ServiceContract{
		"constructionTransitionAccess": {
			Component: "ConstructionTransitionAccess",
			Layer:     layerResourceAccess,
			GoPackage: "internal/resourceaccess/projectstate",
		},
	}
}

// sharedPkgPkgs is the synthetic loaded-package set: ONE ResourceAccess package,
// projectstate, whose full import path carries the internal/... module-relative
// suffix a real packages.Load walk would produce.
func sharedPkgPkgs() []classifiedPackage {
	return []classifiedPackage{
		cpkg("example.com/app/internal/resourceaccess/projectstate", "projectstate", "ResourceAccess"),
	}
}

// TestAlign_SharedGoPackage_MissingBeforeJoin is the RED fixture: today,
// alignSystemToCode has no way to consult ContractKey, so the secondary component
// (which owns no "constructiontransition" package) is reported ALIGN-MISSING-PKG even
// though its contract's goPackage IS backed by a loaded package. This test pins the
// pre-join behavior; TestAlign_SharedGoPackage_Aligned pins the post-join behavior.
func TestAlign_SharedGoPackage_MissingBeforeJoin(t *testing.T) {
	s := sharedPkgSystem(t)
	pkgs := sharedPkgPkgs()
	got := alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, nil)
	if !hasRuleFindings(got, ruleAlignMissingPkg) {
		t.Fatalf("without a contracts join, a name-unmatched secondary component must still emit ALIGN-MISSING-PKG, got %+v", got)
	}
}

// TestAlign_SharedGoPackage_Aligned proves the join: passing the .serviceContracts map
// resolves the secondary's contractKey to a goPackage a loaded package realizes, so the
// secondary is ALIGNED (no findings at all — the primary matches by name, the
// secondary matches via the join).
func TestAlign_SharedGoPackage_Aligned(t *testing.T) {
	s := sharedPkgSystem(t)
	pkgs := sharedPkgPkgs()
	contracts := sharedPkgContracts()
	got := alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, contracts)
	if len(got) != 0 {
		t.Fatalf("a secondary component whose contractKey joins to an existing goPackage must align cleanly, got %+v", got)
	}
}

// TestAlign_SharedGoPackage_NamePrimaryStillWins: when a component's OWN name matches
// a package directly, that primary match is used even if it ALSO carries a
// contractKey — the join is a fallback, never overrides a real name match.
func TestAlign_SharedGoPackage_NamePrimaryStillWins(t *testing.T) {
	primary := comp(t, "ProjectStateAccess", kindResourceAccess)
	primary.ContractKey = "projectStateAccess"
	s := System{Components: []Component{primary}}
	pkgs := sharedPkgPkgs()
	contracts := map[string]ServiceContract{
		"projectStateAccess": {Component: "ProjectStateAccess", Layer: layerResourceAccess, GoPackage: "internal/resourceaccess/DOES-NOT-EXIST"},
	}
	got := alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, contracts)
	if len(got) != 0 {
		t.Fatalf("a component whose own name matches a package must align by name regardless of a (even broken) contractKey join, got %+v", got)
	}
}

// TestAlign_SharedGoPackage_ContractKeyMissing_StillErrors: a secondary component
// whose contractKey names NO .serviceContracts entry must still be a loud
// ALIGN-MISSING-PKG — never a silent pass just because a contractKey is present.
func TestAlign_SharedGoPackage_ContractKeyMissing_StillErrors(t *testing.T) {
	secondary := comp(t, "ConstructionTransitionAccess", kindResourceAccess)
	secondary.ContractKey = "constructionTransitionAccess"
	s := System{Components: []Component{secondary}}
	pkgs := sharedPkgPkgs()
	// No contracts map entry for "constructionTransitionAccess" at all.
	got := alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, nil)
	if !hasRuleFindings(got, ruleAlignMissingPkg) {
		t.Fatalf("a contractKey naming no .serviceContracts entry must still emit ALIGN-MISSING-PKG, got %+v", got)
	}
}

// TestAlign_SharedGoPackage_GoPackageNotOnDisk_StillErrors: the negative join case —
// contractKey resolves to a real ServiceContract, but its goPackage names NO loaded
// code package. This must still be a loud ALIGN-MISSING-PKG, not a silent alignment.
func TestAlign_SharedGoPackage_GoPackageNotOnDisk_StillErrors(t *testing.T) {
	secondary := comp(t, "ConstructionTransitionAccess", kindResourceAccess)
	secondary.ContractKey = "constructionTransitionAccess"
	s := System{Components: []Component{secondary}}
	pkgs := sharedPkgPkgs() // carries "projectstate", NOT "ghoststate"
	contracts := map[string]ServiceContract{
		"constructionTransitionAccess": {
			Component: "ConstructionTransitionAccess",
			Layer:     layerResourceAccess,
			GoPackage: "internal/resourceaccess/ghoststate",
		},
	}
	got := alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, contracts)
	if !hasRuleFindings(got, ruleAlignMissingPkg) {
		t.Fatalf("a contractKey whose goPackage names no loaded package must still emit ALIGN-MISSING-PKG, got %+v", got)
	}
}

// TestAlign_SharedGoPackage_NoGoPackage_FallsThroughToMissing: a contract that exists
// but carries no goPackage at all (nothing to join on) falls through to the ordinary
// missing-package error rather than panicking or silently passing.
func TestAlign_SharedGoPackage_NoGoPackage_FallsThroughToMissing(t *testing.T) {
	secondary := comp(t, "ConstructionTransitionAccess", kindResourceAccess)
	secondary.ContractKey = "constructionTransitionAccess"
	s := System{Components: []Component{secondary}}
	pkgs := sharedPkgPkgs()
	contracts := map[string]ServiceContract{
		"constructionTransitionAccess": {Component: "ConstructionTransitionAccess", Layer: layerResourceAccess},
	}
	got := alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, contracts)
	if !hasRuleFindings(got, ruleAlignMissingPkg) {
		t.Fatalf("a contract with no goPackage must fall through to ALIGN-MISSING-PKG, got %+v", got)
	}
}

// TestAlign_SharedGoPackage_OrphanNotDoubleCounted: the primary component's package is
// marked matched once by the primary and once (idempotently) by the secondary's join —
// it must not spuriously read as ALIGN-EXTRA-PKG, and adding a genuinely unowned
// package must still trip ALIGN-EXTRA-PKG (the matched-marking is not a blanket
// suppressor).
func TestAlign_SharedGoPackage_OrphanNotDoubleCounted(t *testing.T) {
	s := sharedPkgSystem(t)
	pkgs := append(sharedPkgPkgs(), cpkg("example.com/app/internal/resourceaccess/ghost", "ghost", "ResourceAccess"))
	contracts := sharedPkgContracts()
	got := alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, contracts)
	if hasRuleFindings(got, ruleAlignMissingPkg) {
		t.Fatalf("primary + joined secondary must both align, got %+v", got)
	}
	if !hasRuleFindings(got, ruleAlignExtraPkg) {
		t.Fatalf("a genuinely orphaned package must still trip ALIGN-EXTRA-PKG, got %+v", got)
	}
}
