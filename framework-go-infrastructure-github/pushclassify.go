package github

// pushclassify.go is the ONE classifier for a plain (never --force) push's
// rejection, shared by GitStore.CommitSubtree and GitBlobStore.StoreOutput so
// both apply the same git vocabulary. It separates two rejections that git
// reports through the same per-ref report-status channel — go-git formats every
// one as `command error on <ref>: <reason>` (packp/report_status.go) — and that
// mean opposite things:
//
//   - a CAS LOSS: a concurrent writer advanced the ref; re-read and re-apply
//     converges (fwra.Conflict wrapping ErrRefCASLost).
//   - a CHECKOUT REFUSAL: the target is a non-bare repo with the branch checked
//     out (receive.denyCurrentBranch=updateInstead, or refuse) and its working
//     tree/index is not clean. git refuses EVERY push to that branch whatever its
//     base, so a retry can never clear it (fwra.Infrastructure, Retryable
//     explicitly false — Infrastructure is retryable by default, and a retryable
//     refusal would hand a permanent fault to the activity retry policy).
//
// FIX-002: the previous `command error on` catch-all made every checkout refusal
// a CAS loss, and the app burnt its full conflict bound on a wedged repo.

import (
	"errors"
	"fmt"
	"strings"

	gogit "github.com/go-git/go-git/v5"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// checkoutRefusalReasons are git receive-pack's reasons (lower-cased) for
// refusing a push into a checked-out branch. "branch is currently checked out" is
// the report-status reason under denyCurrentBranch=refuse; the rest come from
// push-to-checkout under updateInstead.
var checkoutRefusalReasons = []string{
	"working directory has staged changes",
	"working directory has unstaged changes",
	"up-to-date check failed",
	"could not update working tree to new head",
	"refusing to update checked out branch",
	"branch is currently checked out",
}

// casLossReasons are the rejections that mean the ref moved under us.
var casLossReasons = []string{
	"non-fast-forward",
	"fetch first",
	"stale info",
	"cannot lock ref",
	"already exists", // first-writer race creating the branch
	"failed to update ref",
}

// isCheckoutRefusal reports whether a push error is the target's checked-out
// working tree refusing the update (permanent until cleaned by hand).
func isCheckoutRefusal(err error) bool {
	return containsAny(strings.ToLower(err.Error()), checkoutRefusalReasons)
}

// isNonFastForward reports whether a push error is a genuine CAS loss (a
// concurrent writer advanced the ref). A checkout refusal is never one, even when
// git reports it alongside "failed to update ref".
func isNonFastForward(err error) bool {
	if isCheckoutRefusal(err) {
		return false
	}
	if errors.Is(err, gogit.ErrNonFastForwardUpdate) {
		return true
	}
	return containsAny(strings.ToLower(err.Error()), casLossReasons)
}

func containsAny(msg string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(msg, n) {
			return true
		}
	}
	return false
}

// classifyPushError maps a plain push's result onto the RA error model; op names
// the operation ("github.CommitSubtree"). nil / already-up-to-date → nil.
func classifyPushError(err error, op string) error {
	if err == nil || errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return nil
	}
	if isCheckoutRefusal(err) {
		rae := fwra.Wrap(fwra.Infrastructure, err, op+
			": push refused by the target repo's dirty checkout (the pushed branch is checked out and its"+
			" working tree or index is not clean); this is NOT a CAS loss and retrying cannot clear it."+
			" Recover once, by hand: inspect `git -C <repo> status` / `git diff --cached`, then"+
			" `git -C <repo> reset --mixed && git -C <repo> checkout -- .`")
		rae.Retryable = false
		return rae
	}
	if isNonFastForward(err) {
		return fwra.Wrap(fwra.Conflict, fmt.Errorf("%w: %w", ErrRefCASLost, err),
			op+": concurrent ref advance (non-fast-forward)")
	}
	return ClassifyGitError(err, op+": push")
}
