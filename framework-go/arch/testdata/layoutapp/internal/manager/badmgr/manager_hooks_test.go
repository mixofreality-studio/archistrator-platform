package badmgr

import (
	"os"
	"testing"

	"example.com/other/notallowed"

	// A real collaborator's public package is allowed (DCT §12 B2) …
	_ "example.com/layoutapp/internal/engine/collabeng"
	// … its internal/ sub-package, its generated fake (not named in the
	// module's HooksImportAllowlist) and an unclassified module package are not.
	_ "example.com/layoutapp/internal/engine/collabeng/fake"
	_ "example.com/layoutapp/internal/engine/collabeng/internal/store"
	_ "example.com/layoutapp/internal/workflow"
)

// manager_hooks_test.go is an ALLOWED file name, but it declares the
// in-package `package badmgr` instead of `package badmgr_test`, so it is
// flagged test-package-not-external — not passed as "named correctly". It also
// imports packages outside the hooks allowlist (neither its own package,
// stdlib, testinfra, scenariohost nor a component's public package), so each
// is flagged hooks-import-not-allowed.
func Step_Helper_1(t *testing.T) int {
	t.Helper()
	_ = os.Getenv("X")
	return helper() + notallowed.One()
}
