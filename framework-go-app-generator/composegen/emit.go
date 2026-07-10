package composegen

import (
	"go/format"
	"strings"
)

// emitMain renders and gofmt's main.gen.go.
func emitMain(r *resolved) ([]byte, error) {
	var b strings.Builder
	b.WriteString(genHeader)
	b.WriteString("\npackage " + r.pkgName + "\n\n")
	writeWebExposureNote(&b, r)
	b.WriteString(r.computeImports().render())
	b.WriteString("// serviceName is the OTel service.name + span-name root for this container.\n")
	b.WriteString("const serviceName = \"" + r.serviceName + "\"\n\n")
	writeWebManagersType(&b, r)
	writeHooksInterface(&b, r)
	writeRunGenerated(&b, r)

	src, err := format.Source([]byte(b.String()))
	if err != nil {
		return nil, wrapFormatErr(err, b.String())
	}
	return src, nil
}

// writeWebExposureNote records, in the emitted file's header, that the
// web-exposed manager set was DRIVER-CONFIGURED (B1: Config.WebExposedManagers)
// rather than derived from System relationships — the two can genuinely
// diverge (a client→manager relationship with no generated web handler
// package, e.g. archistrator's billingManager) and a reader of the generated
// file should be able to tell which derivation produced webMgrs without
// cross-referencing the driver invocation. Silent when the driver left web
// exposure to the model (Config.WebExposedManagers == nil).
func writeWebExposureNote(b *strings.Builder, r *resolved) {
	if r.cfg.WebExposedManagers == nil {
		return
	}
	set := "(none)"
	if keys := webExposedKeys(r); len(keys) > 0 {
		set = strings.Join(keys, ", ")
	}
	b.WriteString("// Web-exposed managers are DRIVER-CONFIGURED (Config.WebExposedManagers),\n")
	b.WriteString("// NOT derived from System relationships: " + set + ".\n\n")
}

// webExposedKeys is the resolved web-exposed manager component keys, in
// resolution order (already deterministic — sorted contract-key order).
func webExposedKeys(r *resolved) []string {
	keys := make([]string, 0, len(r.webMgrs))
	for _, mc := range r.webMgrs {
		keys = append(keys, mc.key)
	}
	return keys
}

// writeWebManagersType emits the typed bundle of web-exposed managers threaded
// through WrapManagers + ExtraMounts (only when there are web-exposed managers).
func writeWebManagersType(b *strings.Builder, r *resolved) {
	if len(r.webMgrs) == 0 {
		return
	}
	b.WriteString("// WebManagers is the typed bundle of web-exposed managers the composition\n")
	b.WriteString("// root threads through the logging wrap and into the transports.\n")
	b.WriteString("type WebManagers struct {\n")
	for _, mc := range r.webMgrs {
		b.WriteString("\t" + mc.iface + " " + mc.alias + "." + mc.iface + "\n")
	}
	b.WriteString("}\n\n")
}

// writeHooksInterface emits the derived Hooks interface.
func writeHooksInterface(b *strings.Builder, r *resolved) {
	b.WriteString("// Hooks is the composition-root policy seam: the genuinely-compositional\n")
	b.WriteString("// decisions the deployment model cannot express. The hand hooks.go implements\n")
	b.WriteString("// it; RunGenerated calls it.\n")
	b.WriteString("type Hooks interface {\n")
	for i, h := range r.hooks {
		if i > 0 {
			b.WriteString("\n")
		}
		for _, d := range h.doc {
			b.WriteString("\t// " + d + "\n")
		}
		b.WriteString("\t" + h.line + "\n")
	}
	b.WriteString("}\n\n")
}

// writeRunGenerated emits the ordered boot walk.
func writeRunGenerated(b *strings.Builder, r *resolved) {
	b.WriteString("// RunGenerated performs the container's boot walk: signal context → telemetry\n")
	b.WriteString("// → Temporal client → Postgres pool → ResourceAccess bindings → Engines →\n")
	b.WriteString("// Managers + Workers → HTTP server, with graceful shutdown. All policy is\n")
	b.WriteString("// delegated to hooks; everything else is derived from the model.\n")
	b.WriteString("func RunGenerated(cfg *Config, hooks Hooks, logger *slog.Logger) error {\n")
	writeSignalCtx(b)
	writeProfile(b, r)
	writeTelemetry(b, r)
	writeTemporal(b, r)
	writePostgres(b, r)
	writeRABlocks(b, r)
	writeEngines(b, r)
	writeManagers(b, r)
	writeHTTP(b, r)
	b.WriteString("}\n")
}

func writeSignalCtx(b *strings.Builder) {
	b.WriteString("\t// Root context cancelled on the first SIGINT/SIGTERM.\n")
	b.WriteString("\tctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)\n")
	b.WriteString("\tdefer stop()\n\n")
}

// writeProfile emits the active-profile resolution, only when a binding switches
// on it.
func writeProfile(b *strings.Builder, r *resolved) {
	if !needProfile(r) {
		return
	}
	b.WriteString("\t// Active deployment profile — the binding variant switches key off this.\n")
	b.WriteString("\tprofile := hooks.ResolveProfile(cfg)\n\n")
}

// needProfile reports whether any binding emits a profile switch (⇒ a resolved
// profile): a multi-arm binding, or a single-arm optional binding whose
// missing-profile arms leave it nil.
func needProfile(r *resolved) bool {
	for _, ra := range r.ras {
		if ra.switched {
			return true
		}
	}
	return false
}

func writeTelemetry(b *strings.Builder, r *resolved) {
	if !r.hasOtel {
		return
	}
	b.WriteString("\t// Telemetry — the OTLP satellite builds the Provider; the utilities facade\n")
	b.WriteString("\t// installs it as the OTel globals + a W3C propagator and bridges slog. MUST\n")
	b.WriteString("\t// precede the Temporal dial + the HTTP wrap so both pick up the globals.\n")
	b.WriteString("\ttelemetryProvider, err := otelinfra.NewProvider(ctx, otelinfra.Options{ServiceName: serviceName})\n")
	b.WriteString("\tif err != nil {\n\t\treturn err\n\t}\n")
	b.WriteString("\tshutdownTelemetry := telemetry.Install(telemetryProvider, telemetry.Options{ServiceName: serviceName, Logger: logger})\n")
	b.WriteString("\tdefer func() {\n")
	b.WriteString("\t\tshutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)\n")
	b.WriteString("\t\tdefer cancel()\n")
	b.WriteString("\t\tif err := shutdownTelemetry(shutdownCtx); err != nil {\n")
	b.WriteString("\t\t\tlogger.Error(\"telemetry shutdown failed\", \"err\", err)\n")
	b.WriteString("\t\t}\n\t}()\n\n")
}

func writeTemporal(b *strings.Builder, r *resolved) {
	if !r.hasTemporal {
		return
	}
	hostport := "cfg." + infraFieldName(r.temporalKey, "HOSTPORT")
	namespace := "cfg." + infraFieldName(r.temporalKey, "NAMESPACE")
	b.WriteString("\t// Temporal control-plane client — the Managers own it; the embedded Workers\n")
	b.WriteString("\t// poll their task queues. Principal propagator + OTel tracing interceptor +\n")
	b.WriteString("\t// metrics handler bind the control plane to the installed globals.\n")
	b.WriteString("\ttracingInterceptor, err := temporalotel.NewTracingInterceptor(temporalotel.TracerOptions{})\n")
	b.WriteString("\tif err != nil {\n\t\treturn err\n\t}\n")
	b.WriteString("\ttc, err := client.DialContext(ctx, client.Options{\n")
	b.WriteString("\t\tHostPort:           " + hostport + ",\n")
	b.WriteString("\t\tNamespace:          " + namespace + ",\n")
	b.WriteString("\t\tLogger:             hooks.TemporalLogger(logger),\n")
	b.WriteString("\t\tContextPropagators: []workflow.ContextPropagator{temporalprop.NewPrincipalPropagator()},\n")
	b.WriteString("\t\tInterceptors:       []interceptor.ClientInterceptor{tracingInterceptor},\n")
	b.WriteString("\t\tMetricsHandler:     temporalotel.NewMetricsHandler(temporalotel.MetricsHandlerOptions{}),\n")
	b.WriteString("\t})\n")
	b.WriteString("\tif err != nil {\n\t\treturn err\n\t}\n")
	b.WriteString("\tdefer tc.Close()\n")
	b.WriteString("\tlogger.Info(\"temporal client dialed\", \"hostPort\", " + hostport + ", \"namespace\", " + namespace + ")\n\n")
}

func writePostgres(b *strings.Builder, r *resolved) {
	if !r.consumesPostgres() {
		return
	}
	url := "cfg." + infraFieldName(r.postgresKey, "URL")
	b.WriteString("\t// Postgres pool — the shared satellite the postgres-backed RAs are built on.\n")
	b.WriteString("\tpool, err := postgresinfra.NewPool(ctx, " + url + ")\n")
	b.WriteString("\tif err != nil {\n\t\treturn err\n\t}\n")
	b.WriteString("\tdefer pool.Close()\n\n")
}

func writeEngines(b *strings.Builder, r *resolved) {
	if len(r.engines) == 0 {
		return
	}
	b.WriteString("\t// Engines — pure, deterministic, dependency-free.\n")
	for _, e := range r.engines {
		b.WriteString("\t" + e.varName + " := " + e.ctor + "()\n")
	}
	b.WriteString("\n")
}

// wrapFormatErr annotates a gofmt failure with the unformatted source.
func wrapFormatErr(err error, src string) error {
	return &formatError{err: err, src: src}
}
