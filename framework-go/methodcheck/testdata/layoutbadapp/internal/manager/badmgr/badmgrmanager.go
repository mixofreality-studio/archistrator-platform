// Package badmgr is a Manager fixture that passes the arch layer rules but
// deliberately violates the FILE-LAYOUT gate: helpers.go is a handwritten file
// outside the closed allowed set. It exists solely so methodcheck's tests can
// prove a layout violation surfaces through methodcheck.Check (the default-on
// arch.CheckFileLayout wiring).
package badmgr

// Run is the manager's single operation.
func Run() error { return nil }
