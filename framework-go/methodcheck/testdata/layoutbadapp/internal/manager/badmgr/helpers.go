package badmgr

// helpers.go is the DELIBERATE file-layout violation: a handwritten file that
// is neither the impl file (badmgrmanager.go), a per-workflow file, nor the
// test file — CheckFileLayout must flag it "file-not-allowed".

func helper() int { return 1 }

var _ = helper
