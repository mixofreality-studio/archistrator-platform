// Package clientgen generates an app's whole Go client layer from its contracts and System:
//
//	internal/client/web/{middleware,server}.gen.go   the auth middleware and server wiring
//	internal/client/web/<pkg>/<base>_handlers.gen.go one REST handler set per exposed Manager
//	internal/client/<leaf>/<pkg>/<base>_tools.gen.go one MCP tool table per Manager a machine
//	                                                 client fronts (uiSurface false)
//	api/openapi.yaml                                 every exposed Manager's REST surface, merged
//
// Browser surfaces (uiSurface true) get no Go of their own: they all reach the shared REST
// transport. <leaf> is the machine client's name without its stereotype, the same rule the
// alignment gate matches by, so the package is found without a join: MCPClient lands in
// internal/client/mcp and MachineApi in internal/client/machineapi.
//
// A Manager is exposed when a Client has a relationship to it, unless Config.Exposed says
// otherwise. Output is deterministic and only ever names Managers the caller passes, so an
// unbuilt Manager produces nothing that fails to compile.
package clientgen

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/clientgen/mcpemit"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/httpgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/methodcheck"
)

// Output paths, module-root-relative.
const (
	clientRoot = "internal/client"
	webRoot    = clientRoot + "/web"
	oasPath    = "api/openapi.yaml"
)

// Config is what the design cannot express. Every field is optional.
type Config struct {
	// ModulePath is the app's Go module path. Required.
	ModulePath string
	// Title is the OpenAPI document's title (default: the module's last segment + " API").
	Title string
	// App names the app in the MCP Apps shell resource URI (default: the module's last
	// segment other than a trailing "server").
	App string
	// Exposed, when non-nil, replaces the relationship-derived set: Manager contract key →
	// the operations that reach the client layer, nil meaning the whole interface. Every
	// machine client fronts all of them.
	Exposed map[string][]string
	// OpDocs is the MCP tool description of each operation, by Manager interface and then
	// operation, for a Manager whose contract does not carry its own prose. A Manager listed
	// here must document every operation.
	OpDocs map[string]map[string]string
}

// Project is the input: the raw contract entries, which generation keeps verbatim, and the
// System they belong to.
type Project struct {
	Contracts map[string]json.RawMessage
	System    *projectmodel.System
}

// Generate returns every generated file, keyed by module-root-relative path.
func Generate(p Project, cfg Config) (map[string][]byte, error) {
	if cfg.ModulePath == "" {
		return nil, errors.New("clientgen: ModulePath is required")
	}
	exposed := exposedManagers(p, cfg)
	if len(exposed) == 0 {
		return map[string][]byte{}, nil
	}
	files := map[string][]byte{}
	keys := slices.Sorted(maps.Keys(exposed))
	docs := make([]contractOAS, 0, len(keys))
	for _, key := range keys {
		g, err := newContractGen(p, cfg, key, exposed[key])
		if err != nil {
			return nil, fmt.Errorf("clientgen: contract %q: %w", key, err)
		}
		doc, err := g.rest(files)
		if err != nil {
			return nil, fmt.Errorf("clientgen: contract %q: %w", key, err)
		}
		docs = append(docs, doc)
		for _, c := range machineClients(p, cfg, key) {
			if err := g.mcpTools(files, c); err != nil {
				return nil, fmt.Errorf("clientgen: contract %q: %w", key, err)
			}
		}
	}
	if err := wiring(files); err != nil {
		return nil, err
	}
	merged, err := MergeOpenAPI(docs, titleOf(cfg))
	if err != nil {
		return nil, err
	}
	out, err := yaml.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("clientgen: marshal the merged OpenAPI: %w", err)
	}
	files[oasPath] = out
	return files, nil
}

func wiring(files map[string][]byte) error {
	w, err := httpgen.GenerateWiring(httpgen.WiringOptions{Package: "web"})
	if err != nil {
		return fmt.Errorf("clientgen: httpgen wiring: %w", err)
	}
	files[webRoot+"/middleware.gen.go"] = w.MiddlewareGo
	files[webRoot+"/server.gen.go"] = w.ServerGo
	return nil
}

func moduleLeaf(module string) string {
	segs := strings.Split(module, "/")
	leaf := segs[len(segs)-1]
	if leaf == "server" && len(segs) > 1 {
		return segs[len(segs)-2]
	}
	return leaf
}

func titleOf(cfg Config) string {
	if cfg.Title != "" {
		return cfg.Title
	}
	return moduleLeaf(cfg.ModulePath) + " API"
}

// exposedManagers is Config.Exposed, or every passed Manager contract some built Client has a
// relationship to.
func exposedManagers(p Project, cfg Config) map[string][]string {
	if cfg.Exposed != nil {
		out := make(map[string][]string, len(cfg.Exposed))
		for key, ops := range cfg.Exposed {
			if _, ok := p.Contracts[key]; ok {
				out[key] = ops
			}
		}
		return out
	}
	out := map[string][]string{}
	for _, c := range builtClients(p) {
		for _, key := range managersOf(p, c.ID) {
			out[key] = nil
		}
	}
	return out
}

func builtClients(p Project) []projectmodel.SystemComponent {
	if p.System == nil {
		return nil
	}
	var out []projectmodel.SystemComponent
	for _, c := range p.System.Components {
		if strings.EqualFold(c.Kind, "client") && c.BuildStatus != "planned" && c.BuildStatus != "external" {
			out = append(out, c)
		}
	}
	return out
}

// managersOf is the contract keys of the Managers a Client has a relationship to, among the
// contracts passed in.
func managersOf(p Project, clientID string) []string {
	byID := make(map[string]projectmodel.SystemComponent, len(p.System.Components))
	for _, c := range p.System.Components {
		byID[c.ID] = c
	}
	var out []string
	for _, r := range p.System.Relationships {
		to, ok := byID[r.To]
		if r.From != clientID || !ok || !strings.EqualFold(to.Kind, "manager") {
			continue
		}
		key := to.ContractKey
		if key == "" {
			key = projectmodel.LowerFirst(to.Name)
		}
		if _, built := p.Contracts[key]; built {
			out = append(out, key)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// machineClients is the machine Clients that front the Manager key.
func machineClients(p Project, cfg Config, key string) []projectmodel.SystemComponent {
	var out []projectmodel.SystemComponent
	for _, c := range builtClients(p) {
		if c.IsBrowserSurface() {
			continue
		}
		if cfg.Exposed != nil || slices.Contains(managersOf(p, c.ID), key) {
			out = append(out, c)
		}
	}
	return out
}

// contractGen generates one Manager's client surface.
type contractGen struct {
	entry         json.RawMessage
	doc           *projectmodel.Doc
	pkg, base     string
	managerImport string
	opDoc         func(string) string
	app           string
}

func newContractGen(p Project, cfg Config, key string, ops []string) (*contractGen, error) {
	entry, err := ExposedContract(p.Contracts[key], ops)
	if err != nil {
		return nil, err
	}
	var meta struct {
		GoPackage string `json:"goPackage"`
	}
	if err := json.Unmarshal(entry, &meta); err != nil {
		return nil, fmt.Errorf("parse metadata: %w", err)
	}
	if meta.GoPackage == "" {
		return nil, errors.New("it has no goPackage")
	}
	doc, err := projectmodel.Parse(entry)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	opDoc, err := opDocs(entry, doc, cfg)
	if err != nil {
		return nil, err
	}
	app := cfg.App
	if app == "" {
		app = moduleLeaf(cfg.ModulePath)
	}
	return &contractGen{
		entry: entry, doc: doc, pkg: path.Base(meta.GoPackage),
		base:          projectmodel.Kebab(doc.ManagerBase()),
		managerImport: cfg.ModulePath + "/" + meta.GoPackage,
		opDoc:         opDoc,
		app:           app,
	}, nil
}

// rest writes the Manager's REST handlers and returns its OpenAPI document.
func (g *contractGen) rest(files map[string][]byte) (contractOAS, error) {
	res, err := httpgen.Generate(g.doc, httpgen.Options{Package: g.pkg, ManagerImport: g.managerImport})
	if err != nil {
		return contractOAS{}, fmt.Errorf("httpgen: %w", err)
	}
	files[path.Join(webRoot, g.pkg, g.base+"_handlers.gen.go")] = res.HandlersGo
	var oas map[string]any
	if err := yaml.Unmarshal(res.OpenAPIYAML, &oas); err != nil {
		return contractOAS{}, fmt.Errorf("decode the generated OpenAPI: %w", err)
	}
	return contractOAS{prefix: g.doc.ManagerBase(), doc: oas}, nil
}

// mcpTools writes the MCP tool table this Manager gets on one machine client.
func (g *contractGen) mcpTools(files map[string][]byte, c projectmodel.SystemComponent) error {
	res, err := mcpemit.Generate(g.entry, mcpemit.Options{
		Package: g.pkg, ManagerImport: g.managerImport, OpDoc: g.opDoc, App: g.app,
	})
	if err != nil {
		return fmt.Errorf("mcpemit: %w", err)
	}
	files[path.Join(ClientPackage(c.Name), g.pkg, g.base+"_tools.gen.go")] = res.ToolsGo
	return nil
}

// ClientPackage is a machine client's package root, module-root-relative.
func ClientPackage(clientName string) string {
	return path.Join(clientRoot, methodcheck.StereotypeSuffixNormalizer(clientName))
}

// opDocs resolves each operation's tool description: the knob for this Manager, else the
// contract's own description, else a sentence made from the operation's name. A Manager the
// knob lists has opted in to hand-written prose, so a gap there stays mcpemit's error.
func opDocs(entry json.RawMessage, doc *projectmodel.Doc, cfg Config) (func(string) string, error) {
	if table, ok := cfg.OpDocs[doc.Interface.Name]; ok {
		return func(op string) string { return table[op] }, nil
	}
	var meta struct {
		Interface struct {
			Operations []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"operations"`
		} `json:"interface"`
	}
	if err := json.Unmarshal(entry, &meta); err != nil {
		return nil, fmt.Errorf("parse the operation descriptions: %w", err)
	}
	own := map[string]string{}
	for _, op := range meta.Interface.Operations {
		own[op.Name] = op.Description
	}
	iface := doc.Interface.Name
	return func(op string) string {
		if d := own[op]; d != "" {
			return d
		}
		return wordsOf(op) + " on the " + iface + "."
	}, nil
}

var wordBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

// wordsOf spells a Go method name as words: RecordNewTodo → "Record new todo".
func wordsOf(op string) string {
	s := wordBoundary.ReplaceAllString(op, "$1 $2")
	if s == "" {
		return s
	}
	return s[:1] + strings.ToLower(s[1:])
}

// ExposedContract is entry with its interface.operations narrowed to ops, in contract order,
// and its $defs narrowed to the types those ops reach; every other member is kept verbatim.
// nil ops returns entry unchanged. An op the contract does not declare is an error, so a
// rename can never silently shrink the client surface.
func ExposedContract(entry json.RawMessage, ops []string) (json.RawMessage, error) {
	if ops == nil {
		return entry, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(entry, &doc); err != nil {
		return nil, fmt.Errorf("parse contract: %w", err)
	}
	var iface map[string]json.RawMessage
	if err := json.Unmarshal(doc["interface"], &iface); err != nil {
		return nil, fmt.Errorf("parse interface: %w", err)
	}
	var all []json.RawMessage
	if err := json.Unmarshal(iface["operations"], &all); err != nil {
		return nil, fmt.Errorf("parse interface.operations: %w", err)
	}
	want := map[string]bool{}
	for _, op := range ops {
		want[op] = true
	}
	kept := make([]json.RawMessage, 0, len(ops))
	for _, raw := range all {
		var op struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &op); err != nil {
			return nil, fmt.Errorf("parse operation: %w", err)
		}
		if want[op.Name] {
			kept = append(kept, raw)
			delete(want, op.Name)
		}
	}
	if len(want) > 0 {
		return nil, fmt.Errorf("exposed ops not on the interface: %s", strings.Join(slices.Sorted(maps.Keys(want)), ", "))
	}
	var err error
	if iface["operations"], err = json.Marshal(kept); err != nil {
		return nil, err
	}
	if doc["interface"], err = json.Marshal(iface); err != nil {
		return nil, err
	}
	if raw, ok := doc["$defs"]; ok {
		if doc["$defs"], err = reachedDefs(raw, kept); err != nil {
			return nil, err
		}
	}
	return json.Marshal(doc)
}

// defRef matches a local "$defs" reference and captures the type name.
var defRef = regexp.MustCompile(`"#/\$defs/([^"]+)"`)

// reachedDefs keeps the $defs entries the ops reference, directly or through other kept
// entries.
func reachedDefs(raw json.RawMessage, ops []json.RawMessage) (json.RawMessage, error) {
	var defs map[string]json.RawMessage
	if err := json.Unmarshal(raw, &defs); err != nil {
		return nil, fmt.Errorf("parse $defs: %w", err)
	}
	kept := map[string]json.RawMessage{}
	var queue []string
	visit := func(b []byte) {
		for _, m := range defRef.FindAllSubmatch(b, -1) {
			name := string(m[1])
			if _, seen := kept[name]; seen {
				continue
			}
			if def, ok := defs[name]; ok {
				kept[name] = def
				queue = append(queue, name)
			}
		}
	}
	for _, op := range ops {
		visit(op)
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		visit(defs[name])
	}
	return json.Marshal(kept)
}

// contractOAS pairs one contract's OpenAPI document with the Manager base that namespaces its
// schemas in the merged document.
type contractOAS struct {
	prefix string
	doc    map[string]any
}

// MergeOpenAPI unions the paths and schemas of every contract's OpenAPI 3.1 document.
//
// Contracts are independent, self-contained documents, and several may define a same-named
// type with a different shape. components.schemas is one flat namespace, so a plain union
// would silently collapse them. Each contract's schema graph references nothing outside
// itself, so every schema is prefixed with its Manager's base and its $refs rewritten.
func MergeOpenAPI(docs []contractOAS, title string) (map[string]any, error) {
	paths := map[string]any{}
	schemas := map[string]any{}
	for _, c := range docs {
		prefixRefs(c.doc, c.prefix)
		if p, ok := c.doc["paths"].(map[string]any); ok {
			for route, ops := range p {
				if _, dup := paths[route]; dup {
					return nil, fmt.Errorf("clientgen: duplicate route %q across contracts", route)
				}
				paths[route] = ops
			}
		}
		comps, ok := c.doc["components"].(map[string]any)
		if !ok {
			continue
		}
		sch, ok := comps["schemas"].(map[string]any)
		if !ok {
			continue
		}
		for name, def := range sch {
			if existing, dup := schemas[name]; dup && !reflect.DeepEqual(existing, def) {
				return nil, fmt.Errorf("clientgen: conflicting definitions for namespaced schema %q", name)
			}
			schemas[name] = def
		}
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": title, "version": "1.0.0"},
		"paths":   paths,
		"components": map[string]any{
			"schemas": schemas,
		},
	}, nil
}

// schemaRefPrefix is the JSON-pointer prefix every component schema $ref carries.
const schemaRefPrefix = "#/components/schemas/"

func prefixRefs(doc map[string]any, prefix string) {
	rewriteRefStrings(doc, prefix)
	comps, ok := doc["components"].(map[string]any)
	if !ok {
		return
	}
	sch, ok := comps["schemas"].(map[string]any)
	if !ok {
		return
	}
	renamed := make(map[string]any, len(sch))
	for name, def := range sch {
		renamed[prefix+name] = def
	}
	comps["schemas"] = renamed
}

func rewriteRefStrings(node any, prefix string) {
	switch v := node.(type) {
	case map[string]any:
		for k, val := range v {
			if s, ok := val.(string); ok && k == "$ref" && strings.HasPrefix(s, schemaRefPrefix) {
				v[k] = schemaRefPrefix + prefix + strings.TrimPrefix(s, schemaRefPrefix)
				continue
			}
			rewriteRefStrings(val, prefix)
		}
	case []any:
		for _, item := range v {
			rewriteRefStrings(item, prefix)
		}
	}
}
