package composegen

import (
	"go/format"
	"path"
	"regexp"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// HooksSeedFile is the hand-written half of the composition root, seeded once.
const HooksSeedFile = "hooks.go"

// HooksSeed renders the starting cmd/server/hooks.go: func main and an implementation of the
// generated Hooks interface. It is written once and is the app's own code afterwards. Where a
// hook has a value that is always safe it returns that value; where only the app can answer
// (the authorization policy, an external service's URL) it panics with FILL, so the composition
// root compiles from the first generation and a missed decision is loud at boot.
func HooksSeed(m *projectmodel.Model, cfg Config) ([]byte, error) {
	if err := validateConfig(m, cfg); err != nil {
		return nil, err
	}
	r, err := resolve(m, cfg)
	if err != nil {
		return nil, err
	}
	var body strings.Builder
	body.WriteString(seedMain)
	for _, h := range deriveHooks(r) {
		name, _, _ := strings.Cut(h.line, "(")
		stmt := h.seed
		if stmt == "" {
			stmt = `panic("FILL: ` + name + `")`
		}
		body.WriteString("\nfunc (appHooks) " + h.line + " {\n\t" + stmt + "\n}\n")
	}
	imports := newImportSet()
	imports.add("log/slog", "")
	imports.add("os", "")
	src := body.String()
	for p, alias := range r.computeImports().byPath {
		name := alias
		if name == "" {
			name = path.Base(p)
		}
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\.`).MatchString(src) {
			imports.add(p, alias)
		}
	}
	out := "package " + cfg.PackageName + "\n\n" + imports.render() + src
	formatted, err := format.Source([]byte(out))
	if err != nil {
		return nil, wrapFormatErr(err, out)
	}
	return formatted, nil
}

const seedMain = `// main loads the configuration and runs the generated composition root.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := LoadConfig()
	if err != nil {
		logger.Error("server exited with error", "err", err)
		os.Exit(1)
	}
	if err := RunGenerated(cfg, appHooks{}, logger); err != nil {
		logger.Error("server exited with error", "err", err)
		os.Exit(1)
	}
}

// appHooks answers what the deployment model cannot express.
type appHooks struct{}
`
