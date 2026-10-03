// Hooks for goodmgr scenario tests. Generated ONCE by testgen; owned by the construction agent.
package goodmgr_test

import (
	"testing"

	"example.com/layoutapp/internal/manager/goodmgr"
)

// manager_hooks_test.go is the agent-owned hooks file: black-box (package
// goodmgr_test) and importing only the component's own package and stdlib,
// so it passes both test-package-not-external and hooks-import-not-allowed.
func Step_Do_1(t *testing.T, m *goodmgr.Manager) error {
	t.Helper()
	return m.Do()
}
