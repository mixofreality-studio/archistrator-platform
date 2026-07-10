package composegen

import "strings"

// writeHTTP emits the security Utility + the generated web server + the
// composition-root ExtraMounts + otelhttp wrap + serve/shutdown. When the
// container has no web-exposed Manager it degrades to a worker-only container
// that simply blocks until the shutdown signal.
func writeHTTP(b *strings.Builder, r *resolved) {
	if len(r.webMgrs) == 0 {
		writeWorkerOnlyWait(b)
		return
	}
	writeSecurity(b)
	writeWebManagersBundle(b, r)
	writeServer(b, r)
	writeServe(b)
	writeShutdown(b)
}

// writeWorkerOnlyWait blocks until the shutdown signal (no HTTP surface).
func writeWorkerOnlyWait(b *strings.Builder) {
	b.WriteString("\t<-ctx.Done()\n")
	b.WriteString("\tlogger.Info(\"shutdown signal received; draining\")\n")
	b.WriteString("\treturn nil\n")
}

// writeSecurity builds the security Utility + validator + dev config.
func writeSecurity(b *strings.Builder) {
	b.WriteString("\t// Security Utility + auth boundary.\n")
	b.WriteString("\tsec := security.New(security.WithPolicyDecisionPoint(hooks.PolicyDecisionPoint()))\n")
	b.WriteString("\tvalidator, err := hooks.TokenValidator(ctx, cfg)\n")
	b.WriteString("\tif err != nil {\n\t\treturn err\n\t}\n")
	b.WriteString("\tdev := hooks.DevConfig(cfg)\n\n")
}

// writeWebManagersBundle assembles the WebManagers bundle + applies the logging
// wrap hook.
func writeWebManagersBundle(b *strings.Builder, r *resolved) {
	b.WriteString("\twebManagers := WebManagers{\n")
	for _, mc := range r.webMgrs {
		b.WriteString("\t\t" + mc.iface + ": " + mc.varName + ",\n")
	}
	b.WriteString("\t}\n")
	b.WriteString("\twebManagers = hooks.WrapManagers(webManagers)\n\n")
}

// writeServer emits the generated NewServer call + root mux + ExtraMounts +
// otelhttp wrap.
func writeServer(b *strings.Builder, r *resolved) {
	b.WriteString("\tgenServer := web.NewServer(dev, validator,\n")
	for _, mc := range r.webMgrs {
		b.WriteString("\t\t&" + mc.webAlias + ".Handler{Manager: webManagers." + mc.iface + ", Security: sec},\n")
	}
	b.WriteString("\t)\n")
	b.WriteString("\troot := http.NewServeMux()\n")
	b.WriteString("\troot.Handle(\"/\", genServer)\n")
	b.WriteString("\thooks.ExtraMounts(root, cfg, dev, validator, webManagers)\n")
	b.WriteString("\thandler := otelhttp.NewHandler(root, serviceName)\n")
	b.WriteString("\tsrv := &http.Server{\n")
	b.WriteString("\t\tAddr:              cfg.ListenAddr,\n")
	b.WriteString("\t\tHandler:           handler,\n")
	b.WriteString("\t\tReadHeaderTimeout: 10 * time.Second,\n")
	b.WriteString("\t}\n\n")
}

// writeServe emits the listen goroutine.
func writeServe(b *strings.Builder) {
	b.WriteString("\tserverErr := make(chan error, 1)\n")
	b.WriteString("\tgo func() {\n")
	b.WriteString("\t\tlogger.Info(\"http server listening\", \"addr\", cfg.ListenAddr)\n")
	b.WriteString("\t\tif err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {\n")
	b.WriteString("\t\t\tserverErr <- err\n")
	b.WriteString("\t\t}\n")
	b.WriteString("\t}()\n\n")
}

// writeShutdown emits the shutdown select + graceful drain.
func writeShutdown(b *strings.Builder) {
	b.WriteString("\tselect {\n")
	b.WriteString("\tcase <-ctx.Done():\n")
	b.WriteString("\t\tlogger.Info(\"shutdown signal received; draining\")\n")
	b.WriteString("\tcase err := <-serverErr:\n")
	b.WriteString("\t\treturn err\n")
	b.WriteString("\t}\n\n")
	b.WriteString("\tshutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)\n")
	b.WriteString("\tdefer cancel()\n")
	b.WriteString("\tif err := srv.Shutdown(shutdownCtx); err != nil {\n")
	b.WriteString("\t\tlogger.Error(\"http graceful shutdown failed\", \"err\", err)\n")
	b.WriteString("\t\treturn err\n")
	b.WriteString("\t}\n")
	b.WriteString("\tlogger.Info(\"http server stopped cleanly\")\n")
	b.WriteString("\treturn nil\n")
}
