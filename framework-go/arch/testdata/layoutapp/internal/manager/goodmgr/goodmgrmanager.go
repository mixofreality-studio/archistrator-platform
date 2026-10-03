// Package goodmgr is a CLEAN Manager-layer fixture: the impl file carries the
// contract type + non-workflow method, deploy.go carries the single workflow
// func in its own per-workflow file, the two test files are the generated
// scenarios file and its hooks file (both black-box, package goodmgr_test;
// the hooks file imports only its own package + stdlib), and worker.gen.go
// is exempt as generated. The checker must produce zero violations here.
package goodmgr

// Manager is exported so the black-box (goodmgr_test) scenario tests can
// construct it.
type Manager struct{}

func (w *Manager) Do() error { return nil }
