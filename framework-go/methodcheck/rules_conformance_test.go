package methodcheck

import "testing"

// rules_conformance_test.go exercises the code↔model conformance rules over
// SYNTHETIC classifiedPackages (constructed in-package, imports set by hand) so the
// rule logic is tested without loading a real module — mirroring how the rule-suite
// tests construct System values directly.

// cpkg builds a classifiedPackage whose leaf matches a component name (so the
// default normalizer pairs them) with the given import paths.
func cpkg(pkgPath, leaf, layer string, imports ...string) classifiedPackage {
	return classifiedPackage{pkgPath: pkgPath, leaf: leaf, layer: layer, imports: imports}
}

// conformanceSystem is a legal M→E, M→RA chain used as the conformance baseline.
func conformanceSystem(t *testing.T) System {
	t.Helper()
	mgr := comp(t, "DesignManager", kindManager)
	eng := comp(t, "ValidatingEngine", kindEngine)
	ra := comp(t, "StateAccess", kindResourceAccess)
	return System{
		Components: []Component{mgr, eng, ra},
		Relationships: []Relationship{
			{From: mgr.ID, To: eng.ID, Mode: modeSync},
			{From: mgr.ID, To: ra.ID, Mode: modeSync},
		},
	}
}

const (
	pkgMgr = "ex/manager/designmanager"
	pkgEng = "ex/engine/validatingengine"
	pkgRA  = "ex/resourceaccess/stateaccess"
)

// conformancePkgsBacked returns packages whose imports realize BOTH declared sync
// edges (Manager→Engine, Manager→ResourceAccess).
func conformancePkgsBacked() []classifiedPackage {
	return []classifiedPackage{
		cpkg(pkgMgr, "designmanager", "Manager", pkgEng, pkgRA),
		cpkg(pkgEng, "validatingengine", "Engine"),
		cpkg(pkgRA, "stateaccess", "ResourceAccess"),
	}
}

func TestConformance_Backed_NoFindings(t *testing.T) {
	if f := conformanceCheck(conformanceSystem(t), conformancePkgsBacked(), nil); len(f) != 0 {
		t.Fatalf("a system whose imports back every declared sync edge must produce zero findings, got %+v", f)
	}
}

func TestConformance_CodeEdgeNotInModel_Fails(t *testing.T) {
	s := conformanceSystem(t)
	// The engine imports the RA — a real code edge — but no such relationship is declared.
	pkgs := []classifiedPackage{
		cpkg(pkgMgr, "designmanager", "Manager", pkgEng, pkgRA),
		cpkg(pkgEng, "validatingengine", "Engine", pkgRA), // undeclared engine→RA import
		cpkg(pkgRA, "stateaccess", "ResourceAccess"),
	}
	sev, ok := findingSeverity(conformanceCheck(s, pkgs, nil), ruleCodeEdgeNotInModel)
	if !ok {
		t.Fatalf("expected CODE-EDGE-NOT-IN-MODEL for an undeclared import edge")
	}
	if sev != SeverityError {
		t.Fatalf("CODE-EDGE-NOT-IN-MODEL must be Error, got %v", sev)
	}
}

func TestConformance_ModelEdgeNotInCode_Warns(t *testing.T) {
	s := conformanceSystem(t)
	// The manager imports the engine but NOT the RA, though the model declares M→RA sync.
	pkgs := []classifiedPackage{
		cpkg(pkgMgr, "designmanager", "Manager", pkgEng),
		cpkg(pkgEng, "validatingengine", "Engine"),
		cpkg(pkgRA, "stateaccess", "ResourceAccess"),
	}
	sev, ok := findingSeverity(conformanceCheck(s, pkgs, nil), ruleModelEdgeNotInCode)
	if !ok {
		t.Fatalf("expected MODEL-EDGE-NOT-IN-CODE for a declared sync edge with no import")
	}
	if sev != SeverityWarning {
		t.Fatalf("MODEL-EDGE-NOT-IN-CODE must be Warning, got %v", sev)
	}
}

func TestConformance_ClientManagerWireExempt(t *testing.T) {
	client := comp(t, "AppClient", kindClient)
	mgr := comp(t, "DesignManager", kindManager)
	s := System{
		Components:    []Component{client, mgr},
		Relationships: []Relationship{{From: client.ID, To: mgr.ID, Mode: modeSync}},
	}
	// Client and Manager both implemented, but the client does NOT import the manager
	// (transport-mediated wire). MODEL-EDGE-NOT-IN-CODE must NOT fire.
	pkgs := []classifiedPackage{
		cpkg("ex/client/appclient", "appclient", "Client"),
		cpkg(pkgMgr, "designmanager", "Manager"),
	}
	if hasRuleFindings(conformanceCheck(s, pkgs, nil), ruleModelEdgeNotInCode) {
		t.Fatalf("Client→Manager is a transport wire, exempt from MODEL-EDGE-NOT-IN-CODE")
	}
}

func TestConformance_QueuedModeExempt(t *testing.T) {
	mgr := comp(t, "DesignManager", kindManager)
	eng := comp(t, "ValidatingEngine", kindEngine)
	// A queued edge is substrate-mediated, not a Go import — even to an Engine.
	s := System{
		Components:    []Component{mgr, eng},
		Relationships: []Relationship{{From: mgr.ID, To: eng.ID, Mode: modeQueued}},
	}
	pkgs := []classifiedPackage{
		cpkg(pkgMgr, "designmanager", "Manager"),
		cpkg(pkgEng, "validatingengine", "Engine"),
	}
	if hasRuleFindings(conformanceCheck(s, pkgs, nil), ruleModelEdgeNotInCode) {
		t.Fatalf("a queued relationship is substrate-mediated, exempt from MODEL-EDGE-NOT-IN-CODE")
	}
}

func TestConformance_UtilityImportRequired(t *testing.T) {
	mgr := comp(t, "DesignManager", kindManager)
	util := comp(t, "LoggingUtility", kindUtility)
	s := System{
		Components:    []Component{mgr, util},
		Relationships: []Relationship{{From: mgr.ID, To: util.ID, Mode: modeSync}},
	}
	// Manager declares a sync call to the Utility but does not import it → Warning.
	pkgs := []classifiedPackage{
		cpkg(pkgMgr, "designmanager", "Manager"),
		cpkg("ex/utility/loggingutility", "loggingutility", "Utility"),
	}
	if !hasRuleFindings(conformanceCheck(s, pkgs, nil), ruleModelEdgeNotInCode) {
		t.Fatalf("a sync call to a Utility must be backed by an import; expected MODEL-EDGE-NOT-IN-CODE")
	}
}

func TestConformance_UnimplementedSideSkipsModelEdge(t *testing.T) {
	s := conformanceSystem(t)
	// Only the manager package is loaded; the engine + RA have no code yet, so the
	// declared M→E / M→RA edges have nothing to back them with — no finding.
	pkgs := []classifiedPackage{cpkg(pkgMgr, "designmanager", "Manager")}
	if hasRuleFindings(conformanceCheck(s, pkgs, nil), ruleModelEdgeNotInCode) {
		t.Fatalf("a declared edge whose target has no code package must not trip MODEL-EDGE-NOT-IN-CODE")
	}
}

func TestConformance_EmptyPkgsIsNoOp(t *testing.T) {
	if f := conformanceCheck(conformanceSystem(t), nil, nil); len(f) != 0 {
		t.Fatalf("design phase (no packages) must produce zero conformance findings, got %+v", f)
	}
}
