package httpgen_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/internal/sample/wiring"
	security "github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// whoAmI echoes the principal the generated middleware put on the context:
// subject, roles, and whether the "delegations" claim rode along.
type whoAmI struct{}

func (whoAmI) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/whoami", func(w http.ResponseWriter, r *http.Request) {
		p, ok := security.PrincipalFrom(r.Context())
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, hasClaim := p.Claims["delegations"]
		claims := "noclaims"
		if hasClaim {
			claims = "claims"
		}
		_, _ = w.Write([]byte(p.Subject + "|" + strings.Join(p.Roles, ",") + "|" + claims))
	})
}

func whoamiAs(t *testing.T, dev wiring.DevConfig, header string, set bool) (int, string) {
	t.Helper()
	srv := httptest.NewServer(wiring.NewServer(dev, nil, whoAmI{}))
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/whoami", nil)
	if err != nil {
		t.Fatal(err)
	}
	if set {
		req.Header.Set(wiring.DevPrincipalHeader, header)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(body))
}

func TestAuthMiddleware_DevPrincipalHeaderFromAllowlist(t *testing.T) {
	if wiring.DevPrincipalHeader != "X-Dev-Principal" {
		t.Fatalf("DevPrincipalHeader = %q", wiring.DevPrincipalHeader)
	}
	dev := wiring.DevConfig{
		Enabled:   true,
		Principal: security.Principal{Subject: "process"},
		Principals: map[string]security.Principal{
			"alice": {Subject: "alice", Roles: []string{"owner", "member"}, Claims: map[string]any{"delegations": []any{}}},
		},
	}

	if code, body := whoamiAs(t, dev, "", false); code != http.StatusOK || body != "process||noclaims" {
		t.Errorf("no header: %d %q, want 200 process||noclaims", code, body)
	}
	if code, body := whoamiAs(t, dev, "alice", true); code != http.StatusOK || body != "alice|owner,member|claims" {
		t.Errorf("alice on allowlist: %d %q, want 200 alice|owner,member|claims", code, body)
	}
	code, body := whoamiAs(t, dev, "mallory", true)
	if code != http.StatusUnauthorized || !strings.Contains(body, "dev principal not allowed") {
		t.Errorf("mallory: %d %q, want 401 containing %q", code, body, "dev principal not allowed")
	}
	// Enabled=false ignores the header: the nil validator denies, never admits as alice.
	off := dev
	off.Enabled = false
	if code, _ := whoamiAs(t, off, "alice", true); code != http.StatusUnauthorized {
		t.Errorf("Enabled=false with header: %d, want 401 (header ignored, nil validator denies)", code)
	}
}
