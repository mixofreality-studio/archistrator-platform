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
	return HooksSeedWith(m, cfg, SeedOptions{})
}

// SeedOptions varies HooksSeedWith's output without varying its seam catalogue or import
// resolution; the zero value is the standard seed.
type SeedOptions struct {
	// Header, when set, is written before the package clause (e.g. a generated-code marker).
	Header string
	// Body, when set, may replace a hook's body: a non-empty result is used in place of the
	// standard seed (or of its FILL panic).
	Body func(Hook) string
	// Extra is source appended to the file after the hooks (declarations the bodies use). The
	// file's imports are resolved over it too.
	Extra string
}

// HooksSeedWith is HooksSeed with opts applied. The conformance pipeline's nullgen uses it to
// answer every seam with a value that lets an unbuilt model compose.
func HooksSeedWith(m *projectmodel.Model, cfg Config, opts SeedOptions) ([]byte, error) {
	if err := validateConfig(m, cfg); err != nil {
		return nil, err
	}
	r, err := resolve(m, cfg)
	if err != nil {
		return nil, err
	}
	hooks, err := hooksOf(r)
	if err != nil {
		return nil, err
	}
	var body strings.Builder
	body.WriteString(seedMain)
	for i, h := range deriveHooks(r) {
		body.WriteString(seedMethod(h, hooks[i], opts.Body))
	}
	if opts.Extra != "" {
		body.WriteString("\n" + opts.Extra)
	}
	src := body.String()
	out := opts.Header + "package " + cfg.PackageName + "\n\n" + seedImports(r, src).render() + src
	formatted, err := format.Source([]byte(out))
	if err != nil {
		return nil, wrapFormatErr(err, out)
	}
	return formatted, nil
}

// seedMethod renders one hook's method in the seeded hooks.go: replace's body when it gives one,
// else the standard seed, else a FILL panic.
func seedMethod(h hookMethod, view Hook, replace func(Hook) string) string {
	stmt := h.seed
	if replace != nil {
		if replaced := replace(view); replaced != "" {
			stmt = replaced
		}
	}
	if stmt == "" {
		stmt = `panic("FILL: ` + view.Name + `")`
	}
	recv := "appHooks"
	if h.seedRecv != "" {
		recv = h.seedRecv + " appHooks"
	}
	return "\nfunc (" + recv + ") " + h.line + " {\n\t" + stmt + "\n}\n"
}

// seedImports is the composition root's imports the seed's source actually names.
func seedImports(r *resolved, src string) *importSet {
	imports := newImportSet()
	imports.add("log/slog", "")
	imports.add("os", "")
	candidates := r.computeImports().byPath
	candidates["time"] = ""
	for p, alias := range candidates {
		name := alias
		if name == "" {
			name = path.Base(p)
		}
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\.`).MatchString(src) {
			imports.add(p, alias)
		}
	}
	return imports
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
