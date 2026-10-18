package pushgate_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/pushgate"
)

const fixtures = "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/pushgate/testdata/"

// TestPushgate_SelfModule applies the gate to this satellite's own production
// code: GitStore.CommitSubtree and GitBlobStore.StoreOutput push only inside
// withLocalRepoLock, whose local arm is PushLocal (its remote arm needs no lock —
// a GitHub host has no checked-out branch to wedge).
func TestPushgate_SelfModule(t *testing.T) {
	pushgate.Check(t, pushgate.Spec{
		ModuleRoot: "..",
		Patterns:   []string{"./..."},
		Wrappers: []pushgate.Func{
			pushgate.PushLocalFunc,
			"github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github.withLocalRepoLock",
		},
	})
}

// TestPushgate_WrappedFixtureIsClean — pushes under PushLocal (directly, through
// unexported helpers, through a git runner) and an exempt GitHub-origin push
// produce no violation.
func TestPushgate_WrappedFixtureIsClean(t *testing.T) {
	vs, err := pushgate.Find(pushgate.Spec{
		ModuleRoot: ".",
		Patterns:   []string{"./testdata/wrapped"},
		Wrappers:   []pushgate.Func{pushgate.PushLocalFunc},
		Exempt:     []pushgate.Func{fixtures + "wrapped.PublishToOrigin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		t.Errorf("unexpected violation: %s", v)
	}
}

// TestPushgate_BypassFixtureFails — the gate goes red on every way of pushing
// outside PushLocal, and on a stale exemption.
func TestPushgate_BypassFixtureFails(t *testing.T) {
	vs, err := pushgate.Find(pushgate.Spec{
		ModuleRoot: ".",
		Patterns:   []string{"./testdata/bypass"},
		Wrappers:   []pushgate.Func{pushgate.PushLocalFunc},
		Exempt:     []pushgate.Func{fixtures + "bypass.Gone"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range vs {
		got = append(got, strings.TrimPrefix(v.Func, fixtures+"bypass."))
	}
	want := []string{"DirectPush", "RemotePush", "MethodValue", "ExecPush", "RunnerPush", "helper", "NotTheWrapper", "ExportedHelper", "Gone"}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("gate missed the bypass in %s; violations: %v", w, vs)
		}
	}
	if len(vs) != len(want) {
		t.Errorf("want %d violations, got %d: %v", len(want), len(vs), vs)
	}
}
