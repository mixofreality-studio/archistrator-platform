package badmgr

// worker stands in for the Temporal worker.Worker interface's registration
// method, just enough to exercise the AST match on the call's selector name.
type worker interface {
	RegisterActivityWithOptions(fn interface{}, opts registerOptions)
}

type registerOptions struct{}

// registerHandActivity hand-calls RegisterActivityWithOptions directly, which
// is forbidden anywhere outside the generated worker.gen.go — registration
// must flow through the generated RegisterWorker(w, manifest) entrypoint.
func registerHandActivity(w worker) {
	var opts registerOptions
	w.RegisterActivityWithOptions(nil, opts)
}
