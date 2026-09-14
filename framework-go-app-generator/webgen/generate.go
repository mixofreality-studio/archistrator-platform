package webgen

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed templates
var templates embed.FS

// File is one output, at a path relative to the webApp root.
type File struct {
	Path    string
	Content []byte
}

// Generate renders the GENERATED files: rewritten on every run, never edited by
// hand, and compared by the drift check.
//
//	src/api/ops.gen.ts                     OpsClient, OP_BINDINGS, REST + MCP transports
//	preview/fixtures.schema.json           the fixture JSON Schema, from the OAS
//	src/previewShell/fixtures.gen.test.ts  node:test: every fixture validates against it
//
// fixtures.schema.json sits outside src/ so the app's prettier format:check
// (which would collapse short arrays) never sees it; it is JSON.stringify's
// layout, byte for byte.
func Generate(in Inputs) ([]File, error) {
	bindings, err := in.Bindings()
	if err != nil {
		return nil, err
	}
	ops, err := EmitOps(bindings)
	if err != nil {
		return nil, err
	}
	test, err := render("fixtures.gen.test.ts.tmpl", in.Config)
	if err != nil {
		return nil, err
	}
	schema := Stringify(FixtureSchema(in, bindings)) + "\n"
	return []File{
		{Path: "src/api/ops.gen.ts", Content: ops},
		{Path: "preview/fixtures.schema.json", Content: []byte(schema)},
		{Path: "src/previewShell/fixtures.gen.test.ts", Content: test},
	}, nil
}

// scaffoldFiles maps each scaffold template to its output path.
var scaffoldFiles = []struct{ tmpl, out string }{
	{"errors.ts.tmpl", "src/contracts/errors.ts"},
	{"opsContext.tsx.tmpl", "src/api/opsContext.tsx"},
	{"opTypes.ts.tmpl", "src/api/opTypes.ts"},
	{"fixtureOps.ts.tmpl", "src/api/fixtureOps.ts"},
	{"previewShell.main.tsx.tmpl", "src/previewShell/main.tsx"},
	{"preview.html.tmpl", "preview.html"},
	{"vite.preview.config.ts.tmpl", "vite.preview.config.ts"},
}

// Scaffold renders the SCAFFOLD files: the preview entry, its Vite config and
// the transport's app-side modules. They are written once (WriteScaffold skips
// any that exist) and are the app's own code afterwards.
//
// The preview entry assumes the webApp layout the method-assets template gives
// every generated app: src/App.tsx default-exports App({ router }),
// src/routes/router.tsx exports createAppRouter(history, { ops }), and
// src/index.css holds the global styles.
func Scaffold(c Config) ([]File, error) {
	var out []File
	for _, f := range scaffoldFiles {
		content, err := render(f.tmpl, c)
		if err != nil {
			return nil, err
		}
		out = append(out, File{Path: f.out, Content: content})
	}
	return out, nil
}

// DefaultConfig renders a starting webgen.json for app: the web-client surface,
// httpgen's and mcpgen's conventional output paths, and framework-go's
// /api/userinfo as the one composition route every app has.
func DefaultConfig(app, surface string) ([]byte, error) {
	c := Config{App: app, Surface: surface, OAS: "-", MCPTools: "-", FixturesEnv: DefaultFixturesEnv(app)}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return render("webgen.json.tmpl", c)
}

// DefaultFixturesEnv is <APP>_PREVIEW_FIXTURES.
func DefaultFixturesEnv(app string) string {
	return strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(app)) + "_PREVIEW_FIXTURES"
}

func render(name string, c Config) ([]byte, error) {
	tmpl, err := templates.ReadFile("templates/" + name)
	if err != nil {
		return nil, err
	}
	out := strings.NewReplacer(
		"@@APP@@", c.App,
		"@@SURFACE@@", c.Surface,
		"@@FIXTURES_ENV@@", c.FixturesEnv,
	).Replace(string(tmpl))
	if strings.Contains(out, "@@") {
		return nil, fmt.Errorf("webgen: template %s has an unknown placeholder", name)
	}
	return []byte(out), nil
}

// Write writes files under dir, creating directories as needed.
func Write(dir string, files []File) error {
	for _, f := range files {
		if err := writeFile(filepath.Join(dir, f.Path), f.Content); err != nil {
			return err
		}
	}
	return nil
}

// WriteScaffold writes the files that do not exist yet under dir and returns
// the paths it wrote; an existing file is the app's and is left alone.
func WriteScaffold(dir string, files []File) ([]string, error) {
	var wrote []string
	for _, f := range files {
		p := filepath.Join(dir, f.Path)
		if _, err := os.Stat(p); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return wrote, err
		}
		if err := writeFile(p, f.Content); err != nil {
			return wrote, err
		}
		wrote = append(wrote, f.Path)
	}
	return wrote, nil
}

// Drift returns the generated files under dir that are missing or differ.
func Drift(dir string, files []File) ([]string, error) {
	var stale []string
	for _, f := range files {
		got, err := os.ReadFile(filepath.Join(dir, f.Path)) // #nosec G304 -- a path the generator itself owns
		if errors.Is(err, fs.ErrNotExist) || (err == nil && string(got) != string(f.Content)) {
			stale = append(stale, f.Path)
		} else if err != nil {
			return nil, err
		}
	}
	return stale, nil
}

func writeFile(p string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return err
	}
	return os.WriteFile(p, content, 0o600)
}
