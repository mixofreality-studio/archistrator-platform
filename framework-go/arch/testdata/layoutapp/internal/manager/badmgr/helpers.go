// Package badmgr is a DIRTY Manager-layer fixture exercising every violation
// rule the checker enforces. helpers.go itself is not the impl file
// (badmgrmanager.go), carries no workflow func, and is not a test file — it
// has no allowed reason to exist, so it is flagged file-not-allowed.
package badmgr

func helper() int { return 1 }
