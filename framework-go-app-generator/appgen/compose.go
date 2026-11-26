package appgen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/clientgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/configgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/modelgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/rungen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/temporalgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/scenario"
)

const (
	defaultContainer = "server"
	defaultEnvPrefix = "APP"
	serverDir        = "cmd/server"
)

// Config is appgen.json: what the design cannot express. Every field is optional.
type Config struct {
	Container                  string                              `json:"container"`
	EnvPrefix                  string                              `json:"envPrefix"`
	Managers                   []string                            `json:"managers"`
	CallerKeyedOps             map[string]map[string][]string      `json:"callerKeyedOps"`
	WebExposedManagers         []string                            `json:"webExposedManagers"`
	VariantHookArgs            map[string][]composegen.HookArgType `json:"variantHookArgs"`
	VariantConstructorNoError  map[string]bool                     `json:"variantConstructorNoError"`
	ScheduleRegistrarComponent string                              `json:"scheduleRegistrarComponent"`
	WebDir                     string                              `json:"webDir"`
	Clients                    ClientsConfig                       `json:"clients"`
}

// ClientsConfig is appgen clients' knobs: see clientgen.Config.
type ClientsConfig struct {
	Title   string                       `json:"title"`
	App     string                       `json:"app"`
	Exposed map[string][]string          `json:"exposed"`
	OpDocs  map[string]map[string]string `json:"opDocs"`
	OAS     string                       `json:"oas"`
}

// DefaultConfig is the zero appgen.json's effective knobs: Container "server", EnvPrefix "APP".
func DefaultConfig() Config {
	return Config{Container: defaultContainer, EnvPrefix: defaultEnvPrefix}
}

// ParseConfig reads an appgen.json over the defaults; an unknown field is an error.
func ParseConfig(raw []byte) (Config, error) {
	cfg := DefaultConfig()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// cfg is the config with its defaults filled in.
func (in Input) cfg() Config {
	c := in.Config
	if c.Container == "" {
		c.Container = defaultContainer
	}
	if c.EnvPrefix == "" {
		c.EnvPrefix = defaultEnvPrefix
	}
	return c
}

// model loads the project and keeps the contracts that are built in the tree (every one with a
// goPackage when the tree is nil).
func (in Input) model(managerContracts bool) (*projectmodel.Model, error) {
	m, err := in.load()
	if err != nil {
		return nil, err
	}
	wireOnlyBuilt(m, in.Tree, managerContracts)
	return m, nil
}

// wireOnlyBuilt drops every contract whose package holds no code yet. A generated main that
// names an unbuilt package fails to compile, which would fail every other component's checks
// along with it. With managerContracts, a Manager whose package holds only its generated
// contract stays: its Temporal layer is what the hand-written Manager is built against, so it
// must exist before the first line of the Manager does. A Client whose package holds generated
// files stays too: REST and MCP machine clients are generated end to end, so that is their
// built state. A nil tree is the emission of a fully built module: nothing is dropped.
func wireOnlyBuilt(m *projectmodel.Model, tree fs.FS, managerContracts bool) {
	if tree == nil {
		return
	}
	for key, c := range m.Contracts {
		if c.GoPackage == "" {
			continue
		}
		if !isBuilt(c, tree, c.GoPackage, managerContracts) {
			delete(m.Contracts, key)
		}
	}
}

// isBuilt reports that contract c's package, at dir, is built. A REST or MCP machine client's
// package is generated end to end, so it never holds hand-written code: its generated files are
// the package being built.
func isBuilt(c *projectmodel.Contract, tree fs.FS, dir string, managerContracts bool) bool {
	switch {
	case holdsCode(tree, dir):
		return true
	case managerContracts && strings.EqualFold(c.Layer, "manager"):
		return hasGeneratedContract(tree, dir)
	case strings.EqualFold(c.Layer, "client"):
		return holdsGenerated(tree, dir)
	default:
		return false
	}
}

// holdsGenerated reports that dir holds at least one generated Go file.
func holdsGenerated(tree fs.FS, dir string) bool {
	entries, err := fs.ReadDir(tree, dir)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(entries, func(e fs.DirEntry) bool {
		return !e.IsDir() && strings.HasSuffix(e.Name(), ".gen.go")
	})
}

func hasGeneratedContract(tree fs.FS, dir string) bool {
	_, err := fs.Stat(tree, path.Join(dir, "contract.gen.go"))
	return err == nil
}

func holdsCode(tree fs.FS, dir string) bool {
	entries, err := fs.ReadDir(tree, dir)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(entries, func(e fs.DirEntry) bool {
		n := e.Name()
		if n == modelgen.NullFile {
			// nullgen's null implementation is the package's code in a model with nothing
			// built: the conformance pipeline wires and boots it like a hand-written one.
			return !e.IsDir()
		}
		return !e.IsDir() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") && !strings.HasSuffix(n, ".gen.go")
	})
}

// managersOf is the Manager contracts to wire: the configured set, else every built one.
func managersOf(m *projectmodel.Model, cfg Config) []string {
	if cfg.Managers != nil {
		return cfg.Managers
	}
	var out []string
	for key, c := range m.Contracts {
		if strings.EqualFold(c.Layer, "manager") && c.GoPackage != "" {
			out = append(out, key)
		}
	}
	slices.Sort(out)
	return out
}

func genTemporal(m *projectmodel.Model, cfg Config, module string) (map[string][]byte, error) {
	files := map[string][]byte{}
	for _, key := range managersOf(m, cfg) {
		c, ok := m.Contracts[key]
		if !ok {
			continue
		}
		generated, err := temporalgen.Generate(m, temporalgen.Config{ModulePath: module, ManagerKey: key, CallerKeyedOps: cfg.CallerKeyedOps[key]})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		for name, content := range generated {
			files[path.Join(filepath.ToSlash(c.GoPackage), name)] = content
		}
	}
	return files, nil
}

func withDeployment(m *projectmodel.Model, cfg Config) *projectmodel.Model {
	cp := *m
	cp.Deployment = m.EffectiveDeployment(cfg.Container)
	return &cp
}

func genConfig(m *projectmodel.Model, cfg Config) (map[string][]byte, error) {
	generated, err := configgen.Generate(withDeployment(m, cfg), configgen.Config{ContainerKey: containerOf(m, cfg), EnvPrefix: cfg.EnvPrefix, PackageName: "main"})
	if err != nil {
		return nil, fmt.Errorf("configgen: %w", err)
	}
	return prefixed(generated), nil
}

// genCompose is the composition root's generated files, and the once-seeded hooks.go beside them.
func genCompose(m *projectmodel.Model, in Input, cfg Config) (generated map[string][]byte, hooks []byte, err error) {
	effective, cc := composeInputs(m, in, cfg)
	files, err := composegen.Generate(effective, cc)
	if err != nil {
		return nil, nil, fmt.Errorf("composegen: %w", err)
	}
	hooks, err = composegen.HooksSeed(effective, cc)
	if err != nil {
		return nil, nil, fmt.Errorf("composegen: seed %s: %w", composegen.HooksSeedFile, err)
	}
	if err := refuseUndeclaredEnv(effective, cfg, in.Tree); err != nil {
		return nil, nil, err
	}
	launcher, err := rungen.Generate(effective, rungen.Config{ContainerKey: cc.ContainerKey, EnvPrefix: cfg.EnvPrefix, WebDir: cfg.WebDir})
	if err != nil {
		return nil, nil, err
	}
	out := prefixed(files)
	for name, content := range launcher {
		out[path.Join(rungen.Dir, name)] = content
	}
	return out, hooks, nil
}

// composeInputs is the deployment-effective model and the composegen config both the composition
// root and the generated test subject are built from.
func composeInputs(m *projectmodel.Model, in Input, cfg Config) (*projectmodel.Model, composegen.Config) {
	effective := withDeployment(m, cfg)
	return effective, composeConfig(m, effective, cfg, in.ModulePath)
}

// genScenarioCompose is the generated test subject, keyed by its module-relative path. It is
// not under cmd/server: scenario tests of every component import it, so it belongs with the
// generated tests and not with the composition root. It is emitted even for an app with no
// test plans, because Workflows() feeds the replay gate.
func genScenarioCompose(m *projectmodel.Model, in Input, cfg Config) (map[string][]byte, error) {
	effective, cc := composeInputs(m, in, cfg)
	subject, refused, err := composegen.EmitScenarioCompose(effective, cc)
	if err != nil {
		return nil, err
	}
	reportRefusals(in.Warn, refused)
	return map[string][]byte{composegen.ScenarioComposeFile: subject}, nil
}

// reportRefusals tells the operator which Managers the generated test subject leaves unbuilt
// (their constructor takes composition-root policy the model cannot express). Everything else
// is generated; each refusal is named so none is silently dropped.
func reportRefusals(w io.Writer, refused []composegen.Refusal) {
	if w == nil {
		return
	}
	for _, r := range refused {
		_, _ = fmt.Fprintf(w, "appgen: scenariocompose: manager left unbuilt: %s\n", r) // a report; its write failing changes nothing
	}
}

// handServerSources is the composition root's hand-written Go files (no tests, nothing
// generated), keyed by their module-root-relative path.
func handServerSources(tree fs.FS) (map[string][]byte, error) {
	if tree == nil {
		return nil, nil
	}
	entries, err := fs.ReadDir(tree, serverDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	srcs := map[string][]byte{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") || strings.HasSuffix(n, ".gen.go") {
			continue
		}
		src, err := fs.ReadFile(tree, path.Join(serverDir, n))
		if err != nil {
			return nil, err
		}
		srcs[path.Join(serverDir, n)] = src
	}
	return srcs, nil
}

// refuseUndeclaredEnv is the gate on the composition root's hand half (FIX-049): a hand file
// in cmd/server may read only the <PREFIX>_* variables the generated Config declares. Any
// other is configuration no launcher, operator or gate can see; it belongs in the deployment
// model, read through the Config.
func refuseUndeclaredEnv(m *projectmodel.Model, cfg Config, tree fs.FS) error {
	inputs, err := configgen.Inputs(m, configgen.Config{ContainerKey: containerOf(m, cfg), EnvPrefix: cfg.EnvPrefix, PackageName: "main"})
	if err != nil {
		return fmt.Errorf("configgen: %w", err)
	}
	srcs, err := handServerSources(tree)
	if err != nil {
		return err
	}
	bad, err := configgen.UndeclaredEnvReads(inputs, cfg.EnvPrefix, srcs)
	if err != nil {
		return err
	}
	if len(bad) > 0 {
		return fmt.Errorf("appgen: the composition root reads environment variables the deployment does not declare (declare each in the deployment model and read it from the generated Config):\n  %s", strings.Join(bad, "\n  "))
	}
	return nil
}

func composeConfig(m, effective *projectmodel.Model, cfg Config, module string) composegen.Config {
	hooks := maps.Clone(cfg.VariantHookArgs)
	if hooks == nil {
		hooks = map[string][]composegen.HookArgType{}
	}
	// ExternalHTTP is the one approved binding no provisioned substrate supplies: its URL is a
	// declared external-http input (FIX-049) the app's hook reads from the generated Config,
	// its client is the hook's own, and modelgen's constructor for it returns an error.
	externalArgs, returnsError := composegen.ExternalHTTPArgs(effective)
	for key, args := range externalArgs {
		if _, set := hooks[key]; !set {
			hooks[key] = args
		}
	}
	return composegen.Config{
		ContainerKey: containerOf(m, cfg), ModulePath: module, PackageName: "main", EnvPrefix: cfg.EnvPrefix,
		WebExposedManagers: cfg.WebExposedManagers, ExposedOps: cfg.Clients.Exposed, VariantHookArgs: hooks,
		VariantConstructorNoError: cfg.VariantConstructorNoError, VariantConstructorReturnsError: returnsError,
		ScheduleRegistrarComponent: cfg.ScheduleRegistrarComponent,
		App:                        clientgen.AppName(clientgen.Config{ModulePath: module, App: cfg.Clients.App}),
	}
}

// genClients generates the client layer of the Managers that hold code: wireOnlyBuilt has
// already dropped the rest from m.
func genClients(m *projectmodel.Model, cfg Config, module string, raw []byte, decode func() (methodcheck.Project, error)) (map[string][]byte, error) {
	var doc struct {
		ServiceContracts map[string]json.RawMessage `json:"serviceContracts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("project.json: %w", err)
	}
	built := make(map[string]json.RawMessage, len(m.Contracts))
	for key := range m.Contracts {
		built[key] = doc.ServiceContracts[key]
	}
	apps, err := mcpApps(m, decode, built)
	if err != nil {
		return nil, fmt.Errorf("project.json: %w", err)
	}
	return clientgen.Generate(clientgen.Project{Contracts: built, System: m.System, MCPApps: apps}, clientgen.Config{
		ModulePath: module, Title: cfg.Clients.Title, App: cfg.Clients.App,
		Exposed: cfg.Clients.Exposed, OpDocs: cfg.Clients.OpDocs, OAS: cfg.Clients.OAS,
	})
}

// mcpApps is what each built mcpApp Client reaches, read off the project's dynamic views: the
// Manager operations (by the built contracts' keys) the views call through the Client.
func mcpApps(m *projectmodel.Model, decode func() (methodcheck.Project, error), built map[string]json.RawMessage) ([]clientgen.MCPApp, error) {
	ids := mcpAppClients(m.System)
	if len(ids) == 0 {
		return nil, nil
	}
	p, err := decode()
	if err != nil {
		return nil, err
	}
	in, err := methodcheck.ScenarioInput(p)
	if err != nil {
		return nil, fmt.Errorf("derive what the mcpApp Clients reach: %w", err)
	}
	apps := make([]clientgen.MCPApp, 0, len(ids))
	for _, id := range ids {
		reached := map[string][]string{}
		for key, ops := range scenario.ManagerOpsThrough(in, id) {
			if _, ok := built[key]; ok {
				reached[key] = managerOpNames(ops)
			}
		}
		apps = append(apps, clientgen.MCPApp{Component: id, Ops: reached})
	}
	return apps, nil
}

// managerOpNames is the sorted Manager operation names of ops (the Manager's own, not the client's).
func managerOpNames(ops []scenario.ManagerOp) []string {
	names := make([]string, 0, len(ops))
	for _, op := range ops {
		names = append(names, op.Op)
	}
	slices.Sort(names)
	return names
}

// mcpAppClients is the ids of the System's built mcpApp Clients.
func mcpAppClients(s *projectmodel.System) []string {
	if s == nil {
		return nil
	}
	var ids []string
	for _, c := range s.Components {
		kind, ok := c.SurfaceKind()
		if !ok || !c.IsBuilt() {
			continue
		}
		switch kind.(type) {
		case projectmodel.MCPAppSurface:
			ids = append(ids, c.ID)
		case projectmodel.WebSurface, projectmodel.RESTSurface, projectmodel.MCPToolsSurface:
		}
	}
	return ids
}

func containerOf(m *projectmodel.Model, cfg Config) string {
	if m.Deployment != nil && len(m.Deployment.Containers) > 0 && cfg.Container == defaultContainer {
		return m.Deployment.Containers[0].Key
	}
	return cfg.Container
}

func prefixed(generated map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(generated))
	for name, content := range generated {
		out[path.Join(serverDir, name)] = content
	}
	return out
}
