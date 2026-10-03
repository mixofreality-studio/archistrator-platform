package badmgr

import (
	"os"
	"testing"

	"example.com/other/notallowed"
)

// manager_hooks_test.go is an ALLOWED file name, but it declares the
// in-package `package badmgr` instead of `package badmgr_test`, so it is
// flagged test-package-not-external — not passed as "named correctly". It also
// imports a package outside the hooks allowlist (neither its own package,
// stdlib, testinfra nor scenariohost), so it is flagged hooks-import-not-allowed.
func Step_Helper_1(t *testing.T) int {
	t.Helper()
	_ = os.Getenv("X")
	return helper() + notallowed.One()
}
