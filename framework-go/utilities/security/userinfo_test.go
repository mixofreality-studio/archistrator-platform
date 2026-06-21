package security_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// TestUserInfoHandlerReturnsPrincipalClaims: mounted behind Middleware, the
// handler returns the validated principal's identity claims as JSON (the SPA's
// session probe — GTD parity).
func TestUserInfoHandlerReturnsPrincipalClaims(t *testing.T) {
	want := security.SecurityPrincipal{
		Kind:          security.PrincipalUser,
		Subject:       "u-1",
		Username:      "ada",
		Email:         "ada@example.com",
		Name:          "Ada Lovelace",
		Roles:         []string{"drive-phase"},
		Organizations: []security.Organization{{ID: "org-1", Name: "acme"}},
	}
	v := stubValidator{good: "tok", principal: want}
	h := security.Middleware(v)(http.HandlerFunc(security.UserInfoHandler))

	req := httptest.NewRequest(http.MethodGet, "/api/userinfo", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got security.UserInfoResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Sub != want.Subject || got.PreferredUsername != want.Username ||
		got.Email != want.Email || got.Name != want.Name || got.Kind != string(want.Kind) {
		t.Fatalf("identity mismatch: %+v", got)
	}
	if len(got.Roles) != 1 || got.Roles[0] != "drive-phase" {
		t.Fatalf("roles = %v", got.Roles)
	}
	if len(got.Organizations) != 1 || got.Organizations[0].ID != "org-1" || got.Organizations[0].Name != "acme" {
		t.Fatalf("organizations = %v", got.Organizations)
	}
}

// TestUserInfoHandlerRejectsMissingToken: an unauthenticated request never gets
// a principal on the context, so Middleware rejects it with 401 before the
// handler runs — the SPA reloads on 401 to trigger the edge OIDC redirect.
func TestUserInfoHandlerRejectsMissingToken(t *testing.T) {
	v := stubValidator{good: "tok"}
	h := security.Middleware(v)(http.HandlerFunc(security.UserInfoHandler))

	req := httptest.NewRequest(http.MethodGet, "/api/userinfo", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// TestUserInfoHandlerFailsClosedWithoutMiddleware: mounted OUTSIDE the auth
// boundary (no principal on context), the handler itself returns 401 rather than
// leaking an anonymous response.
func TestUserInfoHandlerFailsClosedWithoutMiddleware(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/userinfo", nil)
	rec := httptest.NewRecorder()
	security.UserInfoHandler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
