package github

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	fwra "github.com/mixofreality-studio/archistrator-platform/framework-go/resourceaccess"
)

// github_ratelimit_internal_test.go unit-tests the 403 rate-limit split at the seam
// where it is made (isRateLimited / rateLimitError, package-internal). The end-to-end
// behavior — that do() short-circuits a rate-limited 403 to a retryable error while a
// permission 403 stays Auth — is covered against the fake in github_pullrequest_test.go.

func TestIsRateLimited(t *testing.T) {
	cases := []struct {
		name   string
		status int
		header http.Header
		body   string
		want   bool
	}{
		{"remaining-zero-header", 403, http.Header{"X-Ratelimit-Remaining": {"0"}}, "", true},
		{"retry-after-header", 403, http.Header{"Retry-After": {"60"}}, "", true},
		{"body-primary-rate-limit", 403, http.Header{}, `{"message":"API rate limit exceeded for installation"}`, true},
		{"body-secondary-rate-limit", 403, http.Header{}, `{"message":"You have exceeded a secondary rate limit"}`, true},
		{"remaining-nonzero-permission", 403, http.Header{"X-Ratelimit-Remaining": {"4999"}}, `{"message":"Resource not accessible by integration"}`, false},
		{"plain-permission-403", 403, http.Header{}, `{"message":"revoked"}`, false},
		{"not-a-403", 429, http.Header{"X-Ratelimit-Remaining": {"0"}}, "rate limit", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRateLimited(tc.status, tc.header, []byte(tc.body)); got != tc.want {
				t.Fatalf("isRateLimited(%d) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestRateLimitError_TypedAndPreservesBody(t *testing.T) {
	body := `{"message":"You have exceeded a secondary rate limit"}`
	err := rateLimitError(403, http.Header{"Retry-After": {"30"}}, []byte(body))
	if err == nil {
		t.Fatal("expected a rate-limit error")
	}
	var fe *fwra.Error
	if !errors.As(err, &fe) {
		t.Fatalf("expected *fwra.Error, got %T", err)
	}
	if fe.Kind != fwra.RateLimited {
		t.Fatalf("rate-limit kind = %v, want RateLimited", fe.Kind)
	}
	if !fe.Retryable {
		t.Fatal("a rate-limit error must be retryable")
	}
	// The body is preserved in the detail (app ledger F14) so the throttle is diagnosable.
	if !strings.Contains(fe.Detail, "secondary rate limit") || !strings.Contains(fe.Detail, "Retry-After: 30") {
		t.Fatalf("rate-limit detail must preserve the body and Retry-After, got %q", fe.Detail)
	}
	// A non-rate-limited 403 yields no rate-limit error (falls through to ClassifyStatus).
	if rateLimitError(403, http.Header{}, []byte(`{"message":"revoked"}`)) != nil {
		t.Fatal("a permission 403 must not be a rate-limit error")
	}
}
