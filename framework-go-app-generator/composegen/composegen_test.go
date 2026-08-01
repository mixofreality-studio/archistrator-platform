package composegen_test

import (
	"flag"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

var update = flag.Bool("update", false, "rewrite golden files")

// greenfieldCfg is the composegen invocation the golden + compile-proof share.
var greenfieldCfg = composegen.Config{
	ContainerKey: "order-app",
	ModulePath:   "example.com/greenfield/server",
	PackageName:  "main",
	EnvPrefix:    "ORDERAPP",
}

func TestGreenfieldGolden(t *testing.T) {
	m := loadGreenfield(t)
	got, err := composegen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	src, ok := got["main.gen.go"]
	if !ok {
		t.Fatal("Generate did not return main.gen.go")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "main.gen.go", src, parser.AllErrors); err != nil {
		t.Fatalf("emitted main.gen.go does not parse: %v\n%s", err, src)
	}
	if !strings.Contains(string(src), "package main") {
		t.Errorf("missing 'package main' clause")
	}
	checkGolden(t, "../testdata/greenfield.composegen.main.gen.go.golden", src)
}

func TestDeterminism(t *testing.T) {
	m := loadGreenfield(t)
	a, err := composegen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := composegen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatal(err)
	}
	if string(a["main.gen.go"]) != string(b["main.gen.go"]) {
		t.Fatal("Generate is not deterministic")
	}
}

func TestConfigErrors(t *testing.T) {
	m := loadGreenfield(t)
	cases := []struct {
		name string
		cfg  composegen.Config
		want string
	}{
		{"no-package", composegen.Config{ModulePath: "x"}, "PackageName"},
		{"no-module", composegen.Config{PackageName: "main"}, "ModulePath"},
		{"bad-container", composegen.Config{PackageName: "main", ModulePath: "x", ContainerKey: "ghost"}, "container"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := composegen.Generate(m, c.cfg); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want %q error, got %v", c.want, err)
			}
		})
	}
	if _, err := composegen.Generate(&projectmodel.Model{}, composegen.Config{PackageName: "main", ModulePath: "x"}); err == nil || !strings.Contains(err.Error(), "deployment") {
		t.Fatalf("want no-deployment error, got %v", err)
	}
}

// projectstatePkg is the archistrator projectstate package import — the source
// of the GitHub projectStateAccess variant's composition-root port types.
const projectstatePkg = "github.com/mixofreality-studio/archistrator/server/internal/resourceaccess/projectstate"

// archistratorCfg is the composition-root invocation for the real archistrator
// server container. VariantHookArgs supplies the two variant constructor arg
// tuples the deployment model cannot express (G3): projectstate GitHub needs the
// sourcecontrol-backed catalog + minter PORTS (an RA→RA edge forbidden inside
// projectstate), and artifact GitHubCloud needs the repoURL/owner strings + the
// typed int64 installationID. WebExposedManagers (B1) matches the REAL driver:
// cmd/clientgen's exposedManagers list (the 4 web-wired managers) — billing is
// deliberately excluded (no internal/client/web/billing package is generated;
// billing is driven by its own Schedules + cross-Manager signals, never
// mounted on HTTP/MCP), even though the committed System model declares a
// web-client/mcp-client → billing-manager relationship.
var archistratorCfg = composegen.Config{
	ContainerKey: "archistrator-server",
	ModulePath:   "github.com/mixofreality-studio/archistrator/server",
	PackageName:  "main",
	EnvPrefix:    "ARCHISTRATOR",
	WebExposedManagers: []string{
		"systemDesignManager",
		"projectDesignManager",
		"constructionManager",
		"operationsManager",
	},
	VariantHookArgs: map[string][]composegen.HookArgType{
		"projectStateAccess/GitHub": {
			{GoType: "string"}, // webHost
			{GoType: "string"}, // account
			{GoType: "projectstate.ProjectCatalog", GoImport: projectstatePkg},
			{GoType: "projectstate.CredentialMinter", GoImport: projectstatePkg},
		},
		"artifactAccess/GitHubCloud": {
			{GoType: "string"}, // repoURL
			{GoType: "string"}, // owner
			{GoType: "string"}, // appID
			{GoType: "string"}, // privateKeyPEM
			{GoType: "string"}, // apiBaseURL
			{GoType: "int64"},  // installationID
		},
	},
}

// hasTemporalInfra reports whether the deployment declares a "temporal"
// infrastructure substrate (as opposed to configuring Temporal via plain
// settings, G7).
func hasTemporalInfra(d *projectmodel.Deployment) bool {
	for _, decl := range d.Infrastructure {
		if decl.Substrate == "temporal" {
			return true
		}
	}
	return false
}

// TestArchistratorFixtureGenerates is the recurrence guard: composegen MUST emit
// a gofmt-clean, parseable composition root for the REAL archistrator model —
// the shape that surfaced the six emitter gaps (import-alias collisions,
// nil-goPackage contracts, hook-provided variant args, arm-less stub bindings,
// per-manager func-dep hooks, conditional worker registration). It asserts the
// three load-bearing anchors: the construction Worker's conditional registration
// (G6b), the arm-less billingState stub call (G4), and the projectstate GitHub
// variant-args hook call (G3).
//
// G7 (recorded in the step-8 A2 report, not yet fixed on the archistrator
// side): the fixture's deployment configures Temporal via plain SETTINGS
// (temporalHostPort/temporalNamespace), not an `infrastructure` decl, even
// though its Managers declare a client.Client dep — today that's a genuine gap
// in the MODEL, not the emitter, and Generate now fails with the clear, named
// error instead of emitting an undefined `tc` reference. This branch is
// flip-ready: once the archistrator-side deployment amendment splices temporal
// into `infrastructure` (the A2 retry), hasTemporalInfra flips true and the
// test falls through to the existing success-path assertions below with no
// further edits required.
func TestArchistratorFixtureGenerates(t *testing.T) {
	m := loadArchistrator(t)
	got, err := composegen.Generate(m, archistratorCfg)

	if !hasTemporalInfra(m.Deployment) {
		if err == nil {
			t.Fatal("want the missing-temporal-infrastructure error, got nil — " +
				"has the fixture gained a temporal infra decl? if so, delete this branch")
		}
		want := `composegen: container "archistrator-server": managers require a "temporal" infrastructure declaration`
		if err.Error() != want {
			t.Fatalf("error = %q, want %q", err.Error(), want)
		}
		return
	}

	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	src, ok := got["main.gen.go"]
	if !ok {
		t.Fatal("Generate did not return main.gen.go")
	}
	// Generate gofmt's internally (format.Source), so a returned src is already
	// gofmt-clean; re-parse to prove it independently.
	if _, err := parser.ParseFile(token.NewFileSet(), "main.gen.go", src, parser.AllErrors); err != nil {
		t.Fatalf("emitted main.gen.go does not parse: %v\n%s", err, src)
	}
	if formatted, err := format.Source(src); err != nil {
		t.Fatalf("emitted main.gen.go is not gofmt-clean: %v", err)
	} else if string(formatted) != string(src) {
		t.Errorf("emitted main.gen.go is not idempotent under gofmt")
	}
	s := string(src)
	for _, anchor := range []string{
		"if hooks.RegisterConstructionManagerWorker(cfg) {", // G6b conditional worker
		"billingstate.NewBillingStateAccess()",              // G4 arm-less required stub
		"hooks.ProjectStateAccessGitHubArgs(cfg)",           // G3 variant-args hook call
		"enginebilling ", // G1 alias-collision disambiguation
		"ProjectStateAccessGitHubArgs(cfg *Config) (string, string, projectstate.ProjectCatalog, projectstate.CredentialMinter)", // G3 typed hook
		"operatedRuntimeAccess = hooks.FinalizeOperatedRuntimeAccess(cfg, operatedRuntimeAccess)",                                // A2 required-binding Finalize call (orthogonal dry-run toggle)
	} {
		if !strings.Contains(s, anchor) {
			t.Errorf("emitted main.gen.go missing expected anchor %q", anchor)
		}
	}
	// G2: the three design-only engine contracts (goPackage=null) must NOT be
	// constructed (no unqualified .NewXxx() calls, no "<module>/" bogus import).
	for _, gone := range []string{"NewArtifactRenderingAccess", "NewSystemDesignEngine", "NewArtifactValidationEngine"} {
		if strings.Contains(s, gone) {
			t.Errorf("emitted main.gen.go constructs a nil-goPackage engine contract %q (G2 skip failed)", gone)
		}
	}
	// B1: WebExposedManagers REPLACES the relationship-derived web-exposed set —
	// billingManager carries a web-client/mcp-client relationship in the
	// committed System model but MUST NOT be web-exposed (clientgen generates no
	// internal/client/web/billing package; a "billingweb" import would not
	// compile).
	for _, gone := range []string{"billingweb", "internal/client/web/billing", "BillingManager: billingManager"} {
		if strings.Contains(s, gone) {
			t.Errorf("emitted main.gen.go references billing web exposure %q (B1 override failed)", gone)
		}
	}
}

// TestGapsGolden covers the emitter-fix gap CLASSES on a composegen-only fixture
// that extends the greenfield shape (the shared greenfield.project.json is left
// untouched so the other generators' suites — modelgen/transportgen/temporalgen —
// are unaffected; composegen + configgen do not require $defs): an alias
// collision (internal/engine/billing + internal/manager/billing -> enginebilling
// / managerbilling, G1), a nil-goPackage engine contract that must be skipped
// (reportingEngine, G2), an arm-less required stub binding (ledgerStateAccess ->
// ledgerstate.NewLedgerStateAccess(), G4), and two managers each declaring a
// same-named "repo" func dep whose bare exported type is qualified per owning
// manager (managerbilling.AccountID vs order's string, G5). TestCompileSandbox
// compiles this same fixture end-to-end.
func TestGapsGolden(t *testing.T) {
	m := loadGaps(t)
	got, err := composegen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	src := got["main.gen.go"]
	if _, err := parser.ParseFile(token.NewFileSet(), "main.gen.go", src, parser.AllErrors); err != nil {
		t.Fatalf("emitted main.gen.go does not parse: %v\n%s", err, src)
	}
	for _, anchor := range []string{
		"enginebilling ",                     // G1
		"managerbilling ",                    // G1
		"ledgerstate.NewLedgerStateAccess()", // G4
		"BillingManagerRepo() func(id managerbilling.AccountID)", // G5 bare-type qualification
		"OrderManagerRepo() func(orderID string)",                // G5 per-manager naming
	} {
		if !strings.Contains(string(src), anchor) {
			t.Errorf("gaps main.gen.go missing anchor %q", anchor)
		}
	}
	if strings.Contains(string(src), "ReportingEngine") { // G2 skip
		t.Errorf("gaps main.gen.go constructs the nil-goPackage reportingEngine (G2 skip failed)")
	}
	checkGolden(t, "../testdata/composegen_gaps.main.gen.go.golden", src)
}

// TestPostgresPoolGatedByProfile proves the shared Postgres pool satellite
// (writePostgres) is built ONLY inside the runtime profiles that actually
// declare the postgres infra key — not unconditionally at generation time
// just because SOME profile (cloud) needs it. This is the root-cause fix for
// the local-first-init-funnel plan's Task 2 finding: a local boot with no
// Postgres reachable must never attempt the dial. The greenfield fixture is
// exactly the target shape — postgres.profiles == ["cloud"] only,
// orderStateAccess's "local" arm is the postgres-free "memory" variant — so
// this is a genuine "absent for the all-no-op profile, present for cloud"
// proof, not a synthetic one.
func TestPostgresPoolGatedByProfile(t *testing.T) {
	m := loadGreenfield(t)
	got, err := composegen.Generate(m, greenfieldCfg)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	s := string(got["main.gen.go"])

	// The pool var is predeclared once, typed explicitly (so both the guarded
	// build and any RA arm outside the guard reference the same nil-or-live
	// var) — never built with the old unconditional "pool, err := ..." form.
	if !strings.Contains(s, "var pool *pgxpool.Pool") {
		t.Errorf("missing profile-gated pool predecl %q", "var pool *pgxpool.Pool")
	}
	if strings.Contains(s, "pool, err := postgresinfra.NewPool") {
		t.Errorf("pool is still built unconditionally (old ungated form present) — gating regressed")
	}

	// The dial itself is nested inside a runtime profile check naming exactly
	// the profile(s) postgres.profiles declares (here: cloud only) — so a
	// "local" boot's control flow never reaches postgresinfra.NewPool, and
	// therefore never dials.
	if !strings.Contains(s, `if profile == "cloud" {`) {
		t.Errorf("pool build is not gated on the resolved profile")
	}
	if !strings.Contains(s, "pool, err = postgresinfra.NewPool(ctx, cfg.PostgresURL)") {
		t.Errorf("missing the gated pool dial call")
	}

	// Sanity anchor: the pool dial call must textually appear AFTER the guard
	// opens and BEFORE it closes, i.e. actually nested inside the `if`, not
	// merely present somewhere else in the file.
	guardIdx := strings.Index(s, `if profile == "cloud" {`)
	dialIdx := strings.Index(s, "pool, err = postgresinfra.NewPool(ctx, cfg.PostgresURL)")
	closeIdx := strings.Index(s, "defer pool.Close()")
	if guardIdx < 0 || dialIdx < 0 || closeIdx < 0 || !(guardIdx < dialIdx && dialIdx < closeIdx) {
		t.Errorf("pool dial + defer Close are not nested inside the profile guard (guard=%d dial=%d close=%d)", guardIdx, dialIdx, closeIdx)
	}
}

// messageBusHookFixture is a minimal project.json (Task 7c: startup
// schedule-registration seam): one Manager (orderManager) depending on BOTH
// the Temporal client and a Utility-layer messageBus component, whose single
// "prod" deployment binding arm consumes the temporal infra AND is
// VariantHookArgs-configured (its real constructor, mirroring
// messagebus.NewTemporalMessageBus(cl, table), needs a composition-root-typed
// table the model cannot express). Mirrors temporalgen_test.go's
// utilityDepFixture shape, extended with the deployment section composegen
// needs.
const messageBusHookFixture = `{
  "id": "messagebus-hook-fixture",
  "version": 1,
  "slots": {
    "6": {
      "kind": 6,
      "model": {
        "deployment": {
          "deliveryStyle": "cloud",
          "containers": [
            { "key": "order-app", "name": "order-app", "components": ["orderManager", "messageBus"] }
          ],
          "environments": [ { "profile": "prod", "title": "Prod" } ],
          "infrastructure": [
            { "key": "temporal", "substrate": "temporal", "profiles": ["prod"], "presence": "required" }
          ],
          "bindings": [
            {
              "component": "messageBus",
              "presence": "required",
              "perProfile": { "prod": { "variant": "Temporal", "infra": ["temporal"] } }
            }
          ]
        }
      }
    }
  },
  "serviceContracts": {
    "orderManager": {
      "component": "orderManager",
      "layer": "Manager",
      "goPackage": "internal/manager/order",
      "title": "order contract",
      "deps": [
        { "name": "client", "goType": "client.Client", "goImport": "go.temporal.io/sdk/client" },
        { "name": "messageBus", "component": "messageBus" }
      ],
      "$defs": { "OrderID": { "type": "string" } },
      "interface": {
        "name": "OrderManager",
        "layer": "manager",
        "operations": [
          { "name": "PlaceOrder", "params": [ { "name": "id", "schema": { "$ref": "#/$defs/OrderID" } } ], "error": true }
        ]
      }
    },
    "messageBus": {
      "component": "messageBus",
      "layer": "Utility",
      "goPackage": "internal/utility/messagebus",
      "title": "messagebus contract",
      "$defs": { "ExecutionID": { "type": "string" }, "SignalName": { "type": "string" } },
      "interface": {
        "name": "MessageBus",
        "layer": "utility",
        "operations": [
          {
            "name": "DeliverSignal",
            "params": [
              { "name": "targetExecutionID", "schema": { "$ref": "#/$defs/ExecutionID" } },
              { "name": "signalName", "schema": { "$ref": "#/$defs/SignalName" } }
            ],
            "error": true
          }
        ]
      }
    }
  }
}`

// TestMessageBusTemporalHookArgsAndScheduleRegistration is the composegen-side
// increment for Task 7c's schedule-registration seam (companion to modelgen's
// utility layerContext + temporalgen's activity-bearing-utility increments,
// e9571782 / 2f5750c3): it proves the two capabilities the messageBus
// composition needs that no PRIOR binding needed together —
//
//  1. A VariantHookArgs-configured arm whose bound infra resolves to an
//     already-constructed LOCAL (temporal → tc), not a cfg field, threads that
//     local as an EXTRA hook parameter (hooks.MessageBusTemporalArgs(cfg, tc)),
//     with a matching Hooks-interface signature — "the hook reads its cfg"
//     (true for every prior github-app-substrate hook-args variant) does not
//     hold here.
//  2. Config.VariantConstructorNoError overrides the infra-implies-error
//     heuristic (messagebus.NewTemporalMessageBus is single-return despite
//     consuming infra) — a plain ":=" construction, no "err :=" wrapping.
//  3. A Manager depending on the messageBus component gets a
//     RegisterSchedules(ctx, messageBus) call emitted immediately after its
//     embedded Worker starts (so a Schedule can never fire against a dormant
//     Worker).
func TestMessageBusTemporalHookArgsAndScheduleRegistration(t *testing.T) {
	m, err := projectmodel.Load([]byte(messageBusHookFixture))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	got, err := composegen.Generate(m, composegen.Config{
		ContainerKey: "order-app",
		ModulePath:   "example.com/messagebushook/server",
		PackageName:  "main",
		EnvPrefix:    "ORDERAPP",
		VariantHookArgs: map[string][]composegen.HookArgType{
			"messageBus/Temporal": {{GoType: "map[string]int"}},
		},
		VariantConstructorNoError: map[string]bool{
			"messageBus/Temporal": true,
		},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	src := got["main.gen.go"]
	if _, err := parser.ParseFile(token.NewFileSet(), "main.gen.go", src, parser.AllErrors); err != nil {
		t.Fatalf("emitted main.gen.go does not parse: %v\n%s", err, src)
	}
	s := string(src)

	for _, anchor := range []string{
		// 1: tc threaded as an extra POSITIONAL arg ahead of the (unchanged-
		// signature) hook call — not folded into the hook call itself.
		"messageBus := messagebus.NewTemporalMessageBus(tc, hooks.MessageBusTemporalArgs(cfg))",
		"MessageBusTemporalArgs(cfg *Config) map[string]int",
		// 3: the startup Schedule-registration call, right after the Worker starts.
		"logger.Info(\"embedded temporal worker started\", \"taskQueue\", order.TaskQueue)\n\tif err := order.RegisterSchedules(ctx, messageBus); err != nil {\n\t\treturn err\n\t}\n\tlogger.Info(\"orderManager Temporal Schedules registered\")",
	} {
		if !strings.Contains(s, anchor) {
			t.Errorf("emitted main.gen.go missing expected anchor %q\nfull src:\n%s", anchor, s)
		}
	}
	// 2: single-return construction — no err-wrapping for messageBus specifically.
	if strings.Contains(s, "messageBus, err := messagebus.NewTemporalMessageBus") {
		t.Error("messageBus construction is err-wrapped despite VariantConstructorNoError override")
	}
	// The hook interface method must appear EXACTLY ONCE — messageBus's
	// "Temporal" variant binds BOTH the local and cloud profiles, and a naive
	// per-arm append would emit a duplicate (uncompilable) interface method.
	if n := strings.Count(s, "MessageBusTemporalArgs(cfg *Config)"); n != 1 {
		t.Errorf("want exactly 1 MessageBusTemporalArgs hook method declaration, got %d", n)
	}
	// The manager's own constructor receives the REAL messageBus var, not nil.
	if !strings.Contains(s, "order.NewOrderManager(tc, messageBus)") {
		t.Errorf("orderManager constructor does not receive the real messageBus var; full src:\n%s", s)
	}
}

func loadGreenfield(t *testing.T) *projectmodel.Model {
	t.Helper()
	m, err := projectmodel.LoadFile("../testdata/greenfield.project.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	return m
}

func loadNoTemporal(t *testing.T) *projectmodel.Model {
	t.Helper()
	m, err := projectmodel.LoadFile("../testdata/composegen_notemporal.project.json")
	if err != nil {
		t.Fatalf("load no-temporal fixture: %v", err)
	}
	return m
}

// TestMissingTemporalInfra covers G7 (recorded not fixed, then closed): a
// deployment whose Managers need the Temporal control-plane client
// (client.Client) but which declares NO "temporal" infrastructure substrate
// must fail Generate with a clear, named error — not silently emit an
// undefined `tc` reference that only fails to COMPILE. The fixture is the
// greenfield model with its "temporal" infra decl removed (Managers keep
// their client.Client dep unchanged).
func TestMissingTemporalInfra(t *testing.T) {
	m := loadNoTemporal(t)
	_, err := composegen.Generate(m, greenfieldCfg)
	if err == nil {
		t.Fatal("want error for missing temporal infrastructure, got nil")
	}
	want := `composegen: container "order-app": managers require a "temporal" infrastructure declaration`
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

func loadGaps(t *testing.T) *projectmodel.Model {
	t.Helper()
	m, err := projectmodel.LoadFile("../testdata/composegen_gaps.project.json")
	if err != nil {
		t.Fatalf("load gaps fixture: %v", err)
	}
	return m
}

func loadArchistrator(t *testing.T) *projectmodel.Model {
	t.Helper()
	m, err := projectmodel.LoadFile("../testdata/archistrator.project.json")
	if err != nil {
		t.Fatalf("load archistrator fixture: %v", err)
	}
	return m
}

func checkGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update)", path, err)
	}
	if string(got) != string(want) {
		t.Errorf("output mismatch for %s (run with -update to refresh)", path)
	}
}
