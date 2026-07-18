package methodassets

import (
	"bytes"
	"embed"
	"encoding/json"
	"strings"
	"text/template"
)

// Pinned platform versions the scaffold seeds (spec §6).
//
// AppGeneratorVersion and ProjectModelVersion are currently unreferenced by
// any rendered template: framework-go-app-generator and
// framework-go-projectmodel ship no cmd/ main packages (library-only), so
// go.mod.tmpl's require/tool blocks omit them (see its EARMARK comment). Kept
// here so a future platform release that ships those CLI wrappers can wire
// the require/tool lines back in against these same pins.
const (
	GoVersion            = "1.25.0"
	FrameworkGoVersion   = "v0.5.2"
	AppGeneratorVersion  = "v0.6.1"
	HTTPGeneratorVersion = "v0.3.0"
	MCPGeneratorVersion  = "v0.2.0"
	ProjectModelVersion  = "v0.2.1"
)

// ScaffoldData is the template data rendered into a newly seeded project repo.
type ScaffoldData struct {
	ModulePath string // github.com/<owner>/<repo>
	// AppSlug is the GitHub App slug seated into aiarch-construct.yml's
	// `allowed_bots` input. REQUIRED: the construct workflow template has no
	// empty-AppSlug guard, so an empty value here renders `allowed_bots: `
	// (empty) and construction dispatch is refused as a non-allow-listed bot.
	AppSlug               string
	ProjectID             string // project.json id (repo name)
	Owner                 string // org/user login
	Name                  string // project display name
	StateMcpModulePath    string // archistrator state-MCP module path
	StateMcpModuleVersion string // pin (version or SHA)
}

// internal render payload: ScaffoldData + version consts.
type renderData struct {
	ScaffoldData
	GoVersion, FrameworkGoVersion, AppGeneratorVersion,
	HTTPGeneratorVersion, MCPGeneratorVersion, ProjectModelVersion string
}

var renderedPaths = map[string]string{ // dest path -> template asset
	".github/workflows/aiarch-design.yml":    "assets/workflows/aiarch-design.yml.tmpl",
	".github/workflows/aiarch-construct.yml": "assets/workflows/aiarch-construct.yml.tmpl",
	"go.mod":                                 "assets/scaffold/go.mod.tmpl",
	"aiarch_method_test.go":                  "assets/scaffold/aiarch_method_test.go.tmpl",
}

// ScaffoldFiles renders the complete managed-scaffold file set for one app
// repo: workflows + go.mod + method test + the .claude tree, plus a seat
// manifest at manifestPath so the server can fingerprint the seated set by
// reading one file instead of comparing every .claude/** entry on every
// design dispatch. It deliberately does NOT seed .aiarch/state/project.json:
// the archistrator server's projectStateAccess.CreateProject already seeds
// that path at Version 1, and the scaffold must not double-write a
// server-owned path.
func ScaffoldFiles(data ScaffoldData) (map[string][]byte, error) {
	out, err := ClaudeFiles()
	if err != nil {
		return nil, err
	}
	rd := renderData{ScaffoldData: data, GoVersion: GoVersion,
		FrameworkGoVersion: FrameworkGoVersion, AppGeneratorVersion: AppGeneratorVersion,
		HTTPGeneratorVersion: HTTPGeneratorVersion, MCPGeneratorVersion: MCPGeneratorVersion,
		ProjectModelVersion: ProjectModelVersion}
	for dest, asset := range renderedPaths {
		b, err := renderAsset(assetsFS, asset, rd)
		if err != nil {
			return nil, err
		}
		out[dest] = b
	}
	out["internal/.gitkeep"] = []byte("")

	mb, err := json.MarshalIndent(buildManifest(claudeSubset(out)), "", "  ")
	if err != nil {
		return nil, err
	}
	out[manifestPath] = append(mb, '\n')
	return out, nil
}

// claudeSubset returns the entries of files keyed under ".claude/" — the set
// the seat manifest describes. None of ScaffoldFiles' rendered destinations
// (renderedPaths, "internal/.gitkeep") fall under that prefix, so this
// yields exactly the embedded .claude tree ClaudeFiles produced.
func claudeSubset(files map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(files))
	for p, b := range files {
		if strings.HasPrefix(p, ".claude/") {
			out[p] = b
		}
	}
	return out
}

func renderAsset(fsys embed.FS, name string, data renderData) ([]byte, error) {
	raw, err := fsys.ReadFile(name)
	if err != nil {
		return nil, err
	}
	t, err := template.New(name).Delims("[[", "]]").Parse(string(raw))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
