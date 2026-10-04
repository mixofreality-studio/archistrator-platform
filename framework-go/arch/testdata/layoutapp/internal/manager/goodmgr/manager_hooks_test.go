// Hooks for goodmgr scenario tests. Generated ONCE by testgen; owned by the construction agent.
package goodmgr_test

import (
	"testing"

	"example.com/layoutapp/internal/engine/collabeng"
	"example.com/layoutapp/internal/manager/goodmgr"
)

// manager_hooks_test.go is the agent-owned hooks file: black-box (package
// goodmgr_test), importing the component's own package, stdlib and a real
// collaborator's public package (DCT §12 B2), so it passes both
// test-package-not-external and hooks-import-not-allowed.
func Step_Do_1(t *testing.T, m *goodmgr.Manager) error {
	t.Helper()
	if _, err := (&collabeng.Engine{}).Price(1); err != nil {
		return err
	}
	return m.Do()
}
