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
	b.WriteString("\tsec := edgeSecurity()\n")
	b.WriteString("\tvalidator, err := hooks.TokenValidator(ctx, cfg)\n")
	b.WriteString("\tif err != nil {\n\t\treturn err\n\t}\n")
	// FIX-039: a dev principal configured under any profile but local is refused by name
	// before the edge is built — it would authenticate every request.
	b.WriteString("\tif _, err := cfg.LocalDevPrincipal(hooks.ResolveProfile(cfg)); err != nil {\n\t\treturn err\n\t}\n")
	b.WriteString("\tdev := hooks.DevConfig(cfg)\n")
	// Plan 3 Task 3.3: the local-profile allowlist the X-Dev-Principal header may name. It is
	// the framework's, set here and not by the hook, so the identities a launch declares reach
	// the edge in every app; the Config rule refuses it under any profile but local.
	b.WriteString("\tif dev.Principals, err = cfg.LocalDevPrincipals(hooks.ResolveProfile(cfg)); err != nil {\n\t\treturn err\n\t}\n\n")
}

// writeWebManagersBundle assembles the WebManagers bundle + applies the logging
// wrap hook.
func writeWebManagersBundle(b *strings.Builder, r *resolved) {
	b.WriteString("\twebManagers := WebManagers{\n")
	for _, mc := range r.webMgrs {
		b.WriteString("\t\t" + mc.iface + ": " + mc.varName + ",\n")
	}
	b.WriteString("\t}\n")
	b.WriteString("\twebManagers = hooks.WrapManagers(webManagers)\n")
	// Plan 3 Task 3.5: a web scenario run points the server at the scenario host's arrange server,
	// whose registered faults the Managers take. Innermost of the test instruments, so the null
	// wrapper below stays outermost. Local profile only: the Config rule refuses the address elsewhere.
	b.WriteString("\tscenariohostURL, err := cfg.LocalScenariohostURL(hooks.ResolveProfile(cfg))\n")
	b.WriteString("\tif err != nil {\n\t\treturn err\n\t}\n")
	b.WriteString("\twebManagers, err = applyFaults(hooks.ResolveProfile(cfg), scenariohostURL, webManagers)\n")
	b.WriteString("\tif err != nil {\n\t\treturn err\n\t}\n")
	// The null-subject run (testgen -discriminate) stubs named operations out here, outermost, so the
	// generated REST handlers and MCP tools never reach the Manager for them. Local profile only.
	b.WriteString("\twebManagers, err = applyNullOps(hooks.ResolveProfile(cfg), webManagers)\n")
	b.WriteString("\tif err != nil {\n\t\treturn err\n\t}\n\n")
}

// writeServer emits the generated NewServer call + root mux + ExtraMounts +
// otelhttp wrap.
func writeServer(b *strings.Builder, r *resolved) {
	b.WriteString("\tgenServer := web.NewServer(dev, validator,\n")
	for _, mc := range r.webMgrs {
		b.WriteString("\t\t&" + mc.webAlias + ".Handler{Manager: webManagers." + mc.iface + ", Security: sec},\n")
	}
	b.WriteString("\t)\n")
	writeRootMounts(b, r)
	b.WriteString("\thooks.ExtraMounts(root, cfg, dev, validator, sec, webManagers)\n")
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

// writeEdgeSecurity emits the one constructor of the security Utility every generated
// transport authorizes against. RunGenerated builds sec here, and so do the generated
// policy tests (authz.gen_test.go), so the policy the tests hold to the generated requests is
// the one the server really serves with: generatedPolicy (policy.gen.go), derived from the model.
func writeEdgeSecurity(b *strings.Builder, r *resolved) {
	if len(r.webMgrs) == 0 {
		return
	}
	b.WriteString("// edgeSecurity is the security Utility every generated transport (the REST handlers\n")
	b.WriteString("// and the MCP tool tables at /mcp) authorizes against, built from the policy generated\n")
	b.WriteString("// from the model. The generated policy tests build it the same way.\n")
	b.WriteString("func edgeSecurity() security.Security {\n")
	b.WriteString("\treturn security.New(security.WithPolicyDecisionPoint(generatedPolicy{}))\n")
	b.WriteString("}\n\n")
}
