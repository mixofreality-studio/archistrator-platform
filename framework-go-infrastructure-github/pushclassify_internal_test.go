package github

import (
	"errors"
	"testing"

	gogit "github.com/go-git/go-git/v5"
)

// TestPushRejectionClassifier pins the shared push classifier over git's REAL
// receive-pack reasons, both bare and wrapped the way go-git v5 reports every
// per-ref rejection (`command error on <ref>: <reason>`, packp/report_status.go).
// The pre-FIX-002 `command error on` catch-all made each checkout refusal a CAS
// loss.
func TestPushRejectionClassifier(t *testing.T) {
	cases := []struct {
		reason  string
		casLoss bool
		refusal bool
	}{
		{"non-fast-forward", true, false},
		{"fetch first", true, false},
		{"stale info", true, false},
		{"cannot lock ref 'refs/heads/main': is at 946917a but expected e2999f9c", true, false},
		{"failed to update ref", true, false},
		{"reference already exists", true, false},
		{"Working directory has staged changes", false, true},
		{"Working directory has unstaged changes", false, true},
		{"Up-to-date check failed", false, true},
		{"Could not update working tree to new HEAD", false, true},
		{"refusing to update checked out branch: refs/heads/main", false, true},
		{"branch is currently checked out", false, true},
		{"failed to update ref; Working directory has staged changes", false, true},
		{"pre-receive hook declined", false, false},
		{"deny updating a hidden ref", false, false},
	}
	for _, c := range cases {
		for _, msg := range []string{c.reason, "command error on refs/heads/main: " + c.reason} {
			err := errors.New(msg)
			if got := isNonFastForward(err); got != c.casLoss {
				t.Errorf("isNonFastForward(%q) = %v, want %v", msg, got, c.casLoss)
			}
			if got := isCheckoutRefusal(err); got != c.refusal {
				t.Errorf("isCheckoutRefusal(%q) = %v, want %v", msg, got, c.refusal)
			}
		}
	}
	if !isNonFastForward(gogit.ErrNonFastForwardUpdate) {
		t.Error("go-git's client-side ErrNonFastForwardUpdate is a CAS loss")
	}
}
