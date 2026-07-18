package security_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
)

// stubValidator is a test-only Validator: it returns a fixed principal for a
// known token and ErrUnauthenticated otherwise.
type stubValidator struct {
	good      string
	principal security.Principal
}

func (v stubValidator) ValidateAccessToken(_ context.Context, raw string) (security.Principal, error) {
	if raw == v.good {
		return v.principal, nil
	}
	return security.Principal{}, security.NewError(security.ErrUnauthenticated)
}

func TestMiddlewarePassesPrincipalThroughOnValidToken(t *testing.T) {
	want := security.Principal{Kind: security.PrincipalUser, Subject: "u-1", Roles: []string{"drive-phase"}}
	v := stubValidator{good: "tok", principal: want}

	var seen security.Principal
	var ok bool
	h := security.Middleware(v)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, ok = security.PrincipalFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !ok || seen.Subject != want.Subject {
		t.Fatalf("principal not propagated: ok=%v seen=%+v", ok, seen)
	}
}

func TestMiddleware401OnMissingAndBadToken(t *testing.T) {
	v := stubValidator{good: "tok"}
	reached := false
	h := security.Middleware(v)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))

	cases := map[string]string{
		"missing": "",
		"wrong":   "Bearer nope",
		"scheme":  "Basic tok",
		"empty":   "Bearer ",
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			reached = false
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			if header != "" {
				req.Header.Set("Authorization", header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if reached {
				t.Fatal("handler ran for unauthenticated request")
			}
		})
	}
}

func TestAuthorizeDefaultPDP(t *testing.T) {
	sec := security.New()
	ctx := context.Background()

	user := security.Principal{
		Kind:          security.PrincipalUser,
		Subject:       "u-1",
		Roles:         []string{"drive-phase"},
		Organizations: []security.Organization{{ID: "org-1", Name: "Acme"}},
	}
	res := security.ResourceRef{Kind: "project", ID: "p-1", Organization: "org-1"}

	// Role grants the verb within the member org → permit.
	if d, err := sec.Authorize(ctx, user, security.Action{Verb: "drive-phase"}, res); err != nil || !d.Permit {
		t.Fatalf("expected permit, got permit=%v err=%v", d.Permit, err)
	}
	// No role for the verb → deny.
	if d, _ := sec.Authorize(ctx, user, security.Action{Verb: "approve-artifact"}, res); d.Permit {
		t.Fatal("expected deny for ungranted verb")
	}
	// Resource in a non-member org → deny even with the role.
	other := security.ResourceRef{Kind: "project", ID: "p-2", Organization: "org-2"}
	if d, _ := sec.Authorize(ctx, user, security.Action{Verb: "drive-phase"}, other); d.Permit {
		t.Fatal("expected deny for cross-organization resource")
	}
	// Application principal → permit its action.
	app := security.Principal{Kind: security.PrincipalApplication, Subject: "svc-1"}
	if d, err := sec.Authorize(ctx, app, security.Action{Verb: "settle-cycle"}, security.ResourceRef{Kind: "cycle", ID: "c-1"}); err != nil || !d.Permit {
		t.Fatalf("expected application permit, got permit=%v err=%v", d.Permit, err)
	}
}

type unreachablePDP struct{}

func (unreachablePDP) Decide(context.Context, security.Principal, security.Action, security.ResourceRef) (bool, error) {
	return false, errors.New("engine down")
}

func TestAuthorizeFailsClosedOnEngineOutage(t *testing.T) {
	sec := security.New(security.WithPolicyDecisionPoint(unreachablePDP{}))
	d, err := sec.Authorize(context.Background(), security.Principal{}, security.Action{Verb: "x"}, security.ResourceRef{Kind: "y"})
	if d.Permit {
		t.Fatal("engine outage must not permit")
	}
	var se *security.Error
	if !errors.As(err, &se) || se.Kind != security.ErrPolicyUnavailable || !se.Retryable {
		t.Fatalf("want retryable ErrPolicyUnavailable, got %v", err)
	}
}

func TestVerifyWebhookSignature(t *testing.T) {
	secret := []byte("shh")
	sec := security.New(security.WithWebhookVerifier(
		security.NewHMACWebhookVerifier(map[security.WebhookChannel][]byte{"chan": secret}),
	))
	body := []byte(`{"event":"x"}`)
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	good := hex.EncodeToString(mac.Sum(nil))

	if err := sec.VerifyWebhookSignature(context.Background(), "chan", body,
		security.NewSignatureMaterial(map[string]string{"signature": good})); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	err := sec.VerifyWebhookSignature(context.Background(), "chan", body,
		security.NewSignatureMaterial(map[string]string{"signature": "deadbeef"}))
	var se *security.Error
	if !errors.As(err, &se) || se.Kind != security.ErrSignatureInvalid {
		t.Fatalf("want ErrSignatureInvalid, got %v", err)
	}

	// Unknown channel → transient, still fail-closed.
	err = sec.VerifyWebhookSignature(context.Background(), "nope", body,
		security.NewSignatureMaterial(map[string]string{"signature": good}))
	if !errors.As(err, &se) || se.Kind != security.ErrSigningKeyUnavailable || !se.Retryable {
		t.Fatalf("want retryable ErrSigningKeyUnavailable, got %v", err)
	}
}

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func TestObtainServiceIdentity(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sec := security.New(security.WithServiceIdentitySource(
		security.NewInProcessIdentitySource(fixedClock{t: now}, 10*time.Minute),
	))
	cred, err := sec.ObtainServiceIdentity(context.Background(), "downstream")
	if err != nil {
		t.Fatalf("mint failed: %v", err)
	}
	if cred.AttachableValue() == "" {
		t.Fatal("empty credential")
	}
	if cred.Principal.Kind != security.PrincipalApplication {
		t.Fatalf("service principal kind = %q, want application", cred.Principal.Kind)
	}
	if !cred.ExpiresAt.Equal(now.Add(10 * time.Minute)) {
		t.Fatalf("ExpiresAt = %v, want %v", cred.ExpiresAt, now.Add(10*time.Minute))
	}
}
