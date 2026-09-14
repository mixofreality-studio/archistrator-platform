package webgen

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

var update = flag.Bool("update", false, "rewrite golden files and the pinned reference diffs")

const (
	testdata  = "../testdata/webgen"
	reference = testdata + "/archistrator-bcb20abf" // archistrator's webApp files at bcb20abf, verbatim
	goldenDir = testdata + "/archistrator.golden"   // webgen's output for archistrator
)

// archistratorInputs is the would-be archistrator webApp/webgen.json over the
// OAS and MCP tool tables archistrator committed at bcb20abf.
func archistratorInputs(t *testing.T) Inputs {
	t.Helper()
	c, err := LoadConfig(filepath.Join(testdata, "archistrator.webgen.json"))
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join(testdata, "archistrator.openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ParseYAML(src)
	if err != nil {
		t.Fatal(err)
	}
	tools, err := LoadMCPTools(filepath.Join(testdata, "archistrator-mcp"))
	if err != nil {
		t.Fatal(err)
	}
	return Inputs{Config: c, OAS: doc, Tools: tools}
}

// referenceOf maps each output to the bcb20abf file it replaces. An output with
// no entry is new (fixtures.gen.test.ts replaces a hand-written .mjs test that
// exercised the retired JS generator, not the fixtures).
var referenceOf = map[string]string{
	"src/api/ops.gen.ts":           "ops.gen.ts",
	"preview/fixtures.schema.json": "fixtures.schema.json",
	"src/contracts/errors.ts":      "errors.ts",
	"src/api/opsContext.tsx":       "opsContext.tsx",
	"src/api/opTypes.ts":           "opTypes.ts",
	"src/api/fixtureOps.ts":        "fixtureOps.ts",
	"src/previewShell/main.tsx":    "previewShell.main.tsx",
	"preview.html":                 "preview.html",
	"vite.preview.config.ts":       "vite.preview.config.ts",
}

// identical are the outputs that must reproduce archistrator's bytes exactly.
var identical = map[string]bool{
	"src/contracts/errors.ts": true,
	"src/api/opsContext.tsx":  true,
	"src/api/opTypes.ts":      true,
}

// TestArchistratorGolden is the P2 golden: webgen run over archistrator's
// committed inputs must reproduce archistrator's committed webApp files, or
// differ only as pinned in <reference>/<file>.diff. Each pinned diff is
// reviewed and explained (preview-p2-report.md):
//
//   - ops.gen.ts: six comment lines that named the retired JS generator. Every
//     line of code, OP_BINDINGS included, is byte-identical.
//   - fixtures.schema.json: the one description string that named the retired
//     generator. The 48 op schemas and every definition are byte-identical.
//   - errors.ts, opsContext.tsx, opTypes.ts: identical, no diff file.
//   - fixtureOps.ts: the runtime moved to framework-web; the scaffold binds it.
//   - previewShell/main.tsx, preview.html, vite.preview.config.ts: the generic
//     scaffold versus archistrator's hand-tuned files (fonts, its router context,
//     the in-config OAS read); the scaffold never overwrites them.
func TestArchistratorGolden(t *testing.T) {
	in := archistratorInputs(t)
	gen, err := Generate(in)
	if err != nil {
		t.Fatal(err)
	}
	scaf, err := Scaffold(in.Config)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range append(gen, scaf...) {
		t.Run(f.Path, func(t *testing.T) {
			checkGolden(t, filepath.Join(goldenDir, f.Path), f.Content)
			checkReference(t, f)
		})
	}
}

func checkGolden(t *testing.T, golden string, got []byte) {
	t.Helper()
	if *update {
		writeTestFile(t, golden, got)
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("output differs from %s (run with -update and review):\n%s", golden, lineDiff(string(want), string(got)))
	}
}

func checkReference(t *testing.T, f File) {
	t.Helper()
	ref, ok := referenceOf[f.Path]
	if !ok {
		return
	}
	refBytes, err := os.ReadFile(filepath.Join(reference, ref))
	if err != nil {
		t.Fatal(err)
	}
	diff := lineDiff(string(refBytes), string(f.Content))
	if identical[f.Path] {
		if diff != "" {
			t.Errorf("%s must be byte-identical to archistrator@bcb20abf:\n%s", f.Path, diff)
		}
		return
	}
	pinned := filepath.Join(reference, ref+".diff")
	if *update {
		writeTestFile(t, pinned, []byte(diff))
	}
	want, err := os.ReadFile(pinned)
	if err != nil {
		t.Fatalf("read pinned diff (run with -update): %v", err)
	}
	if diff != string(want) {
		t.Errorf("%s differs from archistrator@bcb20abf other than as pinned in %s:\n--- got diff:\n%s", f.Path, pinned, diff)
	}
}

func writeTestFile(t *testing.T, p string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// The ops.gen.ts diff is exactly the six comment lines that named the retired
// JS generator, and nothing else. It is spelled out here, not read back from the
// pinned file alone, because `-update` rewrites the pinned file: a code change
// that -update pinned would otherwise pass as long as it looked like a comment.
const wantOpsDiff = `@@ -1,1 +1,1 @@
-// Code generated by scripts/gen-ops.mjs. DO NOT EDIT.
+// Code generated by webgen (framework-go-app-generator). DO NOT EDIT.
@@ -7,1 +7,1 @@
- *  - fixtureOpsClient (hand-written, src/api/fixtureOps.ts): answers from
+ *  - fixtureOpsClient (src/api/fixtureOps.ts, over framework-web/preview): answers from
@@ -11,1 +11,1 @@
- * (see scripts/gen-ops.mjs): every '/api/v1/<mgr>/<op>[/{...}]' path yields an
+ * (see framework-go-app-generator/webgen): every '/api/v1/<mgr>/<op>[/{...}]' path yields an
@@ -17,1 +17,1 @@
- * read from the generated Go tool tables: scripts/mcp-tools.mjs), or null where
+ * read from the generated Go tool tables: webgen.LoadMCPTools), or null where
@@ -20,1 +20,1 @@
- * The composition routes (scripts/composition-routes.mjs) are bound here too,
+ * The composition routes (webgen.json "composition") are bound here too,
@@ -329,1 +329,1 @@
-// gen-ops.mjs deriving both from the same source, not by this cast).
+// webgen deriving both from the same source, not by this cast).
`

// The fixture-schema diff is exactly the one description string that named the
// retired JS generator; every op schema and definition is byte-identical.
const wantSchemaDiff = `@@ -4,1 +4,1 @@
-  "description": "Generated from server/api/openapi.yaml by webApp/scripts/fixture-schema.mjs. Each ops key is an OpsClient OpId; each answer is a result, an error, or pending.",
+  "description": "Generated from ../server/api/openapi.yaml by framework-go-app-generator/webgen. Each ops key is an OpsClient OpId; each answer is a result, an error, or pending.",
`

func TestPinnedDiffsAreExactlyTheReviewedOnes(t *testing.T) {
	in := archistratorInputs(t)
	gen, err := Generate(in)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"src/api/ops.gen.ts": wantOpsDiff, "preview/fixtures.schema.json": wantSchemaDiff}
	for _, f := range gen {
		w, ok := want[f.Path]
		if !ok {
			continue
		}
		delete(want, f.Path)
		ref, err := os.ReadFile(filepath.Join(reference, referenceOf[f.Path]))
		if err != nil {
			t.Fatal(err)
		}
		if got := lineDiff(string(ref), string(f.Content)); got != w {
			t.Errorf("%s differs from archistrator@bcb20abf other than in the reviewed lines:\n%s", f.Path, got)
		}
		pinned, err := os.ReadFile(filepath.Join(reference, referenceOf[f.Path]+".diff"))
		if err != nil {
			t.Fatal(err)
		}
		if string(pinned) != w {
			t.Errorf("%s.diff is not the reviewed diff:\n%s", referenceOf[f.Path], pinned)
		}
	}
	if len(want) != 0 {
		t.Errorf("no generated output for %v", want)
	}
}

// The preview page's own CSP is the full preview response CSP less
// frame-ancestors (framework-web PREVIEW_META_CSP; its headless test serves this
// template with no response headers and proves nothing leaves the page).
const wantPreviewMetaCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; font-src 'self' data:; connect-src 'none'; form-action 'none'; " +
	"base-uri 'none'; object-src 'none'; frame-src 'none'; worker-src 'none'"

func TestPreviewHTMLCarriesTheFullCSP(t *testing.T) {
	scaf, err := Scaffold(archistratorInputs(t).Config)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range scaf {
		if f.Path != "preview.html" {
			continue
		}
		page := string(f.Content)
		if n := strings.Count(page, `http-equiv="Content-Security-Policy"`); n != 1 {
			t.Fatalf("want exactly one CSP meta, got %d", n)
		}
		if !strings.Contains(page, `content="`+wantPreviewMetaCSP+`"`) {
			t.Errorf("preview.html's CSP is not the full preview CSP:\n%s", page)
		}
		if strings.Index(page, "Content-Security-Policy") > strings.Index(page, "<script") {
			t.Error("the CSP meta must come before any script")
		}
		return
	}
	t.Fatal("no preview.html in the scaffold")
}

func TestArchistratorBindings(t *testing.T) {
	bindings, err := archistratorInputs(t).Bindings()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Binding{}
	var noTool []string
	for _, b := range bindings {
		byID[b.OpID] = b
		if b.Tool == "" {
			noTool = append(noTool, b.OpID)
		}
	}
	if len(bindings) != 48 {
		t.Errorf("want 45 OAS ops + 3 composition routes, got %d", len(bindings))
	}
	if strings.Join(noTool, ",") != "compositionGetCapabilities,compositionGetOperatedAppId,compositionGetUserinfo" {
		t.Errorf("only the composition routes may lack a tool, got %v", noTool)
	}
	// The tool comes from the server's table, not the path (preview P1b).
	if got := byID["projectDesignRequestSdpCommit"].Tool; got != "projectDesignRequestSDPCommit" {
		t.Errorf("request-sdp-commit binds tool %q", got)
	}
}

func TestLoadMCPTools(t *testing.T) {
	tools, err := LoadMCPTools(filepath.Join(testdata, "archistrator-mcp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 45 || !tools["projectDesignSubmitSDPDecision"] {
		t.Errorf("want archistrator's 45 registered tools, got %d", len(tools))
	}
	if _, err := LoadMCPTools(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("a missing tool directory must be loud")
	}
	if _, err := LoadMCPTools(t.TempDir()); err == nil {
		t.Error("a directory with no registration must be loud")
	}
}

func TestLoadMCPToolsParsesRegistrationsOnly(t *testing.T) {
	dir := t.TempDir()
	src := `package x
import "github.com/modelcontextprotocol/go-sdk/mcp"
// mcp.AddTool(srv, &mcp.Tool{Name: "inAComment"}, h)
func reg(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Description: "d", Name: "realTool"}, nil)
	_ = "mcp.AddTool(srv, &mcp.Tool{Name: \"inAString\"}"
}
`
	writeTestFile(t, filepath.Join(dir, "m", "m_tools.gen.go"), []byte(src))
	tools, err := LoadMCPTools(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || !tools["realTool"] {
		t.Errorf("want exactly realTool, got %v", tools)
	}
}

func oas(t *testing.T, yamlSrc string) Value {
	t.Helper()
	v, err := ParseYAML([]byte(yamlSrc))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestDeriveBindingsRefusals(t *testing.T) {
	cases := map[string]string{
		"shape":       "paths:\n  /v1/x/y:\n    get: {operationId: Y}\n",
		"operationId": "paths:\n  /api/v1/x/y:\n    get: {}\n",
		"duplicate":   "paths:\n  /api/v1/x/y:\n    get: {operationId: Y}\n  /api/v1/x/y/{id}:\n    get: {operationId: Y}\n",
	}
	for name, src := range cases {
		if _, err := DeriveBindings(oas(t, src), nil); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	derived, err := DeriveBindings(oas(t, "paths:\n  /api/v1/x/y:\n    get: {operationId: Y}\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WithComposition(derived, []CompositionRoute{{OpID: "xY", Method: "GET", Path: "/z"}}); err == nil {
		t.Error("a composition route colliding with a derived op must fail")
	}
}

func TestCollateMatchesLocaleCompare(t *testing.T) {
	// String.prototype.localeCompare (ICU root) orders these as listed.
	want := []string{"a", "A", "abx", "abY", "b", "B", "c1", "cA"}
	for i := 0; i+1 < len(want); i++ {
		if collate(want[i], want[i+1]) >= 0 {
			t.Errorf("want %q before %q", want[i], want[i+1])
		}
	}
	if _, err := sortBindings([]Binding{{OpID: "a_b"}}); err == nil {
		t.Error("an opId outside [A-Za-z0-9] must be refused")
	}
}

func TestStringifyMatchesJSON(t *testing.T) {
	numbers := map[float64]string{
		0: "0", 1: "1", -1.5: "-1.5", 400: "400", 1e21: "1e+21", 1e20: "100000000000000000000",
		1e-7: "1e-7", 0.000001: "0.000001", 1.25e-10: "1.25e-10", 123.456: "123.456",
	}
	for f, want := range numbers {
		if got := jsNumber(f); got != want {
			t.Errorf("jsNumber(%v) = %s, want %s", f, got, want)
		}
	}
	strs := map[string]string{
		"<a & b>": `"<a & b>"`, "\u2028": "\"\u2028\"", "\x01\x1b": `"\u0001\u001b"`, "q\"\\\n": `"q\"\\\n"`,
	}
	for s, want := range strs {
		if got := quoteJS(s); got != want {
			t.Errorf("quoteJS(%q) = %s, want %s", s, got, want)
		}
	}
	o := NewObject().Set("b", 1.0).Set("2", 2.0).Set("a", []Value{}).Set("1", NewObject()).Set("01", nil)
	want := "{\n  \"1\": {},\n  \"2\": 2,\n  \"b\": 1,\n  \"a\": [],\n  \"01\": null\n}"
	if got := Stringify(o); got != want {
		t.Errorf("Stringify:\n%s\nwant:\n%s", got, want)
	}
}

func TestParseYAMLRefusesMergeKeys(t *testing.T) {
	if _, err := ParseYAML([]byte("a: &x {b: 1}\nc:\n  <<: *x\n")); err == nil {
		t.Error("merge keys must be refused")
	}
}

// resolvedFixtureSchema compiles the generated archistrator fixture schema with
// an independent validator (jsonschema-go, draft-07), so skeletons and fixtures
// are checked by something other than the code that wrote the schema.
func resolvedFixtureSchema(t *testing.T, in Inputs, bindings []Binding) *jsonschema.Resolved {
	t.Helper()
	var s jsonschema.Schema
	if err := json.Unmarshal([]byte(Stringify(FixtureSchema(in, bindings))), &s); err != nil {
		t.Fatal(err)
	}
	r, err := s.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func asJSON(t *testing.T, v Value) any {
	t.Helper()
	var out any
	if err := json.Unmarshal([]byte(Stringify(v)), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSkeletonIsSchemaValidForEveryOp(t *testing.T) {
	in := archistratorInputs(t)
	bindings, err := in.Bindings()
	if err != nil {
		t.Fatal(err)
	}
	schema := FixtureSchema(in, bindings)
	resolved := resolvedFixtureSchema(t, in, bindings)
	for _, b := range bindings {
		skel, err := Skeleton(schema, "/project/demo", []string{b.OpID})
		if err != nil {
			t.Errorf("%s: %v", b.OpID, err)
			continue
		}
		if err := resolved.Validate(asJSON(t, skel)); err != nil {
			t.Errorf("%s: the skeleton fails the fixture schema: %v", b.OpID, err)
		}
	}
	if _, err := Skeleton(schema, "/x", []string{"systemDesignGetProjct"}); err == nil {
		t.Error("a skeleton for an op the transport lacks must fail")
	}
	if _, err := Skeleton(schema, "x", nil); err == nil {
		t.Error("a skeleton route must be absolute")
	}
}

// archistrator's own test-local fixtures (validated by ajv at bcb20abf) pass the
// generated schema under an independent validator too; a drifted one fails.
func TestReferenceFixturesValidate(t *testing.T) {
	in := archistratorInputs(t)
	bindings, err := in.Bindings()
	if err != nil {
		t.Fatal(err)
	}
	resolved := resolvedFixtureSchema(t, in, bindings)
	files, _ := filepath.Glob(filepath.Join(reference, "fixtures", "web-client", "*", "*.json"))
	if len(files) < 4 {
		t.Fatalf("want the reference fixtures, found %d", len(files))
	}
	for _, f := range files {
		var v any
		src, _ := os.ReadFile(f)
		if err := json.Unmarshal(src, &v); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(v); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	drifted := map[string]any{"route": "/", "ops": map[string]any{
		"compositionGetCapabilities": map[string]any{"result": map[string]any{"operations": "yes"}},
	}}
	if resolved.Validate(drifted) == nil {
		t.Error("a result that drifted from its contract must fail")
	}
}

func TestScaffoldIsWriteOnceAndDriftIsDetected(t *testing.T) {
	dir := t.TempDir()
	in := archistratorInputs(t)
	scaf, err := Scaffold(in.Config)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "preview.html"), []byte("the app's own"))
	wrote, err := WriteScaffold(dir, scaf)
	if err != nil {
		t.Fatal(err)
	}
	if len(wrote) != len(scaf)-1 {
		t.Errorf("want every scaffold file but the existing one, wrote %v", wrote)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "preview.html")); string(got) != "the app's own" {
		t.Error("the scaffold overwrote an app-owned file")
	}
	gen, err := Generate(in)
	if err != nil {
		t.Fatal(err)
	}
	if stale, _ := Drift(dir, gen); len(stale) != len(gen) {
		t.Errorf("want every generated file stale before generate, got %v", stale)
	}
	if err := Write(dir, gen); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "src/api/ops.gen.ts"), []byte("edited by hand"))
	if stale, _ := Drift(dir, gen); strings.Join(stale, ",") != "src/api/ops.gen.ts" {
		t.Errorf("want only the edited file stale, got %v", stale)
	}
}

func TestConfig(t *testing.T) {
	content, err := DefaultConfig("gtd-app", "web-client")
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseConfig(content)
	if err != nil {
		t.Fatal(err)
	}
	if c.FixturesEnv != "GTD_APP_PREVIEW_FIXTURES" || len(c.Composition) != 1 || c.Composition[0].OpID != "compositionGetUserinfo" ||
		strings.Join(c.RestOnly, ",") != "compositionGetUserinfo" {
		t.Errorf("default config: %+v", c)
	}
	bad := []string{
		`{"surface":"web-client","oas":"a","mcpTools":"b","fixturesEnv":"X"}`,
		`{"app":"a","surface":"Web","oas":"a","mcpTools":"b","fixturesEnv":"X"}`,
		`{"app":"a","surface":"web","oas":"a","mcpTools":"b","fixturesEnv":"x-y"}`,
		`{"app":"a","surface":"web","oas":"a","mcpTools":"b","fixturesEnv":"X","composition":{"c":{"method":"GET"}}}`,
	}
	for _, src := range bad {
		if _, err := ParseConfig([]byte(src)); err == nil {
			t.Errorf("want an error for %s", src)
		}
	}
}
