package httpgen_test

import (
	"flag"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/httpgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"gopkg.in/yaml.v3"
)

var update = flag.Bool("update", false, "rewrite golden files")

type fixture struct {
	name          string
	contractFile  string
	managerImport string
	goGolden      string
	yamlGolden    string
}

var fixtures = []fixture{
	{
		name:          "project",
		contractFile:  "../testdata/project.contract.schema.json",
		managerImport: "github.com/mixofreality-studio/archistrator/server/internal/manager/project",
		goGolden:      "../testdata/project.handlers.go.golden",
		yamlGolden:    "../testdata/project.openapi.yaml.golden",
	},
	{
		name:          "construction",
		contractFile:  "../testdata/construction.contract.schema.json",
		managerImport: "github.com/mixofreality-studio/archistrator/server/internal/manager/construction",
		goGolden:      "../testdata/construction.handlers.go.golden",
		yamlGolden:    "../testdata/construction.openapi.yaml.golden",
	},
	{
		name:          "systemdesign",
		contractFile:  "../testdata/systemdesign.contract.schema.json",
		managerImport: "github.com/mixofreality-studio/archistrator/server/internal/manager/systemdesign",
		goGolden:      "../testdata/systemdesign.handlers.go.golden",
		yamlGolden:    "../testdata/systemdesign.openapi.yaml.golden",
	},
	{
		name:          "projectdesign",
		contractFile:  "../testdata/projectdesign.contract.schema.json",
		managerImport: "github.com/mixofreality-studio/archistrator/server/internal/manager/projectdesign",
		goGolden:      "../testdata/projectdesign.handlers.go.golden",
		yamlGolden:    "../testdata/projectdesign.openapi.yaml.golden",
	},
	{
		name:          "operations",
		contractFile:  "../testdata/operations.contract.schema.json",
		managerImport: "github.com/mixofreality-studio/archistrator/server/internal/manager/operations",
		goGolden:      "../testdata/operations.handlers.go.golden",
		yamlGolden:    "../testdata/operations.openapi.yaml.golden",
	},
}

// sampleFixtures drive the in-module compile proof: each emitted handler set is
// generated against a stub manager package whose signatures match the real ones
// (uuid.UUID identities, int-enum params, struct body params) and committed to
// internal/sample, where `go build ./...` compiles it against the stubs + the
// REAL framework-go security/manager packages.
var sampleFixtures = []struct {
	name    string
	short   string
	pkg     string
	stubPkg string
	sample  string
}{
	{"project", "project", "sample", "projectmgr", "../internal/sample/project_handlers.gen.go"},
	{"systemdesign", "systemdesign", "systemdesign", "systemdesignmgr", "../internal/sample/systemdesign/handlers.gen.go"},
	{"projectdesign", "projectdesign", "projectdesign", "projectdesignmgr", "../internal/sample/projectdesign/handlers.gen.go"},
	{"operations", "operations", "operations", "operationsmgr", "../internal/sample/operations/handlers.gen.go"},
}

func TestGenerateGolden(t *testing.T) {
	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			raw, err := os.ReadFile(fx.contractFile)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := projectmodel.Parse(raw)
			if err != nil {
				t.Fatalf("parse contract: %v", err)
			}
			res, err := httpgen.Generate(doc, httpgen.Options{ManagerImport: fx.managerImport})
			if err != nil {
				t.Fatalf("generate: %v", err)
			}

			// Emitted Go must parse.
			if _, err := parser.ParseFile(token.NewFileSet(), "handlers.go", res.HandlersGo, parser.AllErrors); err != nil {
				t.Fatalf("emitted handlers do not parse: %v", err)
			}

			// The per-component route-registration func (Handler.Register) must mount
			// exactly one route per operation — the full surface the wiring composes.
			handlers := string(res.HandlersGo)
			for _, op := range doc.Interface.Operations {
				if !strings.Contains(handlers, "h.handle"+op.Name+")") {
					t.Errorf("Register does not mount a route for op %q", op.Name)
				}
			}
			// Emitted OpenAPI must be valid YAML with the expected top-level shape.
			var oas map[string]any
			if err := yaml.Unmarshal(res.OpenAPIYAML, &oas); err != nil {
				t.Fatalf("emitted OpenAPI is not valid YAML: %v", err)
			}
			if oas["openapi"] != "3.1.0" {
				t.Errorf("openapi version = %v, want 3.1.0", oas["openapi"])
			}
			paths, ok := oas["paths"].(map[string]any)
			if !ok || len(paths) != len(doc.Interface.Operations) {
				t.Errorf("paths count = %d, want %d", len(paths), len(doc.Interface.Operations))
			}
			comps, _ := oas["components"].(map[string]any)
			schemas, _ := comps["schemas"].(map[string]any)
			for defName := range doc.Defs {
				if _, ok := schemas[defName]; !ok {
					t.Errorf("component schema %q missing from OpenAPI", defName)
				}
			}
			if _, ok := schemas["ErrorResponse"]; !ok {
				t.Error("ErrorResponse component schema missing")
			}

			checkGolden(t, fx.goGolden, res.HandlersGo)
			checkGolden(t, fx.yamlGolden, res.OpenAPIYAML)
		})
	}
}

// TestSampleInSync verifies the committed compile-proof samples (internal/sample,
// generated against the in-module stub packages and compiled by `go build ./...`)
// are regenerated byte-identically.
func TestSampleInSync(t *testing.T) {
	const base = "github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/internal/stub"
	for _, fx := range sampleFixtures {
		t.Run(fx.name, func(t *testing.T) {
			raw, err := os.ReadFile("../testdata/" + fx.short + ".contract.schema.json")
			if err != nil {
				t.Fatal(err)
			}
			doc, err := projectmodel.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			res, err := httpgen.Generate(doc, httpgen.Options{
				Package:                fx.pkg,
				ManagerImport:          base + "/" + fx.stubPkg,
				FrameworkManagerImport: base + "/manager",
				SecurityImport:         base + "/security",
			})
			if err != nil {
				t.Fatal(err)
			}
			checkGolden(t, fx.sample, res.HandlersGo)
		})
	}
}

// realSecurityImport is the REAL platform security package the emitted wiring +
// middleware compile against (internal/sample/wiring is the build proof —
// `go build ./...` fails if any security-pkg API call below is not real).
const realSecurityImport = "github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"

// TestGenerateWiringGolden generates the component-agnostic wiring + middleware
// layer, asserts both files parse, that the middleware references the security
// package API it must (Validator / WithPrincipal / Middleware / NewError /
// Principal), and that the server mounts the authenticated /api/v1/ tree
// plus the unauthenticated health probes — then matches the committed goldens.
func TestGenerateWiringGolden(t *testing.T) {
	res, err := httpgen.GenerateWiring(httpgen.WiringOptions{SecurityImport: realSecurityImport})
	if err != nil {
		t.Fatalf("generate wiring: %v", err)
	}

	for name, src := range map[string][]byte{"middleware.go": res.MiddlewareGo, "server.go": res.ServerGo} {
		if _, err := parser.ParseFile(token.NewFileSet(), name, src, parser.AllErrors); err != nil {
			t.Fatalf("emitted %s does not parse: %v", name, err)
		}
	}

	mw := string(res.MiddlewareGo)
	for _, want := range []string{
		realSecurityImport,
		"security.Validator",
		"security.Middleware(validator)",
		"security.WithPrincipal(",
		"security.Principal",
		"security.NewError(security.ErrUnauthenticated)",
	} {
		if !strings.Contains(mw, want) {
			t.Errorf("middleware does not reference %q", want)
		}
	}

	srv := string(res.ServerGo)
	for _, want := range []string{
		"func NewServer(dev DevConfig, validator security.Validator, registrars ...Registrar) http.Handler",
		"AuthMiddleware(dev, validator)",
		`root.Handle("/api/v1/", authMW(api))`,
		`root.HandleFunc("GET /healthz", handleHealth)`,
		`root.HandleFunc("GET /readyz", handleHealth)`,
	} {
		if !strings.Contains(srv, want) {
			t.Errorf("server wiring does not contain %q", want)
		}
	}

	checkGolden(t, "../testdata/wiring.middleware.go.golden", res.MiddlewareGo)
	checkGolden(t, "../testdata/wiring.server.go.golden", res.ServerGo)
}

// TestWiringSampleInSync verifies the committed compile-proof wiring sample
// (internal/sample/wiring, generated against the REAL security package and built
// by `go build ./...`) is regenerated byte-identically.
func TestWiringSampleInSync(t *testing.T) {
	res, err := httpgen.GenerateWiring(httpgen.WiringOptions{
		Package:        "wiring",
		SecurityImport: realSecurityImport,
	})
	if err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "../internal/sample/wiring/middleware.gen.go", res.MiddlewareGo)
	checkGolden(t, "../internal/sample/wiring/server.gen.go", res.ServerGo)
}

func checkGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
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
