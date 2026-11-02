package modelgen_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/clientgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/temporalgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// proofRoot is where a proof tree is generated: inside this module, so its packages resolve
// the real framework and SDK at the versions this module pins, with no module of their own.
const (
	proofRoot   = "../internal/sample"
	proofModule = "github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/sample"
)

// proofContracts is one small project: a Manager with a dependency and machine and REST
// clients over it, every generator's whole output from a fresh project's point of view.
type proofContracts struct {
	Dep, Manager string // the contract documents
}

// proofDep is a dependency contract whose interface is named iface: one operation per result
// shape, each over the param, so the param meets every body the generators write.
func proofDep(layer, iface, op, params string) string {
	return fmt.Sprintf(`{"component":"proofDep","layer":%q,"goPackage":"internal/%s/proofdep","stub":true,"title":"dep","$defs":{"Thing":{"type":"string"}},
	"interface":{"name":%q,"layer":%q,"operations":[
	  {"name":%q,"params":[%s],"result":{"type":"string"},"error":true},
	  {"name":"ZCount","params":[%s],"result":{"type":"integer"},"error":true},
	  {"name":"ZDrop","params":[%s],"error":true}%s]}}`,
		layer, strings.ToLower(layer), iface, strings.ToLower(layer), op, params, params, params, totalOps(layer, params))
}

// totalOps are a Utility's operations that declare no error, which only a Utility may.
func totalOps(layer, params string) string {
	if layer != "Utility" {
		return ""
	}
	return fmt.Sprintf(`,{"name":"ZNow","params":[%s],"result":{"type":"integer"},"error":false},{"name":"ZTick","params":[%s],"error":false}`, params, params)
}

// proofManager is a Manager over the dependency, with a read and a write operation over one
// param, so the param reaches both the query binding and the body binding of the REST client.
func proofManager(params string) string {
	return fmt.Sprintf(`{"component":"proofManager","layer":"Manager","goPackage":"internal/manager/proofm","title":"m","$defs":{"Thing":{"type":"string"}},
	"deps":[{"name":"dep","component":"proofDep"}],
	"interface":{"name":"ProofManager","layer":"manager","operations":[
	  {"name":"ListThings","params":[%s],"result":{"type":"string"},"error":true},
	  {"name":"SaveThing","params":[%s],"error":true}]}}`, params, params)
}

const proofSystem = `{"components":[
  {"id":"mcp-client","name":"MCPClient","kind":"client","uiSurface":false,"machineProtocol":"mcp"},
  {"id":"web-api","name":"WebApi","kind":"client","uiSurface":false,"machineProtocol":"rest"},
  {"id":"proof-manager","name":"ProofManager","kind":"manager","contractKey":"proofManager"}],
  "relationships":[{"from":"mcp-client","to":"proof-manager"},{"from":"web-api","to":"proof-manager"}]}`

// writeProofProject generates every file a fresh project would, under the tree dir, and adds
// the hand-written Manager. It returns the generators' refusal, if any.
func writeProofProject(t *testing.T, dir string, pc proofContracts, managerParamTypes string) error {
	t.Helper()
	module := proofModule + "/" + filepath.Base(filepath.Dir(dir)) + "/" + filepath.Base(dir)
	project := []byte(`{"serviceContracts":{"proofDep":` + pc.Dep + `,"proofManager":` + pc.Manager + `}}`)
	files := map[string][]byte{}

	contracts, err := modelgen.Generate(project, modelgen.Config{ModulePath: module})
	if err != nil {
		return err
	}
	for pkg, src := range contracts {
		files[path.Join(pkg, "contract.gen.go")] = src
	}
	fakes, err := modelgen.GenerateFakes(project, modelgen.Config{ModulePath: module})
	if err != nil {
		return err
	}
	for pkg, src := range fakes {
		files[path.Join(pkg, "fake.gen.go")] = src
	}
	if err := addTemporalFiles(project, module, files); err != nil {
		return err
	}
	if err := addClientFiles(pc, module, files); err != nil {
		return err
	}
	files["internal/manager/proofm/impl.go"] = []byte(fmt.Sprintf(`package proofm

import (
	"github.com/mixofreality-studio/archistrator-platform/framework-go/manager"

	dep %q
)

type proofManager struct{ dep dep.%s }

func newProofManager(d dep.%s) *proofManager { return &proofManager{dep: d} }

func (*proofManager) ListThings(_ manager.Context%s) (string, error) { return "", nil }

func (*proofManager) SaveThing(_ manager.Context%s) error { return nil }

func (m *proofManager) WorkerManifest() genWorkerManifest {
	return genWorkerManifest{Activities: genActivities{Dep: m.dep}}
}
`, module+"/"+depPackage(t, pc.Dep), depInterface(t, pc.Dep), depInterface(t, pc.Dep), managerParamTypes, managerParamTypes))
	for rel, src := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, src, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return nil
}

func depField(t *testing.T, doc, field string) string {
	t.Helper()
	var d struct {
		GoPackage string `json:"goPackage"`
		Interface struct {
			Name string `json:"name"`
		} `json:"interface"`
	}
	if err := json.Unmarshal([]byte(doc), &d); err != nil {
		t.Fatal(err)
	}
	if field == "package" {
		return d.GoPackage
	}
	return d.Interface.Name
}

func depPackage(t *testing.T, doc string) string   { return depField(t, doc, "package") }
func depInterface(t *testing.T, doc string) string { return depField(t, doc, "interface") }

func addTemporalFiles(project []byte, module string, files map[string][]byte) error {
	m, err := projectmodel.Load(project)
	if err != nil {
		return err
	}
	out, err := temporalgen.Generate(m, temporalgen.Config{ModulePath: module, ManagerKey: "proofManager"})
	if err != nil {
		return err
	}
	for name, src := range out {
		files[path.Join("internal/manager/proofm", name)] = src
	}
	return nil
}

func addClientFiles(pc proofContracts, module string, files map[string][]byte) error {
	var sys projectmodel.System
	if err := json.Unmarshal([]byte(proofSystem), &sys); err != nil {
		return err
	}
	out, err := clientgen.Generate(clientgen.Project{
		Contracts: map[string]json.RawMessage{"proofManager": json.RawMessage(pc.Manager)},
		System:    &sys,
	}, clientgen.Config{ModulePath: module, OpDocs: map[string]map[string]string{"ProofManager": {"ListThings": "list", "SaveThing": "save"}}})
	if err != nil {
		return err
	}
	for rel, src := range out {
		// The MCP tables need an SDK this module does not require, and they reach a param
		// only as a field of their input struct, never as a local.
		if strings.HasSuffix(rel, ".go") && !strings.HasPrefix(rel, "internal/client/mcp/") {
			files[rel] = src
		}
	}
	return nil
}

var buildFailure = regexp.MustCompile(`(?m)^# (\S+)`)

// buildProofTree builds every package under the proof tree and returns the top-level
// directory of each package that failed.
func buildProofTree(t *testing.T, root string) map[string]string {
	t.Helper()
	rel, err := filepath.Rel("..", root)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "./"+filepath.ToSlash(rel)+"/...")
	cmd.Dir = ".."
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	failed := map[string]string{}
	if err == nil {
		return failed
	}
	prefix := proofModule + "/" + filepath.Base(root) + "/"
	matches := buildFailure.FindAllStringSubmatch(string(out), -1)
	for _, m := range matches {
		name, _, _ := strings.Cut(strings.TrimPrefix(m[1], prefix), "/")
		failed[name] = string(out)
	}
	if len(matches) == 0 {
		t.Fatalf("go build failed without naming a package: %v\n%s", err, out)
	}
	return failed
}

func proofTree(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp(proofRoot, "zproof")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

// TestValidateContractMatchesWhatCompiles pins the record-time rule to the generators: a
// contract the rule accepts generates and compiles through every generator, and one it
// refuses does not, on a fresh project with no hand-trimmed shape. Each row is one project.
func TestValidateContractMatchesWhatCompiles(t *testing.T) {
	type row struct{ name, layer, iface, op, param string }
	access := func(iface, op, param string) row {
		return row{iface + "." + op + "." + param, "ResourceAccess", iface, op, param}
	}
	utility := func(iface, op, param string) row {
		return row{"utility " + iface + "." + op + "." + param, "Utility", iface, op, param}
	}
	rows := []row{
		access("TodoAccess", "Save", "todo"), access("TodoAccess", "Type", "kind"), access("TodoAccess", "Save", "len"), access("TodoAccess", "Save", "true"), access("TodoAccess", "Save", "req"),
		utility("PubSub", "Publish", "topic"), utility("Pub", "Save", "key"),
		// RUN-009's C-pub-sub wall and the rest of its class.
		utility("PubSub", "publish", "topic"), utility("pubSub", "Publish", "topic"),
		access("TodoAccess", "Pub-lish", "x"), access("Todo-Access", "Save", "x"),
		access("TodoAccess", "Save", "type"), access("TodoAccess", "Save", "func"), access("TodoAccess", "Save", "1x"), access("TodoAccess", "Save", "Pub-lish"),
		access("TodoAccess", "Save", "string"), access("TodoAccess", "Save", "nil"),
	}
	for _, reserved := range modelgen.ReservedParamNames() {
		rows = append(rows, access("TodoAccess", "Save", reserved))
	}
	root := proofTree(t)
	refused := map[string]bool{}
	dirs := map[string]row{}
	for i, r := range rows {
		param := fmt.Sprintf(`{"name":%q,"schema":{"type":"string"}}`, r.param)
		dep := proofDep(r.layer, r.iface, r.op, param)
		manager := proofManager(param)
		if err := modelgen.ValidateContract("k", []byte(dep)); err != nil {
			refused[r.name] = true
		}
		if err := modelgen.ValidateContract("m", []byte(manager)); err != nil {
			refused[r.name] = true
		}
		dir := filepath.Join(root, fmt.Sprintf("p%d", i))
		dirs[filepath.Base(dir)] = r
		if err := writeProofProject(t, dir, proofContracts{Dep: dep, Manager: manager}, ", _ string"); err != nil {
			t.Logf("%s: the generators refuse it: %v", r.name, err)
			delete(dirs, filepath.Base(dir))
			if !refused[r.name] {
				t.Errorf("%s: ValidateContract accepted what generation refuses: %v", r.name, err)
			}
		}
	}
	failed := buildProofTree(t, root)
	for dir, r := range dirs {
		out, broke := failed[dir]
		switch {
		case broke && !refused[r.name]:
			t.Errorf("%s: ValidateContract accepted what does not compile:\n%s", r.name, projectLines(out, dir))
		case !broke && refused[r.name]:
			t.Errorf("%s: ValidateContract refused what compiles", r.name)
		}
	}
}

// inlineSchemas are the inline shapes a contract's author writes without naming them, with the
// Go type the component's own interface gets for each.
var inlineSchemas = []struct{ schema, typ string }{
	{`{"type":"string"}`, "string"},
	{`{"type":"integer"}`, "int64"},
	{`{"type":"integer","minimum":0}`, "int64"},
	{`{"type":["integer","null"]}`, "int64"},
	{`{"type":"number"}`, "float64"},
	{`{"type":"boolean"}`, "bool"},
	{`{"type":"array","items":{"type":"integer"}}`, "[]int64"},
	{`{"type":"array","items":{"type":"array","items":{"type":"string"}}}`, "[][]string"},
	{`{"type":"object"}`, "map[string]any"},
	{`{"type":"object","additionalProperties":{"type":"integer"}}`, "map[string]int64"},
	{`{"type":"object","additionalProperties":{"type":"array","items":{"type":"boolean"}}}`, "map[string][]bool"},
	{`{"type":"object","additionalProperties":true}`, "map[string]any"},
	{`{"type":"object","additionalProperties":false}`, "map[string]any"},
	{`{"type":"object","properties":{"a":{"type":"string"}},"additionalProperties":{"type":"integer"}}`, "map[string]any"},
	{`{"type":"object","properties":{},"additionalProperties":{"type":"string"}}`, "map[string]string"},
	{`{}`, "any"},
	{`{"$ref":"#/$defs/Thing"}`, "alias.Thing"},
	{`{"type":"array","items":{"$ref":"#/$defs/Thing"}}`, "[]alias.Thing"},
	{`{"type":"object","additionalProperties":{"$ref":"#/$defs/Thing"}}`, "map[string]alias.Thing"},
}

// TestInlineSchemaMappersAgree holds the two type mappers together: modelgen spells a
// component's interface and projectmodel spells every caller of it, so any disagreement is a
// generated file that does not compile, in a package no agent may edit.
func TestInlineSchemaMappersAgree(t *testing.T) {
	for _, tc := range inlineSchemas {
		t.Run(tc.schema, func(t *testing.T) {
			var s jsonschema.Schema
			if err := json.Unmarshal([]byte(tc.schema), &s); err != nil {
				t.Fatal(err)
			}
			got, _ := modelgen.GoTypeFor(&s, "alias")
			var n projectmodel.SchemaNode
			if err := json.Unmarshal([]byte(tc.schema), &n); err != nil {
				t.Fatal(err)
			}
			caller := projectmodel.GoType(&n, false, "alias")
			if norm(got) != norm(caller) || norm(got) != tc.typ {
				t.Errorf("modelgen says %s and projectmodel says %s; both must say %s", got, caller, tc.typ)
			}
		})
	}
}

func norm(goType string) string { return strings.ReplaceAll(goType, "interface{}", "any") }

// A fresh contract's inline types reach a Manager through its activity and invoker and the
// REST client's query and body bindings, and all of it compiles. RUN-009's todoListAccess
// declared result {type: integer, minimum: 0} and stopped every package above it.
func TestInlineSchemasCompileThroughEveryGenerator(t *testing.T) {
	root := proofTree(t)
	var names []string
	for i, tc := range inlineSchemas {
		if strings.Contains(tc.typ, "alias") || tc.typ == "any" {
			continue // a named type or an untyped value takes the path every fixture already takes
		}
		param := fmt.Sprintf(`{"name":"value","schema":%s}`, tc.schema)
		dep := proofDep("ResourceAccess", "TodoAccess", "Save", param)
		dep = strings.Replace(dep, `"result":{"type":"string"}`, `"result":`+tc.schema, 1)
		manager := proofManager(param)
		if err := modelgen.ValidateContract("k", []byte(dep)); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(root, fmt.Sprintf("p%d", i))
		if err := writeProofProject(t, dir, proofContracts{Dep: dep, Manager: manager}, ", _ "+tc.typ); err != nil {
			t.Fatalf("%s: %v", tc.schema, err)
		}
		names = append(names, tc.schema)
	}
	failed := buildProofTree(t, root)
	for dir, out := range failed {
		t.Errorf("%s does not compile:\n%s", dir, projectLines(out, dir))
	}
	if len(names) == 0 {
		t.Fatal("the table proved nothing")
	}
}

// projectLines is the part of a build log about one project of the proof tree.
func projectLines(out, dir string) string {
	var keep []string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "/"+dir+"/") {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, "\n")
}
