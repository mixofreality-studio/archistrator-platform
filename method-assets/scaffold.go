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
// The generators are `tool` directives in the seated go.mod, invoked as `go tool
// <name>`. `go run pkg@version` cannot work for them: the published generator modules
// carry replace directives (they need them for atomic cross-module changes), and the go
// command refuses that form for such a module. scaffold_test.go bans it.
//
// assets/scaffold/go.mod.tmpl and go.sum.tmpl are the TIDIED form of these pins, written
// by cmd/gen-scaffold-mod at release: a hand-listed require set would turn the
// command:tidy gate red on the first commit of every project.
const (
	// GoVersion 1.26+ is required by the seated go-checks workflow's
	// analyzer-based `go fix -diff` gate; runners resolve it via the go.mod
	// toolchain directive / setup-go.
	GoVersion            = "1.26.0"
	FrameworkGoVersion   = "v0.17.0"
	AppGeneratorVersion  = "v0.14.0"
	HTTPGeneratorVersion = "v0.4.0"
	MCPGeneratorVersion  = "v0.3.0"
	ProjectModelVersion  = "v0.2.4"

	// AppGeneratorModulePath is the published module that ships the generators.
	AppGeneratorModulePath = "github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator"
)

// platformModule is the import prefix of the platform's module family.
const platformModule = "github.com/mixofreality-studio/archistrator-platform/"

// ScaffoldTools is the generator CLIs every app gets as `go tool <name>`.
func ScaffoldTools() []string {
	return []string{
		AppGeneratorModulePath + "/cmd/appgen",
		AppGeneratorModulePath + "/cmd/modelgen",
		AppGeneratorModulePath + "/cmd/testgen",
		AppGeneratorModulePath + "/cmd/webgen",
		platformModule + "framework-go-http-generator/cmd/httpgen",
		platformModule + "framework-go-mcp-generator/cmd/mcpgen",
	}
}

// ScaffoldRequires is module path → version of the pins the tidied go.mod starts from.
func ScaffoldRequires() map[string]string {
	return map[string]string{
		platformModule + "framework-go":                FrameworkGoVersion,
		AppGeneratorModulePath:                         AppGeneratorVersion,
		platformModule + "framework-go-http-generator": HTTPGeneratorVersion,
		platformModule + "framework-go-mcp-generator":  MCPGeneratorVersion,
	}
}

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
	GoVersion, FrameworkGoVersion, AppGeneratorVersion, AppGeneratorModulePath,
	HTTPGeneratorVersion, MCPGeneratorVersion, ProjectModelVersion string
}

var renderedPaths = map[string]string{ // dest path -> template asset
	".github/workflows/aiarch-design.yml":    "assets/workflows/aiarch-design.yml.tmpl",
	".github/workflows/aiarch-construct.yml": "assets/workflows/aiarch-construct.yml.tmpl",
	".github/workflows/go-checks.yml":        "assets/workflows/go-checks.yml.tmpl",
	"go.mod":                                 "assets/scaffold/go.mod.tmpl",
	"go.sum":                                 "assets/scaffold/go.sum.tmpl",
	// The Method gate. Under internal/ because `go test ./internal/` is the one argv
	// the arch check has for every app; beside the code it holds to the layer model.
	ArchGatePath: "assets/scaffold/aiarch_arch_test.go.tmpl",
	"Makefile":   "assets/scaffold/Makefile.tmpl",
	// The generated-code and scenario-test make targets the construction commands and
	// the local venue call; the app Makefile includes it. Module-root-relative: the
	// seated app module lives at the repo root (go.mod above), so there is no server/
	// prefix.
	"Makefile.scenarios.mk": "assets/scaffold/Makefile.scenarios.mk.tmpl",
	// The shared archistrator lint baseline (standard set + revive + gocritic +
	// gocyclo + gochecksumtype/gosec) the go-checks workflow runs against.
	".golangci.yml": "assets/scaffold/golangci.yml",
	// Sibling go.mod/go.sum edits union instead of conflicting.
	".gitattributes": "assets/scaffold/gitattributes",
}

// ArchGatePath is where the Method gate is seated.
const ArchGatePath = "internal/aiarch_arch_test.go"

// ScaffoldFiles renders the complete managed-scaffold file set for one app
// repo: workflows + go.mod + go.sum + Makefile + the Method gate + the .claude tree, plus a seat
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
		AppGeneratorModulePath: AppGeneratorModulePath,
		HTTPGeneratorVersion:   HTTPGeneratorVersion, MCPGeneratorVersion: MCPGeneratorVersion,
		ProjectModelVersion: ProjectModelVersion}
	for dest, asset := range renderedPaths {
		b, err := renderAsset(assetsFS, asset, rd)
		if err != nil {
			return nil, err
		}
		out[dest] = b
	}

	mb, err := json.MarshalIndent(buildManifest(claudeSubset(out)), "", "  ")
	if err != nil {
		return nil, err
	}
	out[manifestPath] = append(mb, '\n')
	return out, nil
}

// claudeSubset returns the entries of files keyed under ".claude/" — the set
// the seat manifest describes. None of ScaffoldFiles' rendered destinations
// (renderedPaths) fall under that prefix, so this
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
