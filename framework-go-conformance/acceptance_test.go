//go:build conformance

package conformance

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	scenariohost "github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
)

// Acceptance runs (FPC plan 4) drive one launched app as several actors at once: each request names
// its own principal in X-Dev-Principal, and the launch honours the header only for a subject its
// allowlist holds. The browser runtime and the generated acceptance specs rely on exactly this, so
// it is pinned here against the real composed app and the real generated edge.

// rawOpenList posts the open-list route with the header as given ("" sends none) and returns the
// status and body: the request a browser runtime's fetch makes.
func rawOpenList(ctx context.Context, t *testing.T, baseURL, header string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/list/open-list", strings.NewReader(`{"owner":"alice","view":"all"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if header != "" {
		req.Header.Set("X-Dev-Principal", header)
	}
	res, err := http.DefaultClient.Do(req) // #nosec G107 -- the app this test launched
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(body)
}

// A per-request X-Dev-Principal is honoured only for a subject the launch allowlisted: the allowlisted
// delegated agent reaches the member's list it acts for, a subject off the list is a 401 whatever the
// header claims, and a request without the header is served as the process principal.
func TestDevPrincipals_PerRequestHeaderOnlyWhenAllowlisted(t *testing.T) {
	h, dir := launchBuilt(t)
	alice := scenariohost.Principal{Subject: "alice"}
	agent := scenariohost.Principal{Subject: "agent-1", Roles: []string{"agent"}, OnBehalfOf: "alice", Intents: []string{"read"}}
	app := h.LaunchDevstack(t, dir, alice, agent)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	const wording = "alice-owns-this-todo"
	asAlice := scenariohost.MCPSession(t, app, alice)
	if _, err := asAlice.Call(ctx, "listAddTodo", `{"owner":"alice","wording":"`+wording+`","requestId":"r1"}`); err != nil {
		t.Fatalf("alice adding a todo: %v\n%s", err, app.Log())
	}

	if status, body := rawOpenList(ctx, t, app.BaseURL, "agent-1"); status != http.StatusOK || !strings.Contains(body, wording) {
		t.Errorf("X-Dev-Principal agent-1 (allowlisted): status %d body %q; want 200 and alice's list", status, body)
	}
	if status, body := rawOpenList(ctx, t, app.BaseURL, "mallory"); status != http.StatusUnauthorized || !strings.Contains(body, "dev principal not allowed") {
		t.Errorf("X-Dev-Principal mallory (not allowlisted): status %d body %q; want 401 \"dev principal not allowed\"", status, body)
	}
	if status, body := rawOpenList(ctx, t, app.BaseURL, ""); status != http.StatusOK || !strings.Contains(body, wording) {
		t.Errorf("no header: status %d body %q; want 200, served as the process principal alice", status, body)
	}
}
