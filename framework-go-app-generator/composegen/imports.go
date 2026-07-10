package composegen

import (
	"sort"
	"strings"
)

// Framework + SDK import paths threaded into the emitted composition root.
const (
	pathTemporalClient    = "go.temporal.io/sdk/client"
	pathTemporalIntercept = "go.temporal.io/sdk/interceptor"
	pathTemporalWorker    = "go.temporal.io/sdk/worker"
	pathTemporalWorkflow  = "go.temporal.io/sdk/workflow"
	pathTemporalLog       = "go.temporal.io/sdk/log"
	pathTemporalOtel      = "go.temporal.io/sdk/contrib/opentelemetry"
	pathOtelHTTP          = "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	pathFwBase        = "github.com/mixofreality-studio/archistrator-platform"
	pathOtelInfra     = pathFwBase + "/framework-go-infrastructure-otel"
	pathPostgresInfra = pathFwBase + "/framework-go-infrastructure-postgres"
	pathTemporalInfra = pathFwBase + "/framework-go-infrastructure-temporal"
	pathSecurity      = pathFwBase + "/framework-go/utilities/security"
	pathTelemetry     = pathFwBase + "/framework-go/utilities/telemetry"
)

// importSpec is one import: a path with an optional explicit alias.
type importSpec struct {
	path  string
	alias string
}

// importSet accumulates the emitted file's imports, deduplicating by path.
type importSet struct {
	byPath map[string]string // path -> alias ("" = none)
}

func newImportSet() *importSet { return &importSet{byPath: map[string]string{}} }

// add records an import (later non-empty aliases win over an empty one).
func (s *importSet) add(path, alias string) {
	if path == "" {
		return
	}
	if existing, ok := s.byPath[path]; ok && existing != "" {
		return
	}
	s.byPath[path] = alias
}

// render emits a grouped import block: the standard library first, then the
// rest, each group sorted (gofmt-stable).
func (s *importSet) render() string {
	var std, other []importSpec
	for p, a := range s.byPath {
		spec := importSpec{path: p, alias: a}
		if isStdlib(p) {
			std = append(std, spec)
		} else {
			other = append(other, spec)
		}
	}
	sortSpecs(std)
	sortSpecs(other)

	var b strings.Builder
	b.WriteString("import (\n")
	writeGroup(&b, std)
	if len(std) > 0 && len(other) > 0 {
		b.WriteString("\n")
	}
	writeGroup(&b, other)
	b.WriteString(")\n\n")
	return b.String()
}

func writeGroup(b *strings.Builder, specs []importSpec) {
	for _, sp := range specs {
		if sp.alias != "" {
			b.WriteString("\t" + sp.alias + " \"" + sp.path + "\"\n")
			continue
		}
		b.WriteString("\t\"" + sp.path + "\"\n")
	}
}

func sortSpecs(specs []importSpec) {
	sort.Slice(specs, func(i, j int) bool { return specs[i].path < specs[j].path })
}

// isStdlib reports whether an import path is a standard-library package (its
// first path segment carries no dot, i.e. no domain).
func isStdlib(p string) bool {
	seg := p
	if i := strings.Index(p, "/"); i >= 0 {
		seg = p[:i]
	}
	return !strings.Contains(seg, ".")
}

// computeImports collects every import the walk for r needs.
func (r *resolved) computeImports() *importSet {
	s := newImportSet()
	for _, p := range []string{"context", "log/slog", "os/signal", "syscall", "time"} {
		s.add(p, "")
	}
	r.addInfraImports(s)
	r.addComponentImports(s)
	if len(r.webMgrs) > 0 {
		r.addWebImports(s)
	}
	return s
}

// addInfraImports adds the satellite + Temporal SDK imports.
func (r *resolved) addInfraImports(s *importSet) {
	if r.hasOtel {
		s.add(pathOtelInfra, "otelinfra")
		s.add(pathTelemetry, "telemetry")
	}
	if r.hasTemporal {
		s.add(pathTemporalClient, "")
		s.add(pathTemporalIntercept, "")
		s.add(pathTemporalWorkflow, "")
		s.add(pathTemporalOtel, "temporalotel")
		s.add(pathTemporalInfra, "temporalprop")
		s.add(pathTemporalLog, "tlog")
	}
	if len(r.managers) > 0 {
		s.add(pathTemporalWorker, "")
	}
	if r.consumesPostgres() {
		s.add(pathPostgresInfra, "postgresinfra")
	}
}

// addComponentImports adds the RA / engine / manager package imports.
func (r *resolved) addComponentImports(s *importSet) {
	for _, ra := range r.ras {
		s.add(ra.importPath, "")
	}
	for _, e := range r.engines {
		s.add(e.importPath, "")
	}
	for _, mc := range r.managers {
		s.add(mc.importPath, "")
	}
}

// addWebImports adds the transport + security + otelhttp imports.
func (r *resolved) addWebImports(s *importSet) {
	s.add("errors", "")
	s.add("net/http", "")
	s.add(pathOtelHTTP, "otelhttp")
	s.add(pathSecurity, "security")
	s.add(r.cfg.ModulePath+"/internal/client/web", "web")
	for _, mc := range r.webMgrs {
		s.add(mc.webImport, mc.webAlias)
	}
}

// consumesPostgres reports whether any binding variant consumes a postgres
// infra key (⇒ the shared pool satellite is constructed).
func (r *resolved) consumesPostgres() bool {
	for key := range r.consumedKeys {
		if r.infra[key].Substrate == "postgres" {
			return true
		}
	}
	return false
}
