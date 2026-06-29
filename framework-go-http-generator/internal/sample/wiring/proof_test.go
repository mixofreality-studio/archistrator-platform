// Package wiring's proof test exercises the GENERATED middleware + server
// (middleware.gen.go / server.gen.go) against the REAL platform security package.
// It is not generated — it is the runtime proof that the emitted layer composes a
// working HTTP surface: dev-mode principal injection, the unauthenticated health
// probes, and the /api/v1/ auth boundary (deny with no validator, admit a
// validated token, and a principal readable downstream via security.PrincipalFrom).
package wiring

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	security "github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// stubRegistrar is a stand-in for a generated component Handler: it satisfies the
// generated Registrar seam and mounts one authenticated route that echoes whether
// the auth middleware put a principal on the context.
type stubRegistrar struct{}

func (stubRegistrar) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		if p, ok := security.PrincipalFrom(r.Context()); ok {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(p.Subject))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
}

// fakeValidator accepts exactly one token and maps it to a fixed principal.
type fakeValidator struct{}

func (fakeValidator) ValidateAccessToken(_ context.Context, raw string) (security.SecurityPrincipal, error) {
	if raw == "good" {
		return security.SecurityPrincipal{Kind: security.PrincipalUser, Subject: "u-real"}, nil
	}
	return security.SecurityPrincipal{}, security.NewError(security.ErrUnauthenticated)
}

func TestHealthIsUnauthenticated(t *testing.T) {
	srv := httptest.NewServer(NewServer(DevConfig{}, nil, stubRegistrar{}))
	defer srv.Close()

	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s = %d, want 200 (health must be outside the auth boundary)", path, resp.StatusCode)
		}
	}
}

func TestNilValidatorDeniesAPI(t *testing.T) {
	srv := httptest.NewServer(NewServer(DevConfig{}, nil, stubRegistrar{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/ping")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("/api/v1/ping with no validator = %d, want 401 (fail-closed)", resp.StatusCode)
	}
}

func TestDevModeInjectsPrincipal(t *testing.T) {
	dev := DevConfig{Enabled: true, Principal: security.SecurityPrincipal{Subject: "u-dev"}}
	srv := httptest.NewServer(NewServer(dev, nil, stubRegistrar{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/ping")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/v1/ping in dev mode = %d, want 200", resp.StatusCode)
	}
	buf := make([]byte, 16)
	n, _ := resp.Body.Read(buf)
	if got := string(buf[:n]); got != "u-dev" {
		t.Errorf("injected principal subject = %q, want %q", got, "u-dev")
	}
}

func TestValidatedTokenAdmitsAPI(t *testing.T) {
	srv := httptest.NewServer(NewServer(DevConfig{}, fakeValidator{}, stubRegistrar{}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer good")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/v1/ping with valid token = %d, want 200", resp.StatusCode)
	}
	buf := make([]byte, 16)
	n, _ := resp.Body.Read(buf)
	if got := string(buf[:n]); got != "u-real" {
		t.Errorf("validated principal subject = %q, want %q", got, "u-real")
	}
}

func TestBadTokenRejected(t *testing.T) {
	srv := httptest.NewServer(NewServer(DevConfig{}, fakeValidator{}, stubRegistrar{}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer nope")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("/api/v1/ping with bad token = %d, want 401", resp.StatusCode)
	}
}
