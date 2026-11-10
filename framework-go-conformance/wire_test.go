//go:build conformance

package conformance

import (
	"strings"
	"testing"

	scenariohost "github.com/mixofreality-studio/archistrator-platform/framework-go-scenariohost"
)

// Machine clients over the wire against the built fixture (Plan 3 Task 3.3): the app is the composed
// server the launcher runs, a session or a REST client is one principal's request stream, and the
// policy answering it is the one generated into the composition root (Task 3.4). Until that policy
// lands these fail, which is the point of writing them first.

// The generated policy is the real one: an owner reads their own list, another principal's request
// for it reaches the Manager, which refuses (accessgranting: notOwner), and a header naming a subject
// the launch did not allowlist never authenticates.
func TestMCP_OverWire_UsesRealPolicy(t *testing.T) {
	h, dir := launchBuilt(t)
	alice, mallory := scenariohost.Principal{Subject: "alice"}, scenariohost.Principal{Subject: "mallory"}
	app := h.LaunchDevstack(t, dir, alice, mallory)

	asAlice := scenariohost.MCPSession(t, app, alice)
	if _, err := asAlice.Call(t.Context(), "listOpenList", `{"owner":"alice","view":"all"}`); err != nil {
		t.Fatalf("alice reading her own list: %v\n%s", err, app.Log())
	}

	asMallory := scenariohost.MCPSession(t, app, mallory)
	_, err := asMallory.Call(t.Context(), "listOpenList", `{"owner":"alice","view":"all"}`)
	if err == nil || !strings.Contains(err.Error(), "notOwner") {
		t.Errorf("mallory reading alice's list: want the Manager's notOwner refusal, got %v", err)
	}

	// A session as a subject the launch never allowlisted is refused at the edge: the handshake
	// itself fails with the middleware's 401 (the MCP client spells it "Unauthorized").
	stranger := scenariohost.Principal{Subject: "stranger"}
	if _, err := scenariohost.TryMCPSession(t, app, stranger); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("a subject off the allowlist must be refused with 401, got %v", err)
	}
	rest := scenariohost.RESTClient(t, app, stranger, map[string]scenariohost.Route{"listOpenList": {Verb: "POST", Path: "/api/v1/list/open-list"}})
	if _, err := rest.Call(t.Context(), "listOpenList", `{"owner":"stranger","view":"all"}`); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("a REST request naming a subject off the allowlist must be refused with 401, got %v", err)
	}
}

// The same refusal on both transports satisfies the plan's one error name: REST carries the wire code
// and the status, MCP the Manager's kind, and errorHasCode accepts either for `NotFound`.
func TestREST_OverWire_ErrorEnvelopeMatchesContract(t *testing.T) {
	h, dir := launchBuilt(t)
	alice := scenariohost.Principal{Subject: "alice"}
	app := h.LaunchDevstack(t, dir, alice)

	routes := map[string]scenariohost.Route{
		"listRewordTodo": {Verb: "POST", Path: "/api/v1/list/reword-todo"},
	}
	const args = `{"owner":"alice","todoId":"no-such-todo","wording":"x"}`
	rest := scenariohost.RESTClient(t, app, alice, routes)
	_, err := rest.Call(t.Context(), "listRewordTodo", args)
	if err == nil || !strings.Contains(err.Error(), "not_found") || !strings.Contains(err.Error(), "404") {
		t.Errorf("REST: want status 404 and the wire code not_found, got %v", err)
	}

	mcp := scenariohost.MCPSession(t, app, alice)
	_, err = mcp.Call(t.Context(), "listRewordTodo", args)
	if err == nil || !strings.Contains(err.Error(), "NotFound") {
		t.Errorf("MCP: want the Manager's NotFound kind in the error text, got %v", err)
	}
}
