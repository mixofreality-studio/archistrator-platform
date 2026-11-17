package appgen

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing/fstest"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/nullgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/plan"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/testgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/webgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// ArtifactKind is a record verb's artifact kind (D2).
type ArtifactKind string

// The artifact kinds the record verbs admit.
const (
	KindServiceContract     ArtifactKind = "serviceContract"
	KindTestPlan            ArtifactKind = "testPlan"
	KindUIDesign            ArtifactKind = "uiDesign"
	KindAcceptancePlan      ArtifactKind = "acceptancePlan"
	KindSystem              ArtifactKind = "system"
	KindOperationalConcepts ArtifactKind = "operationalConcepts"
	KindCoreUseCases        ArtifactKind = "coreUseCases"
)

// ArtifactKinds is the seven, in declaration order.
func ArtifactKinds() []ArtifactKind {
	return []ArtifactKind{KindServiceContract, KindTestPlan, KindUIDesign, KindAcceptancePlan, KindSystem, KindOperationalConcepts, KindCoreUseCases}
}

// Input is what a consumer reads.
type Input struct {
	Project    []byte // the project.json bytes
	ModulePath string // the go.mod module line
	// ModuleDir is the Go module's repo-relative dir ("" or "server"): every Output.Path is
	// repo-relative, so the module's files are joined under it. A command that writes into the
	// module itself passes "" and so gets module-relative paths (which may climb out of it:
	// testgen's Playwright output is ../uitests/generated).
	ModuleDir string
	Config    Config
	// Tree is the module tree as it stands, rooted at ModuleDir; nil means every contract with a
	// goPackage counts as built (the emission of a fully built module).
	Tree fs.FS

	// UITestsDir is testgen's Playwright output directory relative to the module; empty means
	// uitests/generated at the repository root.
	UITestsDir string
	// Only, for modelgen, names the contract keys to generate (a construction task names its own);
	// empty means every contract whose package holds code, or is flagged a stub.
	Only []string
	// WebTree is the webApp directory as it stands (nil: none); its webgen.json, when present,
	// configures `webgen generate`, else the config `webgen bootstrap` writes applies.
	WebTree fs.FS
	// Warn receives what a generation leaves undone without failing; nil drops it.
	Warn io.Writer

	// memo, set only by a pass over every consumer of one document (AdmitTier1), shares what the
	// consumers would each recompute from Project.
	memo *emitMemo
}

// emitMemo is what the consumers of one in-process pass share instead of each recomputing it from
// the same bytes: the loaded model, the decoded project envelope and the `appgen clients`
// emission (`webgen generate` binds to it). Parsing a project is most of what a pass costs, and a
// pass runs inside a workflow task, under Temporal's 1 s deadlock detector (AdmitTier1). The
// consumers only read what the memo holds, and only a pass with no Tree uses it, so wireOnlyBuilt
// (the one writer of a loaded model) has nothing to drop. A pass is single-goroutine.
type emitMemo struct {
	model        *projectmodel.Model
	modelErr     error
	modelDone    bool
	decoded      methodcheck.Project
	decodedErr   error
	decodedDone  bool
	clients      []plan.File
	clientsErr   error
	clientsReady bool
}

// load is projectmodel.Load over in.Project, once per pass when the pass has a memo.
func (in Input) load() (*projectmodel.Model, error) {
	if in.memo == nil {
		return projectmodel.Load(in.Project)
	}
	if !in.memo.modelDone {
		in.memo.model, in.memo.modelErr = projectmodel.Load(in.Project)
		in.memo.modelDone = true
	}
	return in.memo.model, in.memo.modelErr
}

// decode is methodcheck.DecodeProject over in.Project, once per pass when the pass has a memo.
func (in Input) decode() (methodcheck.Project, error) {
	if in.memo == nil {
		p, _, err := methodcheck.DecodeProject(in.Project)
		return p, err
	}
	if !in.memo.decodedDone {
		in.memo.decoded, _, in.memo.decodedErr = methodcheck.DecodeProject(in.Project)
		in.memo.decodedDone = true
	}
	return in.memo.decoded, in.memo.decodedErr
}

// Consumer is one generator invocation the platform runs over an artifact.
type Consumer struct {
	Name  string         // "modelgen", "nullgen", "appgen temporal", … (D2)
	Kinds []ArtifactKind // the artifact kinds it reads
	Emit  func(in Input) ([]plan.File, error)
}

// Plan is c's manifest: the Outputs of c.Emit over in with Tree == nil, sorted by path. The
// manifest is the emission of a fully built tree, a superset of any partial emission.
func (c Consumer) Plan(in Input) ([]plan.Output, error) {
	in.Tree, in.Only = nil, nil // WebTree stays: it is the web project's configuration, not what is built
	files, err := c.Emit(in)
	if err != nil {
		return nil, err
	}
	return plan.Outputs(files), nil
}

// AllConsumers is the nine of D2, in CLI order.
func AllConsumers() []Consumer {
	return []Consumer{
		{Name: "modelgen", Kinds: []ArtifactKind{KindServiceContract}, Emit: emitModels},
		{Name: "nullgen", Kinds: []ArtifactKind{KindServiceContract}, Emit: emitNull},
		{Name: "appgen temporal", Kinds: []ArtifactKind{KindServiceContract}, Emit: emitTemporal},
		{Name: "appgen config", Kinds: []ArtifactKind{KindSystem, KindOperationalConcepts}, Emit: emitConfig},
		{Name: "appgen compose", Kinds: []ArtifactKind{KindServiceContract, KindSystem, KindOperationalConcepts}, Emit: emitCompose},
		{Name: "appgen clients", Kinds: []ArtifactKind{KindServiceContract, KindSystem}, Emit: emitClients},
		{Name: "testgen", Kinds: []ArtifactKind{KindServiceContract, KindTestPlan, KindUIDesign, KindAcceptancePlan, KindCoreUseCases, KindSystem}, Emit: emitTests},
		{Name: "webgen bootstrap", Kinds: []ArtifactKind{KindSystem}, Emit: emitWebBootstrap},
		{Name: "webgen generate", Kinds: []ArtifactKind{KindServiceContract}, Emit: emitWebGenerate},
	}
}

// Consumers are those whose Kinds contain kind; nil for an unknown kind.
func Consumers(kind ArtifactKind) []Consumer {
	var out []Consumer
	for _, c := range AllConsumers() {
		if slices.Contains(c.Kinds, kind) {
			out = append(out, c)
		}
	}
	return out
}

// ConsumerNamed finds a consumer by its CLI name.
func ConsumerNamed(name string) (Consumer, bool) {
	for _, c := range AllConsumers() {
		if c.Name == name {
			return c, true
		}
	}
	return Consumer{}, false
}

// repoPath joins a module-relative path under ModuleDir; the result is clean and slash-separated.
func (in Input) repoPath(rel string) string {
	return path.Join(in.ModuleDir, filepath.ToSlash(rel))
}

// generated turns a module-relative emission into Generated plan files, sorted by path.
func (in Input) generated(files map[string][]byte) []plan.File {
	out := make([]plan.File, 0, len(files))
	for rel, content := range files {
		out = append(out, plan.File{Output: plan.Output{Path: in.repoPath(rel), Role: plan.Generated{}}, Content: content})
	}
	plan.Sort(out)
	return out
}

// repoFile is a plan file at a path already relative to the repository.
func repoFile(p string, role plan.OutputRole, content []byte) plan.File {
	return plan.File{Output: plan.Output{Path: p, Role: role}, Content: content}
}

// --- modelgen ---------------------------------------------------------------------------------

func emitModels(in Input) ([]plan.File, error) {
	only := in.Only
	if len(only) == 0 {
		var err error
		if only, err = builtContractKeys(in); err != nil {
			return nil, err
		}
	}
	if len(only) == 0 {
		return nil, nil // nothing built yet: nothing to generate and nothing to drift from
	}
	cfg := modelgen.Config{ModulePath: in.ModulePath, Only: only}
	models, err := modelgen.Generate(in.Project, cfg)
	if err != nil {
		return nil, err
	}
	files := make(map[string][]byte, 2*len(models))
	for goPackage, src := range models {
		files[path.Join(goPackage, "contract.gen.go")] = src
	}
	fakes, err := modelgen.GenerateFakes(in.Project, cfg)
	if err != nil {
		return nil, err
	}
	for fakePackage, src := range fakes {
		files[path.Join(fakePackage, "fake.gen.go")] = src
	}
	return in.generated(files), nil
}

// builtContractKeys is the contracts modelgen generates without an explicit selection: every
// non-client contract with a goPackage that holds code or is flagged a stub.
func builtContractKeys(in Input) ([]string, error) {
	var doc struct {
		ServiceContracts map[string]struct {
			GoPackage string `json:"goPackage"`
			Layer     string `json:"layer"`
			Stub      bool   `json:"stub"`
		} `json:"serviceContracts"`
	}
	if err := json.Unmarshal(in.Project, &doc); err != nil {
		return nil, fmt.Errorf("parse project.json: %w", err)
	}
	var out []string
	for key, c := range doc.ServiceContracts {
		if c.GoPackage == "" || strings.EqualFold(c.Layer, "client") {
			continue
		}
		if c.Stub || in.Tree == nil || holdsGoFile(in.Tree, c.GoPackage) {
			out = append(out, key)
		}
	}
	slices.Sort(out)
	return out, nil
}

// holdsGoFile reports whether dir has a Go file other than a test: testgen seeds test files
// into a package directory before its code exists, and that is not a package yet.
func holdsGoFile(tree fs.FS, dir string) bool {
	entries, err := fs.ReadDir(tree, dir)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(entries, func(e fs.DirEntry) bool {
		n := e.Name()
		return !e.IsDir() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go")
	})
}

// --- nullgen ----------------------------------------------------------------------------------

func emitNull(in Input) ([]plan.File, error) {
	cfg := in.cfg()
	opts := nullgen.Options{ModulePath: in.ModulePath, EnvPrefix: cfg.EnvPrefix}
	if cfg.Container != defaultContainer {
		opts.Container = cfg.Container // the default is the deployment's first container, which nullgen picks
	}
	out, err := nullgen.Emit(in.Project, opts)
	if err != nil {
		return nil, err
	}
	files := make([]plan.File, 0, len(out.Files))
	for rel, content := range out.Files {
		role := plan.OutputRole(plan.Generated{})
		if rel == nullgen.HooksPath {
			role = plan.Seam{}
		}
		files = append(files, repoFile(in.repoPath(rel), role, content))
	}
	plan.Sort(files)
	return files, nil
}

// --- appgen -----------------------------------------------------------------------------------

func emitTemporal(in Input) ([]plan.File, error) {
	m, err := in.model(true)
	if err != nil {
		return nil, err
	}
	files, err := genTemporal(m, in.cfg(), in.ModulePath)
	if err != nil {
		return nil, err
	}
	return in.generated(files), nil
}

func emitConfig(in Input) ([]plan.File, error) {
	m, err := in.model(false)
	if err != nil {
		return nil, err
	}
	files, err := genConfig(m, in.cfg())
	if err != nil {
		return nil, err
	}
	return in.generated(files), nil
}

func emitCompose(in Input) ([]plan.File, error) {
	m, err := in.model(false)
	if err != nil {
		return nil, err
	}
	generated, hooks, err := genCompose(m, in, in.cfg())
	if err != nil {
		return nil, err
	}
	files := in.generated(generated)
	files = append(files, repoFile(in.repoPath(path.Join(serverDir, composegen.HooksSeedFile)), plan.Seam{}, hooks))
	plan.Sort(files)
	return files, nil
}

func emitClients(in Input) ([]plan.File, error) {
	if in.memo == nil {
		return emitClientsOnce(in)
	}
	if !in.memo.clientsReady {
		in.memo.clients, in.memo.clientsErr = emitClientsOnce(in)
		in.memo.clientsReady = true
	}
	return in.memo.clients, in.memo.clientsErr
}

func emitClientsOnce(in Input) ([]plan.File, error) {
	m, err := in.model(false)
	if err != nil {
		return nil, err
	}
	files, err := genClients(m, in.cfg(), in.ModulePath, in.Project, in.decode)
	if err != nil {
		return nil, err
	}
	return in.generated(files), nil
}

// --- testgen ----------------------------------------------------------------------------------

// uitestsDir is the Playwright output directory relative to the module.
func (in Input) uitestsDir() string {
	if in.UITestsDir != "" {
		return in.UITestsDir
	}
	if in.ModuleDir == "" || in.ModuleDir == "." {
		return "uitests/generated"
	}
	return strings.Repeat("../", len(strings.Split(path.Clean(in.ModuleDir), "/"))) + "uitests/generated"
}

// TestgenOutput is testgen's own output for in: its files keyed by module-relative path, and the
// warnings and discrimination runs the testgen command acts on. The testgen consumer's Emit is
// these files.
func TestgenOutput(in Input) (testgen.Output, error) {
	p, err := in.decode()
	if err != nil {
		return testgen.Output{}, err
	}
	return testgen.GenerateDecoded(in.Project, p, testgen.Config{ModulePath: in.ModulePath, UITestsDir: filepath.ToSlash(in.uitestsDir())})
}

func emitTests(in Input) ([]plan.File, error) {
	out, err := TestgenOutput(in)
	if err != nil {
		return nil, err
	}
	if in.Warn != nil {
		for _, w := range out.Warnings {
			if _, err := fmt.Fprintln(in.Warn, w); err != nil {
				return nil, err
			}
		}
	}
	return in.generated(out.Generated), nil
}

// --- webgen -----------------------------------------------------------------------------------

// webDir is the one web project at the repository root.
const webDir = "webApp"

// WebFile is one file of a surface's bootstrap: webgen's file (its path relative to the webApp
// directory) and who owns it once written.
type WebFile struct {
	webgen.File
	Role plan.OutputRole
}

// BootstrapSurface renders one browser surface's bootstrap with the role of each file. Every
// seat is the app's own once written (a Seam), except the mcpApp host-bridge seat
// src/<surface>/main.tsx: it connects to the MCP host and gives the app its OpsClient, no
// person writes it, and a committed edit of it is drift (earmark E30).
func BootstrapSurface(app, surface string, kind projectmodel.Surface) ([]WebFile, error) {
	files, err := webgen.Bootstrap(app, surface, kind)
	if err != nil {
		return nil, err
	}
	out := make([]WebFile, 0, len(files))
	for _, f := range files {
		role := plan.OutputRole(plan.Seam{})
		if kind == projectmodel.SurfaceMCPApp && f.Path == "src/"+surface+"/main.tsx" {
			role = plan.Generated{}
		}
		out = append(out, WebFile{File: f, Role: role})
	}
	return out, nil
}

func emitWebBootstrap(in Input) ([]plan.File, error) {
	m, err := in.load()
	if err != nil {
		return nil, err
	}
	app := webgen.AppSlug(in.ModulePath)
	seen := map[string]bool{}
	var files []plan.File
	for _, c := range m.System.BuiltBrowserSurfaces() {
		boot, err := BootstrapSurface(app, c.ID, c.Surface)
		if err != nil {
			return nil, fmt.Errorf("surface %s: %w", c.ID, err)
		}
		for _, f := range boot {
			p := path.Join(webDir, f.Path)
			if seen[p] {
				continue // the shared seats are surface-neutral: every surface writes the same bytes
			}
			seen[p] = true
			files = append(files, repoFile(p, f.Role, f.Content))
		}
	}
	plan.Sort(files)
	return files, nil
}

// emitWebGenerate renders the web project's generated transport. It runs `appgen clients` in
// memory first and feeds its OpenAPI document and MCP tool tables to webgen.Generate: the
// transport the web app binds to is the one the server emits, not whatever is on disk.
func emitWebGenerate(in Input) ([]plan.File, error) {
	m, err := in.load()
	if err != nil {
		return nil, err
	}
	surfaces := m.System.BuiltBrowserSurfaces()
	if len(surfaces) == 0 {
		return nil, nil
	}
	clients, err := emitClients(in)
	if err != nil {
		return nil, err
	}
	cfg, err := in.webConfig(surfaces[0])
	if err != nil {
		return nil, err
	}
	repo := fstest.MapFS{}
	for _, f := range clients {
		repo[f.Path] = &fstest.MapFile{Data: f.Content}
	}
	inputs, err := webgen.LoadInputsFS(repo, webDir, cfg)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil // the server's API does not exist yet, so there is no transport to bind
	}
	if err != nil {
		return nil, err
	}
	gen, err := webgen.Generate(inputs)
	if err != nil {
		return nil, err
	}
	files := make([]plan.File, 0, len(gen))
	for _, f := range gen {
		files = append(files, repoFile(path.Join(webDir, f.Path), plan.Generated{}, f.Content))
	}
	plan.Sort(files)
	return files, nil
}

// webConfig is the web project's webgen.json: the one in WebTree, else the one the bootstrap
// writes, its paths pointed at where the module emits the OpenAPI document and tool tables.
func (in Input) webConfig(first projectmodel.SystemComponent) (webgen.Config, error) {
	if in.WebTree != nil {
		raw, err := fs.ReadFile(in.WebTree, webgen.ConfigFile)
		if err == nil {
			return webgen.ParseConfig(raw)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return webgen.Config{}, err
		}
	}
	return in.bootstrapConfig(first)
}

// bootstrapConfig is the webgen.json a bootstrap writes, with its OpenAPI and tool-table paths
// pointed at where the module emits them.
func (in Input) bootstrapConfig(first projectmodel.SystemComponent) (webgen.Config, error) {
	boot, err := webgen.Bootstrap(webgen.AppSlug(in.ModulePath), first.ID, first.Surface)
	if err != nil {
		return webgen.Config{}, err
	}
	for _, f := range boot {
		if f.Path != webgen.ConfigFile {
			continue
		}
		cfg, err := webgen.ParseConfig(f.Content)
		if err != nil {
			return webgen.Config{}, err
		}
		oas := in.cfg().Clients.OAS
		if oas == "" {
			oas = "api/openapi.yaml"
		}
		cfg.OAS = relFromWeb(in.repoPath(oas))
		cfg.MCPTools = relFromWeb(in.repoPath("internal/client"))
		return cfg, nil
	}
	return webgen.Config{}, errors.New("appgen: the web bootstrap writes no " + webgen.ConfigFile)
}

// relFromWeb is a repo-relative path as seen from the webApp directory.
func relFromWeb(repoRel string) string {
	return path.Join("..", repoRel)
}
