package github_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	gh "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-github/testinfra"
	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// github_pullrequest_test.go covers the PR-rail cleanup verbs added for design-rail
// branch-debris removal (ClosePullRequest, DeleteBranch) and the end-to-end 403
// rate-limit split, against the testinfra FakeGitHub.

const prRepo = "acme/widget"

// ---- ClosePullRequest ----

func TestClosePullRequest_ClosesOpenPR(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.On("GET", "/repos/acme/widget/pulls/7", gh.Response{Status: 200, Body: `{"number":7,"state":"open"}`})
	fake.On("PATCH", "/repos/acme/widget/pulls/7", gh.Response{Status: 200, Body: `{"number":7,"state":"closed"}`})

	c := newClient(t, fake.BaseURL())
	already, err := c.ClosePullRequest(context.Background(), prRepo, 7, "tok")
	if err != nil {
		t.Fatalf("ClosePullRequest: %v", err)
	}
	if already {
		t.Fatal("an open PR is not already closed")
	}
	last, _ := fake.LastRequest()
	if last.Method != "PATCH" || !strings.Contains(last.Body, `"state":"closed"`) {
		t.Fatalf("expected a PATCH with state=closed, got %s %s body=%q", last.Method, last.Path, last.Body)
	}
}

func TestClosePullRequest_AlreadyClosedIsIdempotent(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.On("GET", "/repos/acme/widget/pulls/7", gh.Response{Status: 200, Body: `{"number":7,"state":"closed"}`})

	c := newClient(t, fake.BaseURL())
	already, err := c.ClosePullRequest(context.Background(), prRepo, 7, "tok")
	if err != nil {
		t.Fatalf("ClosePullRequest: %v", err)
	}
	if !already {
		t.Fatal("a closed PR must report alreadyClosed==true")
	}
	// No PATCH should have been issued — the GET is the only request.
	for _, r := range fake.Requests() {
		if r.Method == "PATCH" {
			t.Fatalf("no PATCH expected for an already-closed PR, got %s %s", r.Method, r.Path)
		}
	}
}

func TestClosePullRequest_AlreadyMergedIsIdempotent(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.On("GET", "/repos/acme/widget/pulls/7", gh.Response{Status: 200, Body: `{"number":7,"state":"closed","merged":true}`})

	c := newClient(t, fake.BaseURL())
	already, err := c.ClosePullRequest(context.Background(), prRepo, 7, "tok")
	if err != nil {
		t.Fatalf("ClosePullRequest: %v", err)
	}
	if !already {
		t.Fatal("a merged PR must report alreadyClosed==true")
	}
}

// ---- DeleteBranch ----

func TestDeleteBranch_Deletes(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.On("DELETE", "/repos/acme/widget/git/refs/heads/session-1", gh.Response{Status: 204, Body: ""})

	c := newClient(t, fake.BaseURL())
	absent, err := c.DeleteBranch(context.Background(), prRepo, "session-1", "tok")
	if err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if absent {
		t.Fatal("a live branch is not already absent")
	}
	last, _ := fake.LastRequest()
	if last.Method != "DELETE" || last.Path != "/repos/acme/widget/git/refs/heads/session-1" {
		t.Fatalf("expected a DELETE on the ref, got %s %s", last.Method, last.Path)
	}
}

func TestDeleteBranch_AlreadyAbsentIsIdempotent(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.On("DELETE", "/repos/acme/widget/git/refs/heads/gone", gh.Response{Status: 404, Body: `{"message":"Reference does not exist"}`})

	c := newClient(t, fake.BaseURL())
	absent, err := c.DeleteBranch(context.Background(), prRepo, "gone", "tok")
	if err != nil {
		t.Fatalf("DeleteBranch on an absent ref must be idempotent success, got %v", err)
	}
	if !absent {
		t.Fatal("an absent branch must report alreadyAbsent==true")
	}
}

func TestDeleteBranch_PermissionErrorClassified(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	fake.On("DELETE", "/repos/acme/widget/git/refs/heads/protected", gh.Response{Status: 403, Body: `{"message":"Resource not accessible by integration"}`})

	c := newClient(t, fake.BaseURL())
	_, err := c.DeleteBranch(context.Background(), prRepo, "protected", "tok")
	if kindOf(err) != fwra.Auth {
		t.Fatalf("a permission 403 must classify as Auth, got %v", kindOf(err))
	}
}

// ---- 403 rate-limit split (end-to-end through do) ----

func TestRateLimit403ClassifiesRetryable(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	// A rate-limited 403 (body names the rate limit) on the token exchange must classify
	// as the retryable RateLimited kind, NOT terminal Auth — the F14 misclassification fix.
	fake.On("POST", "/app/installations/99/access_tokens", gh.Response{Status: 403, Body: `{"message":"API rate limit exceeded for installation"}`})

	c := newClient(t, fake.BaseURL())
	_, _, err := c.MintInstallationToken(context.Background(), 99)
	if kindOf(err) != fwra.RateLimited {
		t.Fatalf("a rate-limit 403 must classify as RateLimited, got %v", kindOf(err))
	}
	// preserve-body assertion (F14): the classified error carries the GitHub message.
	var fe *fwra.Error
	if errors.As(err, &fe) && !strings.Contains(fe.Detail, "rate limit exceeded") {
		t.Fatalf("rate-limit error must preserve the response body, got %q", fe.Detail)
	}
}

func TestPermission403StaysAuth(t *testing.T) {
	fake := gh.Start()
	defer fake.Close()
	// A genuine permission 403 (no rate-limit signal) must remain terminal Auth.
	fake.On("POST", "/app/installations/99/access_tokens", gh.Response{Status: 403, Body: `{"message":"revoked"}`})

	c := newClient(t, fake.BaseURL())
	if _, _, err := c.MintInstallationToken(context.Background(), 99); kindOf(err) != fwra.Auth {
		t.Fatalf("a permission 403 must classify as Auth, got %v", kindOf(err))
	}
}
