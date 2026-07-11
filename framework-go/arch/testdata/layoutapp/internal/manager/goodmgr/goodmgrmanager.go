// Package goodmgr is a CLEAN Manager-layer fixture: the impl file carries the
// contract type + non-workflow method, deploy.go carries the single workflow
// func in its own per-workflow file, manager_test.go is correctly named, and
// worker.gen.go is exempt as generated. The checker must produce zero
// violations here.
package goodmgr

type wfs struct{}

func (w *wfs) Do() error { return nil }
