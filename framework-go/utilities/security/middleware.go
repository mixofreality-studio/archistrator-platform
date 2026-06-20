package security

import (
	"net/http"
	"strings"
)

// Middleware wraps an http.Handler so every request is authenticated before it
// reaches the wrapped handler: it extracts the bearer token from the
// Authorization header, runs it through v, and on success stashes the resulting
// [SecurityPrincipal] on the request context (readable downstream with
// [PrincipalFrom]). On any failure it writes 401 and does NOT call next — the
// handler never runs for an unauthenticated request.
//
// This is the GTD-equivalent edge: Envoy forwards the Authorization header
// unchanged and the server validates the token itself. The wrapped handler can
// assume a principal is always present.
func Middleware(v Validator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				unauthorized(w, "missing bearer token")
				return
			}
			principal, err := v.ValidateAccessToken(r.Context(), token)
			if err != nil {
				unauthorized(w, "invalid token")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
// The scheme match is case-insensitive per RFC 7235; the token is returned
// verbatim. ok is false when the header is absent, malformed, or carries an empty
// token.
func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	const prefix = "bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(h[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}

// unauthorized writes a 401 with a Bearer challenge. The detail is deliberately
// generic — it never leaks why validation failed.
func unauthorized(w http.ResponseWriter, detail string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "unauthorized: "+detail, http.StatusUnauthorized)
}
