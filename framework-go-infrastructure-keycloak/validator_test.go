package keycloak_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	keycloak "github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-keycloak"
	"github.com/mixofreality-studio/archistrator-platform/framework-go/utilities/security"
	"github.com/golang-jwt/jwt/v5"
)

const (
	testKID    = "test-key"
	testIssuer = "https://keycloak.example.com/realms/aiarch"
)

// jwksServer serves a single-key JWKS for the given RSA public key and returns
// its URL. The key id is testKID.
func jwksServer(t *testing.T, pub *rsa.PublicKey) string {
	t.Helper()
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	body := fmt.Sprintf(`{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":%q,"n":%q,"e":%q}]}`, testKID, n, e)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func signToken(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = testKID
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func newValidator(t *testing.T, jwksURL string) security.Validator {
	t.Helper()
	v, err := keycloak.NewValidator(context.Background(), keycloak.Config{
		JWKSURL: jwksURL,
		Issuer:  testIssuer,
		Leeway:  3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewValidator: %v", err)
	}
	return v
}

func TestValidateUserAccessToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	v := newValidator(t, jwksServer(t, &key.PublicKey))

	raw := signToken(t, key, jwt.MapClaims{
		"iss":                testIssuer,
		"sub":                "user-123",
		"exp":                time.Now().Add(time.Hour).Unix(),
		"preferred_username": "amira",
		"email":              "amira@example.com",
		"name":               "Amira A.",
		"realm_access":       map[string]any{"roles": []any{"drive-phase", "approve-artifact"}},
		"organization":       map[string]any{"Acme": map[string]any{"id": "org-1"}},
	})

	p, err := v.ValidateAccessToken(context.Background(), raw)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if p.Kind != security.PrincipalUser {
		t.Errorf("Kind = %q, want user", p.Kind)
	}
	if p.Subject != "user-123" || p.Username != "amira" || p.Email != "amira@example.com" || p.Name != "Amira A." {
		t.Errorf("identity fields wrong: %+v", p)
	}
	if p.Issuer != testIssuer {
		t.Errorf("Issuer = %q", p.Issuer)
	}
	if !p.HasRole("drive-phase") || !p.HasRole("approve-artifact") {
		t.Errorf("roles = %v", p.Roles)
	}
	if !p.IsMemberOf("org-1") || !p.IsMemberOf("Acme") {
		t.Errorf("organizations = %v", p.Organizations)
	}
}

func TestValidateApplicationToken(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	v := newValidator(t, jwksServer(t, &key.PublicKey))

	raw := signToken(t, key, jwt.MapClaims{
		"iss":                testIssuer,
		"sub":                "svc-account-uuid",
		"exp":                time.Now().Add(time.Hour).Unix(),
		"preferred_username": "service-account-scheduler",
		"azp":                "scheduler",
	})
	p, err := v.ValidateAccessToken(context.Background(), raw)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if p.Kind != security.PrincipalApplication {
		t.Errorf("Kind = %q, want application", p.Kind)
	}
}

func TestRejectsBadToken(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	v := newValidator(t, jwksServer(t, &key.PublicKey))

	cases := map[string]jwt.MapClaims{
		"expired":      {"iss": testIssuer, "sub": "u", "exp": time.Now().Add(-time.Hour).Unix()},
		"wrong issuer": {"iss": "https://evil.example.com/realms/x", "sub": "u", "exp": time.Now().Add(time.Hour).Unix()},
		"no exp":       {"iss": testIssuer, "sub": "u"},
	}
	for name, claims := range cases {
		t.Run(name, func(t *testing.T) {
			raw := signToken(t, key, claims)
			_, err := v.ValidateAccessToken(context.Background(), raw)
			assertUnauthenticated(t, err)
		})
	}

	t.Run("wrong signing key", func(t *testing.T) {
		raw := signToken(t, other, jwt.MapClaims{"iss": testIssuer, "sub": "u", "exp": time.Now().Add(time.Hour).Unix()})
		_, err := v.ValidateAccessToken(context.Background(), raw)
		assertUnauthenticated(t, err)
	})

	t.Run("garbage", func(t *testing.T) {
		_, err := v.ValidateAccessToken(context.Background(), "not.a.jwt")
		assertUnauthenticated(t, err)
	})
}

func TestVerifiedTokenWithoutSubjectIsPrincipalUnknown(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	v := newValidator(t, jwksServer(t, &key.PublicKey))
	raw := signToken(t, key, jwt.MapClaims{"iss": testIssuer, "exp": time.Now().Add(time.Hour).Unix()})
	_, err := v.ValidateAccessToken(context.Background(), raw)
	var se *security.SecurityError
	if !errors.As(err, &se) || se.Kind != security.ErrPrincipalUnknown {
		t.Fatalf("want ErrPrincipalUnknown, got %v", err)
	}
}

func assertUnauthenticated(t *testing.T, err error) {
	t.Helper()
	var se *security.SecurityError
	if !errors.As(err, &se) || se.Kind != security.ErrUnauthenticated {
		t.Fatalf("want ErrUnauthenticated, got %v", err)
	}
}
