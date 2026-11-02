package methodcheck

import (
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// align_buildstatus_test.go exercises the ALIGN-gate mechanisms layered on top of the
// design↔code alignment pass: the StereotypeSuffixNormalizer, layer-scoped matching,
// and the buildStatus-aware rules (ALIGN-STALE-PLANNED, ALIGN-EXTERNAL-NONUTILITY,
// ALIGN-EXTERNAL-UNWIRED). Packages are synthetic classifiedPackages (via cpkg) so the
// rule logic is exercised without loading a real module.

func TestStereotypeSuffixNormalizer(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Settlement", "settlement"},        // non-suffixed — unchanged
		{"SettlementManager", "settlement"}, // strip trailing manager
		{"SettlementEngine", "settlement"},  // strip trailing engine
		{"StateAccess", "state"},            // ResourceAccess carries the "access" suffix
		{"AppClient", "app"},                // strip trailing client
		{"MCPClient", "mcp"},                // strip client after non-alnum removal
		{"Manager", "manager"},              // bare suffix must NOT collapse to ""
		{"Client", "client"},                // bare suffix
		{"Engine", "engine"},                // bare suffix
		{"Access", "access"},                // bare suffix
		{"Security", "security"},            // non-suffixed framework utility — not ""
		{"AccessManager", "access"},         // double suffix: strip exactly ONE (manager)
		{"Order-Manager", "order"},          // non-alnum stripped, then suffix
		{"", ""},                            // empty stays empty
	}
	for _, c := range cases {
		if got := StereotypeSuffixNormalizer(c.in); got != c.want {
			t.Errorf("StereotypeSuffixNormalizer(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---- Layer-scoped matching (regression: same leaf, two layers) ----

// TestAlign_LayerScoped_SameLeafTwoLayers proves that with a suffix-stripping
// normalizer, SettlementManager and SettlementEngine (both normalizing to
// "settlement") match their OWN layer's package rather than colliding — both are
// matched, so no missing/mismatch/extra findings.
func TestAlign_LayerScoped_SameLeafTwoLayers(t *testing.T) {
	s := System{Components: []Component{
		comp(t, "SettlementManager", kindManager),
		comp(t, "SettlementEngine", kindEngine),
	}}
	pkgs := []classifiedPackage{
		cpkg("ex/manager/settlement", "settlement", "Manager"),
		cpkg("ex/engine/settlement", "settlement", "Engine"),
	}
	if f := alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, nil, nil, nil); len(f) != 0 {
		t.Fatalf("same-leaf packages in two layers must both match under layer-scoped keys, got %+v", f)
	}
}

// TestConformance_LayerScoped_SameLeafTwoLayers is the conformance-pass regression:
// the Manager→Engine import between manager/settlement and engine/settlement must be
// attributed as an edge BETWEEN the two distinct components (backing the declared sync
// edge). Pre-fix (leaf-only key) both packages collapsed to one component, the edge's
// endpoints were equal and dropped, and MODEL-EDGE-NOT-IN-CODE spuriously fired.
func TestConformance_LayerScoped_SameLeafTwoLayers(t *testing.T) {
	mgr := comp(t, "SettlementManager", kindManager)
	eng := comp(t, "SettlementEngine", kindEngine)
	s := System{
		Components:    []Component{mgr, eng},
		Relationships: []Relationship{{From: mgr.ID, To: eng.ID, Mode: modeSync}},
	}
	pkgs := []classifiedPackage{
		cpkg("ex/manager/settlement", "settlement", "Manager", "ex/engine/settlement"),
		cpkg("ex/engine/settlement", "settlement", "Engine"),
	}
	if f := conformanceCheck(s, pkgs, StereotypeSuffixNormalizer); len(f) != 0 {
		t.Fatalf("layer-scoped keys must attribute the M→E import between the two settlement components, got %+v", f)
	}
}

// ---- ALIGN-STALE-PLANNED ----

// TestAlign_StalePlanned_ExactLeaf: a planned component whose leaf-named package
// already exists is stale.
func TestAlign_StalePlanned_ExactLeaf(t *testing.T) {
	mgr := comp(t, "DesignManager", kindManager)
	mgr.BuildStatus = buildStatusPlanned
	s := System{Components: []Component{mgr}}
	pkgs := []classifiedPackage{cpkg("ex/manager/design", "design", "Manager")}
	if !hasRuleFindings(alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, nil, nil, nil), ruleAlignStalePlanned) {
		t.Fatalf("a planned component with a matching package must emit ALIGN-STALE-PLANNED")
	}
}

// TestAlign_StalePlanned_MCPSubpackageShape: the MCPClient shape — generated
// subpackages under client/mcp/* with NO root package — is detected as "has a package"
// on the ancestor "mcp" directory segment.
func TestAlign_StalePlanned_MCPSubpackageShape(t *testing.T) {
	mcp := comp(t, "MCPClient", kindClient)
	mcp.BuildStatus = buildStatusPlanned
	s := System{Components: []Component{mcp}}
	pkgs := []classifiedPackage{
		cpkg("ex/client/mcp/designtools", "designtools", "Client"),
		cpkg("ex/client/mcp/statetools", "statetools", "Client"),
	}
	got := alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, nil, nil, nil)
	if !hasRuleFindings(got, ruleAlignStalePlanned) {
		t.Fatalf("a planned MCPClient with generated subpackages under client/mcp/* must emit ALIGN-STALE-PLANNED, got %+v", got)
	}
	// The stale subpackages map to the (planned) component, so they must NOT ALSO read
	// as orphaned.
	if hasRuleFindings(got, ruleAlignExtraPkg) {
		t.Fatalf("stale planned subpackages must not additionally trip ALIGN-EXTRA-PKG, got %+v", got)
	}
}

// TestAlign_Planned_NoPackage_NoMissing: a planned component with no package is the
// expected in-progress state — no ALIGN-MISSING-PKG, no ALIGN-STALE-PLANNED.
func TestAlign_Planned_NoPackage_NoMissing(t *testing.T) {
	built := comp(t, "DesignManager", kindManager)
	planned := comp(t, "PricingEngine", kindEngine)
	planned.BuildStatus = buildStatusPlanned
	s := System{Components: []Component{built, planned}}
	pkgs := []classifiedPackage{cpkg("ex/manager/design", "design", "Manager")}
	got := alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, nil, nil, nil)
	if hasRuleFindings(got, ruleAlignMissingPkg) {
		t.Fatalf("a planned component with no package must not emit ALIGN-MISSING-PKG, got %+v", got)
	}
	if hasRuleFindings(got, ruleAlignStalePlanned) {
		t.Fatalf("a planned component with no package must not emit ALIGN-STALE-PLANNED, got %+v", got)
	}
	if len(got) != 0 {
		t.Fatalf("built component matched + planned component unbuilt must be clean, got %+v", got)
	}
}

// ---- ALIGN-EXTERNAL-NONUTILITY / ALIGN-EXTERNAL-UNWIRED ----

// TestAlign_ExternalNonUtility: external is legal only for a Utility; an external
// Manager is a contract-misuse Error.
func TestAlign_ExternalNonUtility(t *testing.T) {
	built := comp(t, "DesignManager", kindManager)
	ext := comp(t, "SettlementEngine", kindEngine)
	ext.BuildStatus = buildStatusExternal
	s := System{Components: []Component{built, ext}}
	pkgs := []classifiedPackage{cpkg("ex/manager/design", "design", "Manager")}
	sev, ok := findingSeverity(alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, nil, nil, nil), ruleAlignExternalNonUtility)
	if !ok {
		t.Fatalf("an external non-Utility must emit ALIGN-EXTERNAL-NONUTILITY")
	}
	if sev != SeverityError {
		t.Fatalf("ALIGN-EXTERNAL-NONUTILITY must be Error, got %v", sev)
	}
}

// TestAlign_ExternalUtility_Wired: an external Utility that a loaded package imports
// from framework-go/utilities/<name> passes (provenance asserted).
func TestAlign_ExternalUtility_Wired(t *testing.T) {
	built := comp(t, "DesignManager", kindManager)
	sec := comp(t, "Security", kindUtility)
	sec.BuildStatus = buildStatusExternal
	s := System{Components: []Component{built, sec}}
	pkgs := []classifiedPackage{
		cpkg("ex/manager/design", "design", "Manager", "github.com/x/framework-go/utilities/security"),
	}
	got := alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, nil, nil, nil)
	if hasRuleFindings(got, ruleAlignExternalUnwired) || hasRuleFindings(got, ruleAlignExternalNonUtility) {
		t.Fatalf("a wired external Utility must produce no external finding, got %+v", got)
	}
}

// TestAlign_ExternalUtility_Unwired: an external Utility that NO loaded package imports
// from framework-go/utilities/… is a Warning — assert wired, don't waive.
func TestAlign_ExternalUtility_Unwired(t *testing.T) {
	built := comp(t, "DesignManager", kindManager)
	sec := comp(t, "Security", kindUtility)
	sec.BuildStatus = buildStatusExternal
	s := System{Components: []Component{built, sec}}
	pkgs := []classifiedPackage{cpkg("ex/manager/design", "design", "Manager")}
	sev, ok := findingSeverity(alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, nil, nil, nil), ruleAlignExternalUnwired)
	if !ok {
		t.Fatalf("an unwired external Utility must emit ALIGN-EXTERNAL-UNWIRED")
	}
	if sev != SeverityWarning {
		t.Fatalf("ALIGN-EXTERNAL-UNWIRED must be Warning, got %v", sev)
	}
}

// ---- DV-STATIC-COVERAGE planned skip ----

func TestDVStaticCoverage_PlannedSkipped(t *testing.T) {
	mgr := comp(t, "OrderManager", kindManager)
	eng := comp(t, "PricingEngine", kindEngine)
	eng.BuildStatus = buildStatusPlanned
	s := System{
		Components: []Component{mgr, eng},
		// A self-loop step marks mgr as a step-keyed model "participant" (participantIDs
		// derives participation from call endpoints, not a separate declared list) without
		// asserting anything about mgr's own coverage — this fixture only isolates eng.
		DynamicViews: []DynamicView{{Key: "k1", UseCaseID: "u1", Steps: []CallStep{{Calls: []TraceCall{{From: mgr.ID, To: mgr.ID, Mode: modeSync}}}}}},
	}
	got := checkStaticParticipationCoverage(s)
	if hasRuleFindings(got, ruleDVStaticCoverage) {
		t.Fatalf("a planned component must be exempt from DV-STATIC-COVERAGE, got %+v", got)
	}
	if !hasRuleFindings(got, ruleDVPlannedSkipped) {
		t.Fatalf("the planned exemption must be surfaced as DV-PLANNED-SKIPPED (Info), got %+v", got)
	}
	sev, _ := findingSeverity(got, ruleDVPlannedSkipped)
	if sev != SeverityInfo {
		t.Fatalf("DV-PLANNED-SKIPPED must be Info, got %v", sev)
	}
}

// TestDVStaticCoverage_BuiltStillFlagged is the passing control: a BUILT (absent
// buildStatus) core component in no view still trips DV-STATIC-COVERAGE.
func TestDVStaticCoverage_BuiltStillFlagged(t *testing.T) {
	mgr := comp(t, "OrderManager", kindManager)
	eng := comp(t, "PricingEngine", kindEngine) // built, participates in nothing
	s := System{
		Components: []Component{mgr, eng},
		// A self-loop step marks mgr as a step-keyed model "participant" (participantIDs
		// derives participation from call endpoints, not a separate declared list) without
		// asserting anything about mgr's own coverage — this fixture only isolates eng.
		DynamicViews: []DynamicView{{Key: "k1", UseCaseID: "u1", Steps: []CallStep{{Calls: []TraceCall{{From: mgr.ID, To: mgr.ID, Mode: modeSync}}}}}},
	}
	got := checkStaticParticipationCoverage(s)
	if !hasRuleFindings(got, ruleDVStaticCoverage) {
		t.Fatalf("a built core component in no view must trip DV-STATIC-COVERAGE, got %+v", got)
	}
	if hasRuleFindings(got, ruleDVPlannedSkipped) {
		t.Fatalf("no planned components → no DV-PLANNED-SKIPPED, got %+v", got)
	}
}

// ---- DEP-COVERAGE planned skip ----

func TestDeploymentCoverage_PlannedSkipped(t *testing.T) {
	base := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, base) // containers/environments cover ONLY the base
	future := comp(t, "FutureManager", kindManager)
	future.BuildStatus = buildStatusPlanned
	// The planned component is added to the System but packaged into NO container.
	s := System{Components: append(append([]Component{}, base.Components...), future)}
	got := deploymentConsistency(op, s)
	if hasRuleFindings(got, ruleDepCoverage) {
		t.Fatalf("a planned component must be exempt from DEP-COVERAGE, got %+v", got)
	}
	if hasRuleFindings(got, ruleDepGraphIdentity) {
		t.Fatalf("a planned component must not trip DEP-GRAPH-IDENTITY either, got %+v", got)
	}
	if !hasRuleFindings(got, ruleDepPlannedSkipped) {
		t.Fatalf("the planned exemption must be surfaced as DEP-PLANNED-SKIPPED (Info), got %+v", got)
	}
	sev, _ := findingSeverity(got, ruleDepPlannedSkipped)
	if sev != SeverityInfo {
		t.Fatalf("DEP-PLANNED-SKIPPED must be Info, got %v", sev)
	}
}

// TestDeploymentCoverage_BuiltUncoveredStillFlagged is the passing control: a BUILT
// container-eligible component packaged into no container still trips DEP-COVERAGE.
func TestDeploymentCoverage_BuiltUncoveredStillFlagged(t *testing.T) {
	base := deploymentBaseSystem(t)
	op := deploymentBaseOC(t, base) // covers ONLY the base
	// A BUILT (absent buildStatus) component packaged into no container is uncovered.
	s := System{Components: append(append([]Component{}, base.Components...), comp(t, "FutureManager", kindManager))}
	got := deploymentConsistency(op, s)
	if !hasRuleFindings(got, ruleDepCoverage) {
		t.Fatalf("a built container-eligible component covered by no container must trip DEP-COVERAGE, got %+v", got)
	}
	if hasRuleFindings(got, ruleDepPlannedSkipped) {
		t.Fatalf("no planned components → no DEP-PLANNED-SKIPPED, got %+v", got)
	}
}

// ---- ancestor-absorb for BUILT components (subpackage-only shape) ----

// TestAlign_Built_MCPSubpackageShape: a BUILT MCPClient whose code is ONLY generated
// subpackages under client/mcp/* (no root client/mcp package) is cleanly matched by
// ancestor-absorb: no ALIGN-MISSING-PKG, and its subpackages are not ALIGN-EXTRA-PKG.
func TestAlign_Built_MCPSubpackageShape(t *testing.T) {
	mcp := comp(t, "MCPClient", kindClient) // built (absent buildStatus)
	s := System{Components: []Component{mcp}}
	pkgs := []classifiedPackage{
		cpkg("ex/client/mcp/designtools", "designtools", "Client"),
		cpkg("ex/client/mcp/statetools", "statetools", "Client"),
	}
	got := alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, nil, nil, nil)
	if len(got) != 0 {
		t.Fatalf("a built subpackage-only MCPClient must absorb client/mcp/* cleanly, got %+v", got)
	}
}

// TestAlign_Built_NeighborClients_NoSwallow: two NEIGHBORING Client components (WebClient
// with a root package + subpackages, MCPClient with subpackages only) each absorb their
// OWN client/<name>/* subtree. Both matched, no missing, no extra; the identically-named
// subsystem leaves (client/web/tools and client/mcp/tools) do not cross-attribute.
func TestAlign_Built_NeighborClients_NoSwallow(t *testing.T) {
	web := comp(t, "WebClient", kindClient)
	mcp := comp(t, "MCPClient", kindClient)
	s := System{Components: []Component{web, mcp}}
	pkgs := []classifiedPackage{
		cpkg("ex/client/web", "web", "Client"),
		cpkg("ex/client/web/tools", "tools", "Client"),
		cpkg("ex/client/mcp/tools", "tools", "Client"),
	}
	got := alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, nil, nil, nil)
	if len(got) != 0 {
		t.Fatalf("neighboring web/mcp clients must each absorb their own subtree cleanly, got %+v", got)
	}
}

// TestAlign_Built_NestedComponent_DeepestOwnerWins: when one component's directory NESTS
// inside another's (ToolsClient at client/mcp/tools inside MCPClient at client/mcp), the
// DEEPEST matching segment owns the package. A planned MCPClient whose only nested content
// belongs to the built ToolsClient is therefore NOT stale, and ToolsClient is matched: the
// outer component does not swallow the inner component's package.
func TestAlign_Built_NestedComponent_DeepestOwnerWins(t *testing.T) {
	mcp := comp(t, "MCPClient", kindClient)
	mcp.BuildStatus = buildStatusPlanned        // planned: owns nothing of its own
	tools := comp(t, "ToolsClient", kindClient) // built, dir nests under client/mcp/tools
	s := System{Components: []Component{mcp, tools}}
	pkgs := []classifiedPackage{
		cpkg("ex/client/mcp/tools/render", "render", "Client"),
	}
	got := alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, nil, nil, nil)
	if hasRuleFindings(got, ruleAlignStalePlanned) {
		t.Fatalf("outer planned MCPClient must not be flagged stale by a nested component's package, got %+v", got)
	}
	if hasRuleFindings(got, ruleAlignMissingPkg) {
		t.Fatalf("nested built ToolsClient must be matched via ancestor-absorb, got %+v", got)
	}
	if len(got) != 0 {
		t.Fatalf("nested/neighbor deepest-owner resolution must be clean, got %+v", got)
	}
}

// TestAlign_Built_OrphanSubpackage_StillExtra: a subpackage under NO design component's
// directory is still ALIGN-EXTRA-PKG. Ancestor-absorb must claim only packages a
// component actually owns, never blanket-suppress genuinely orphaned code.
func TestAlign_Built_OrphanSubpackage_StillExtra(t *testing.T) {
	mcp := comp(t, "MCPClient", kindClient)
	s := System{Components: []Component{mcp}}
	pkgs := []classifiedPackage{
		cpkg("ex/client/mcp/designtools", "designtools", "Client"), // owned by MCPClient
		cpkg("ex/client/ghost/orphaned", "orphaned", "Client"),     // under no component dir
	}
	got := alignSystemToCode(s, pkgs, StereotypeSuffixNormalizer, nil, nil, nil)
	if !hasRuleFindings(got, ruleAlignExtraPkg) {
		t.Fatalf("a subpackage under no component directory must still be ALIGN-EXTRA-PKG, got %+v", got)
	}
}

func boolPtr(b bool) *bool { return &b }

// A browser surface's code is under webApp/src/, so finishing its activity must not turn the
// Go alignment gate red for every later task. A machine client still owes a package.
func TestAlignBrowserSurfaceOwesNoPackage(t *testing.T) {
	sys := System{Components: []Component{
		{ID: "web-app", Name: "WebApp", Kind: kindClient, Layer: layerClient, UISurface: boolPtr(true)},
		{ID: "machine-api", Name: "MachineApi", Kind: kindClient, Layer: layerClient, UISurface: boolPtr(false), MachineProtocol: machineProtocolMCP},
		{ID: "list-manager", Name: "ListManager", Kind: kindManager, Layer: layerManager},
	}}
	pkgs := []classifiedPackage{{pkgPath: "m/internal/manager/list", leaf: "list", layer: "Manager"}}
	got := alignSystemToCode(sys, pkgs, StereotypeSuffixNormalizer, nil, nil, nil)
	if len(got) != 1 || got[0].RuleID != ruleAlignMissingPkg || !strings.Contains(got[0].Message, "MachineApi") {
		t.Fatalf("only the machine client owes a package, got %+v", got)
	}
}

// Generated shared transport is nobody's component; a hand-written package beside it still is
// an orphan.
func TestAlignGeneratedTransportIsNotAnOrphan(t *testing.T) {
	sys := System{Components: []Component{{ID: "list-manager", Name: "ListManager", Kind: kindManager, Layer: layerManager}}}
	pkgs := []classifiedPackage{
		{pkgPath: "m/internal/manager/list", leaf: "list", layer: "Manager"},
		{pkgPath: "m/internal/client/web", leaf: "web", layer: "Client", transport: true},
		{pkgPath: "m/internal/client/web/list", leaf: "list", layer: "Client", transport: true},
	}
	if got := alignSystemToCode(sys, pkgs, StereotypeSuffixNormalizer, nil, nil, nil); len(got) != 0 {
		t.Fatalf("generated transport must not be an orphan, got %+v", got)
	}
	pkgs = append(pkgs, classifiedPackage{pkgPath: "m/internal/client/rogue", leaf: "rogue", layer: "Client"})
	got := alignSystemToCode(sys, pkgs, StereotypeSuffixNormalizer, nil, nil, nil)
	if len(got) != 1 || got[0].RuleID != ruleAlignExtraPkg {
		t.Fatalf("a hand-written client package must stay an orphan, got %+v", got)
	}
}

func TestIsGeneratedTransport(t *testing.T) {
	cases := []struct {
		path  string
		files []string
		want  bool
	}{
		{"m/internal/client/web", []string{"/x/server.gen.go"}, true},
		{"m/internal/client/mcp/list", []string{"/x/list_tools.gen.go"}, true},
		{"m/internal/client/web/list", []string{"/x/h.gen.go", "/x/hand.go"}, false},
		{"m/internal/client/machineapi", []string{"/x/a.gen.go"}, false},
		{"m/internal/client/webhooks", []string{"/x/a.gen.go"}, false},
	}
	for _, c := range cases {
		if got := isGeneratedTransport(&packages.Package{PkgPath: c.path, GoFiles: c.files}); got != c.want {
			t.Errorf("%s %v: got %v", c.path, c.files, got)
		}
	}
}

// A generated Client no activity covers cannot exist before a Manager is integrated.
func TestGeneratedClientPendingFollowsItsManagers(t *testing.T) {
	sys := System{Components: []Component{
		{ID: "mcp", Kind: kindClient, ConstructionProfile: constructionGenerated},
		{ID: "hand", Kind: kindClient},
		{ID: "mgr", Kind: kindManager},
	}}
	pending := map[string]bool{"mgr": true}
	withGeneratedClientsPending(sys, pending)
	if !pending["mcp"] || pending["hand"] {
		t.Fatalf("got %v", pending)
	}
	pending = map[string]bool{}
	withGeneratedClientsPending(sys, pending)
	if pending["mcp"] {
		t.Fatal("once the Managers are integrated a generated client is obligated")
	}
}

// The whole path a project takes: a generated Client owes nothing while a Manager is still to
// come, and is owed once every Manager's activity has completed.
func TestAlignProjectAppliesTheGeneratedClientTolerance(t *testing.T) {
	sys := System{Components: []Component{
		{ID: "mcp", Name: "Mcp", Kind: kindClient, Layer: layerClient, ConstructionProfile: constructionGenerated},
		{ID: "mgr", Name: "Mgr", Kind: kindManager, Layer: layerManager},
	}}
	pkgs := []classifiedPackage{{pkgPath: "m/internal/manager/mgr", leaf: "mgr", layer: "Manager"}}
	missing := func(rows map[string]ActivityRow) int {
		t.Helper()
		found, err := alignProject(sys, pkgs, defaultNormalizer, pendingProject(t, []string{"mgr"}, rows))
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, f := range found {
			if f.RuleID == ruleAlignMissingPkg {
				n++
			}
		}
		return n
	}
	if got := missing(nil); got != 0 {
		t.Errorf("a pending Manager leaves a generated client pending, got %d missing", got)
	}
	if got := missing(map[string]ActivityRow{"C-mgr": {CompletedAt: done}}); got != 1 {
		t.Errorf("an integrated Manager makes the generated client owed, got %d missing", got)
	}
}

// The rule is live on the slot the architect drafts, not only as a function.
func TestArchitectureValidationRequiresClientSurface(t *testing.T) {
	res, err := validateArchitecture(System{Components: []Component{{ID: "a", Name: "A", Kind: kindClient}}}, CoreUseCases{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(res.Findings, func(f Finding) bool { return f.RuleID == ruleSysClientSurface }) {
		t.Fatalf("no %s in %+v", ruleSysClientSurface, res.Findings)
	}
}

func TestClientSurfaceMustBeExplicit(t *testing.T) {
	s := System{Components: []Component{
		{ID: "a", Name: "A", Kind: kindClient},
		{ID: "b", Name: "B", Kind: kindClient, UISurface: boolPtr(false), MachineProtocol: machineProtocolMCP},
		{ID: "c", Name: "C", Kind: kindManager},
	}}
	got := clientSurfaceExplicit(s)
	if len(got) != 1 || got[0].RuleID != ruleSysClientSurface || got[0].Severity != SeverityError {
		t.Fatalf("got %+v", got)
	}
}
