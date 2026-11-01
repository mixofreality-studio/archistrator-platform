// Command appgen generates an app's composition layer from project.json, reading the
// module path from go.mod. Run it from the app's Go module with `go tool appgen`:
//
//	go tool appgen temporal [-check]   per-Manager activities, invokers and worker
//	go tool appgen config   [-check]   cmd/server/config.gen.go
//	go tool appgen compose  [-check]   cmd/server/main.gen.go, and hooks.go if absent
//	go tool appgen clients  [-check]   internal/client/** and api/openapi.yaml
//
// A contract is wired only once its package holds code, so the generated composition
// root always builds at whatever point construction has reached. The knobs an app may
// need live in a birth-seated appgen.json; a fresh app's is empty and every knob has a
// default derived from the contracts and the design.
//
// Flags: -project (default .aiarch/state/project.json), -root (default .), -config (default
// <root>/appgen.json, optional).
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/clientgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/composegen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/configgen"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/temporalgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/arch"
)

const (
	defaultContainer = "server"
	defaultEnvPrefix = "APP"
	serverDir        = "cmd/server"
)

// errDrift is the -check verdict when anything drifted.
var errDrift = errors.New("appgen: generated files are out of date; run appgen")

// config is appgen.json: what the design cannot express. Every field is optional.
type config struct {
	Container                  string                              `json:"container"`
	EnvPrefix                  string                              `json:"envPrefix"`
	Managers                   []string                            `json:"managers"`
	CallerKeyedOps             map[string]map[string][]string      `json:"callerKeyedOps"`
	WebExposedManagers         []string                            `json:"webExposedManagers"`
	VariantHookArgs            map[string][]composegen.HookArgType `json:"variantHookArgs"`
	VariantConstructorNoError  map[string]bool                     `json:"variantConstructorNoError"`
	ScheduleRegistrarComponent string                              `json:"scheduleRegistrarComponent"`
	Clients                    clientsConfig                       `json:"clients"`
}

// clientsConfig is appgen clients' knobs: see clientgen.Config.
type clientsConfig struct {
	Title   string                       `json:"title"`
	App     string                       `json:"app"`
	Exposed map[string][]string          `json:"exposed"`
	OpDocs  map[string]map[string]string `json:"opDocs"`
}

type options struct {
	project, root, configPath string
	check                     bool
}

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: appgen temporal|config|compose|clients [-check]")
	}
	sub, args := args[0], args[1:]
	var o options
	fl := flag.NewFlagSet("appgen "+sub, flag.ContinueOnError)
	fl.SetOutput(out)
	fl.StringVar(&o.project, "project", ".aiarch/state/project.json", "the project.json to read")
	fl.StringVar(&o.root, "root", ".", "the Go module to generate into")
	fl.StringVar(&o.configPath, "config", "", "the knobs file (default <root>/appgen.json)")
	fl.BoolVar(&o.check, "check", false, "report drift and exit 1 instead of writing")
	if err := fl.Parse(args); err != nil {
		return err
	}
	module, err := arch.ModulePathOf(o.root)
	if err != nil {
		return err
	}
	cfg, err := readConfig(o)
	if err != nil {
		return err
	}
	m, err := projectmodel.LoadFile(o.project)
	if err != nil {
		return err
	}
	wireOnlyBuilt(m, o.root)
	var files map[string][]byte
	switch sub {
	case "temporal":
		files, err = genTemporal(m, cfg, module)
	case "config":
		files, err = genConfig(m, cfg)
	case "compose":
		files, err = genCompose(m, cfg, module, o)
	case "clients":
		files, err = genClients(m, cfg, module, o.project)
	default:
		return fmt.Errorf("appgen: unknown subcommand %q", sub)
	}
	if err != nil {
		return err
	}
	return emit(o, files, out)
}

func readConfig(o options) (config, error) {
	cfg := config{Container: defaultContainer, EnvPrefix: defaultEnvPrefix}
	path := o.configPath
	if path == "" {
		path = filepath.Join(o.root, "appgen.json")
	}
	raw, err := os.ReadFile(path) // #nosec G304 G703 -- the CLI's own argument; no trust boundary
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// wireOnlyBuilt drops every contract whose package holds no code yet. A generated main
// that names an unbuilt package fails to compile, which would fail every other component's
// checks along with it.
func wireOnlyBuilt(m *projectmodel.Model, root string) {
	for key, c := range m.Contracts {
		if c.GoPackage == "" {
			continue
		}
		if !holdsCode(filepath.Join(root, c.GoPackage)) {
			delete(m.Contracts, key)
		}
	}
}

func holdsCode(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(entries, func(e os.DirEntry) bool {
		n := e.Name()
		return !e.IsDir() && strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") && n != "contract.gen.go"
	})
}

// managersOf is the Manager contracts to wire: the configured set, else every built one.
func managersOf(m *projectmodel.Model, cfg config) []string {
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

func genTemporal(m *projectmodel.Model, cfg config, module string) (map[string][]byte, error) {
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
			files[filepath.Join(c.GoPackage, name)] = content
		}
	}
	return files, nil
}

func withDeployment(m *projectmodel.Model, cfg config) *projectmodel.Model {
	cp := *m
	cp.Deployment = m.EffectiveDeployment(cfg.Container)
	return &cp
}

func genConfig(m *projectmodel.Model, cfg config) (map[string][]byte, error) {
	generated, err := configgen.Generate(withDeployment(m, cfg), configgen.Config{ContainerKey: containerOf(m, cfg), EnvPrefix: cfg.EnvPrefix, PackageName: "main"})
	if err != nil {
		return nil, fmt.Errorf("configgen: %w", err)
	}
	return prefixed(generated), nil
}

func genCompose(m *projectmodel.Model, cfg config, module string, o options) (map[string][]byte, error) {
	effective := withDeployment(m, cfg)
	cc := composeConfig(m, effective, cfg, module)
	generated, err := composegen.Generate(effective, cc)
	if err != nil {
		return nil, fmt.Errorf("composegen: %w", err)
	}
	if !o.check {
		if err := seedHooks(effective, cc, o.root); err != nil {
			return nil, err
		}
	}
	return prefixed(generated), nil
}

func composeConfig(m, effective *projectmodel.Model, cfg config, module string) composegen.Config {
	hooks := maps.Clone(cfg.VariantHookArgs)
	if hooks == nil {
		hooks = map[string][]composegen.HookArgType{}
	}
	// ExternalHTTP is the one approved binding no substrate supplies: its URL and client
	// come from the app's hooks.
	for _, b := range effective.Deployment.Bindings {
		for _, v := range b.PerProfile {
			key := b.Component + "/" + v.Variant
			if _, set := hooks[key]; v.Variant == "ExternalHTTP" && !set {
				hooks[key] = []composegen.HookArgType{{GoType: "string"}, {GoType: "*http.Client", GoImport: "net/http"}}
			}
		}
	}
	return composegen.Config{
		ContainerKey: containerOf(m, cfg), ModulePath: module, PackageName: "main", EnvPrefix: cfg.EnvPrefix,
		WebExposedManagers: cfg.WebExposedManagers, VariantHookArgs: hooks,
		VariantConstructorNoError: cfg.VariantConstructorNoError, ScheduleRegistrarComponent: cfg.ScheduleRegistrarComponent,
	}
}

// seedHooks writes the once-seeded hooks.go if the app has none: it is the app's own afterwards.
func seedHooks(m *projectmodel.Model, cc composegen.Config, root string) error {
	dest := filepath.Join(root, serverDir, composegen.HooksSeedFile)
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		return err
	}
	seed, err := composegen.HooksSeed(m, cc)
	if err != nil {
		return fmt.Errorf("composegen: seed %s: %w", composegen.HooksSeedFile, err)
	}
	return writeGenerated(dest, seed)
}

// genClients generates the client layer of the Managers that hold code: wireOnlyBuilt has
// already dropped the rest from m.
func genClients(m *projectmodel.Model, cfg config, module, project string) (map[string][]byte, error) {
	raw, err := os.ReadFile(project) // #nosec G304 G703 -- the CLI's own argument; no trust boundary
	if err != nil {
		return nil, err
	}
	var doc struct {
		ServiceContracts map[string]json.RawMessage `json:"serviceContracts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", project, err)
	}
	built := make(map[string]json.RawMessage, len(m.Contracts))
	for key := range m.Contracts {
		built[key] = doc.ServiceContracts[key]
	}
	return clientgen.Generate(clientgen.Project{Contracts: built, System: m.System}, clientgen.Config{
		ModulePath: module, Title: cfg.Clients.Title, App: cfg.Clients.App,
		Exposed: cfg.Clients.Exposed, OpDocs: cfg.Clients.OpDocs,
	})
}

func containerOf(m *projectmodel.Model, cfg config) string {
	if m.Deployment != nil && len(m.Deployment.Containers) > 0 && cfg.Container == defaultContainer {
		return m.Deployment.Containers[0].Key
	}
	return cfg.Container
}

func prefixed(generated map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(generated))
	for name, content := range generated {
		out[filepath.Join(serverDir, name)] = content
	}
	return out
}

func emit(o options, files map[string][]byte, out io.Writer) error {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	if o.check {
		return checkDrift(o.root, paths, files, out)
	}
	for _, p := range paths {
		if err := writeGenerated(filepath.Join(o.root, p), files[p]); err != nil {
			return err
		}
	}
	return nil
}

// checkDrift reports every generated file that is missing or differs from what is on disk.
func checkDrift(root string, paths []string, files map[string][]byte, out io.Writer) error {
	drifted := false
	for _, p := range paths {
		have, err := os.ReadFile(filepath.Join(root, p)) // #nosec G304 G703 -- derived from the repo's own project.json
		if err != nil || !bytes.Equal(have, files[p]) {
			drifted = true
			_, _ = fmt.Fprintf(out, "drift: %s\n", p)
		}
	}
	if drifted {
		return errDrift
	}
	return nil
}

func writeGenerated(dest string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil { // #nosec G301 G703 -- derived from the repo's own project.json
		return err
	}
	return os.WriteFile(dest, content, 0o600) // #nosec G306 G703 -- derived from the repo's own project.json
}
