package webgen

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// ConfigFile is the conventional name of a webApp's webgen config, beside its
// package.json.
const ConfigFile = "webgen.json"

// Config is one webApp's generator input (webgen.json). Paths are relative to
// the directory holding the config, which is the webApp root every output path
// is written under.
type Config struct {
	// App names the app in generated titles ("<app> preview fixture").
	App string
	// Surface is the slot-5 UI surface the fixtures belong to (the client id,
	// e.g. "web-client"); fixtures live at <root>/<surface>/<screen>/<state>.json.
	Surface string
	// OAS is the server's generated OpenAPI document (httpgen).
	OAS string
	// MCPTools is the directory of the server's generated MCP tool tables.
	MCPTools string
	// FixturesEnv names the environment variable that points the preview build
	// and the fixture test at another fixture root (e.g. test-local fixtures).
	FixturesEnv string
	// Composition are the hand-declared composition routes, in config order.
	Composition []CompositionRoute
}

// The name limits keep every templated TypeScript line within the app's
// prettier printWidth (100) whatever the names are, so generated and scaffolded
// files are prettier-clean by construction: prettier re-wraps a line only when
// it crosses the width, and these names are the only variable-length parts.
var (
	appRE   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	kebabRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	envRE   = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,59}$`)
)

const maxSurface = 24

// LoadConfig reads a webgen.json.
func LoadConfig(file string) (Config, error) {
	src, err := os.ReadFile(file) // #nosec G304 -- the config path is the generator's own input
	if err != nil {
		return Config{}, fmt.Errorf("webgen: read config: %w", err)
	}
	return ParseConfig(src)
}

// ParseConfig reads webgen.json bytes. Composition route result schemas keep
// the key order the file wrote them in, because they are copied into
// fixtures.schema.json verbatim.
func ParseConfig(src []byte) (Config, error) {
	doc, err := ParseYAML(src) // JSON is YAML; the YAML reader keeps key order
	if err != nil {
		return Config{}, err
	}
	root := obj(doc)
	if root == nil {
		return Config{}, fmt.Errorf("webgen: config is not an object")
	}
	c := Config{
		App:         str(root, "app"),
		Surface:     str(root, "surface"),
		OAS:         str(root, "oas"),
		MCPTools:    str(root, "mcpTools"),
		FixturesEnv: str(root, "fixturesEnv"),
	}
	routes, err := compositionRoutes(obj(path(root, "composition")))
	if err != nil {
		return Config{}, err
	}
	c.Composition = routes
	return c, c.Validate()
}

// Validate checks the fields every generator relies on.
func (c Config) Validate() error {
	switch {
	case !appRE.MatchString(c.App):
		return fmt.Errorf("webgen: config: app %q must be 1-40 lower-case letters, digits or '-'", c.App)
	case !kebabRE.MatchString(c.Surface) || len(c.Surface) > maxSurface:
		return fmt.Errorf("webgen: config: surface %q must be kebab-case, at most %d characters", c.Surface, maxSurface)
	case c.OAS == "":
		return fmt.Errorf("webgen: config: oas is required")
	case c.MCPTools == "":
		return fmt.Errorf("webgen: config: mcpTools is required")
	case !envRE.MatchString(c.FixturesEnv):
		return fmt.Errorf("webgen: config: fixturesEnv %q must be an UPPER_SNAKE environment variable of at most 60 characters", c.FixturesEnv)
	}
	return nil
}

func compositionRoutes(o *Object) ([]CompositionRoute, error) {
	if o == nil {
		return nil, nil
	}
	var out []CompositionRoute
	for _, id := range o.Keys() {
		v, _ := o.Get(id)
		r := CompositionRoute{OpID: id, Method: str(obj(v), "method"), Path: str(obj(v), "path")}
		r.Result, _ = obj(v).Get("result")
		if r.Method == "" || r.Path == "" || obj(r.Result) == nil {
			return nil, fmt.Errorf("webgen: config: composition route %q needs method, path and a result schema", id)
		}
		out = append(out, r)
	}
	return out, nil
}

func str(o *Object, key string) string {
	if o == nil {
		return ""
	}
	v, _ := o.Get(key)
	s, _ := v.(string)
	return s
}

// Inputs are the resolved generator inputs: the parsed OAS and the tool set.
type Inputs struct {
	Config Config
	OAS    Value
	Tools  map[string]bool
}

// LoadInputs reads the OAS and the tool tables a config names, resolving its
// paths against dir (the webApp root).
func LoadInputs(dir string, c Config) (Inputs, error) {
	src, err := os.ReadFile(filepath.Join(dir, c.OAS)) // #nosec G304 -- the OAS path is the generator's own input
	if err != nil {
		return Inputs{}, fmt.Errorf("webgen: read OAS: %w", err)
	}
	doc, err := ParseYAML(src)
	if err != nil {
		return Inputs{}, err
	}
	tools, err := LoadMCPTools(filepath.Join(dir, c.MCPTools))
	if err != nil {
		return Inputs{}, err
	}
	return Inputs{Config: c, OAS: doc, Tools: tools}, nil
}

// Bindings is every binding the OpsClient carries: the OAS ops and the
// composition routes, in OpId order.
func (in Inputs) Bindings() ([]Binding, error) {
	derived, err := DeriveBindings(in.OAS, in.Tools)
	if err != nil {
		return nil, err
	}
	return WithComposition(derived, in.Config.Composition)
}
