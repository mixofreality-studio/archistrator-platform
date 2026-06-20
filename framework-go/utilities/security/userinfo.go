package security

import (
	"encoding/json"
	"net/http"
)

// UserInfoResponse is the JSON shape [UserInfoHandler] returns for GET
// /api/userinfo: the validated principal's identity claims. The browser SPA uses
// it to (a) confirm it still has a live edge session (200 vs 401) and (b) render
// the signed-in user. The keys mirror the OIDC claim names the SPA reads.
type UserInfoResponse struct {
	Kind              string                 `json:"kind"`
	Sub               string                 `json:"sub"`
	PreferredUsername string                 `json:"preferred_username,omitempty"`
	Email             string                 `json:"email,omitempty"`
	Name              string                 `json:"name,omitempty"`
	Roles             []string               `json:"roles,omitempty"`
	Organizations     []UserInfoOrganization `json:"organizations,omitempty"`
}

// UserInfoOrganization is one organization membership in a [UserInfoResponse].
type UserInfoOrganization struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// UserInfoHandler is the shared GET /api/userinfo handler for aiarch Go servers
// behind the edge-OIDC topology (GTD parity). It MUST be mounted behind
// [Middleware] (or any wrapper that places a principal on the context via
// [WithPrincipal]): it reads that validated principal and returns its identity
// claims as JSON.
//
// The contract the SPA depends on: a 200 means "you have a live session" and
// carries the user; a 401 means "no session". The SPA reloads the page on 401 to
// trigger the edge OIDC redirect. [Middleware] already returns 401 for an
// absent/invalid bearer token before this handler runs; this handler's own 401
// (no principal on context) is the fail-closed guard for a route mistakenly
// mounted OUTSIDE the auth boundary — never an anonymous allow.
func UserInfoHandler(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFrom(r.Context())
	if !ok {
		unauthorized(w, "no principal")
		return
	}
	resp := UserInfoResponse{
		Kind:              string(principal.Kind),
		Sub:               principal.Subject,
		PreferredUsername: principal.Username,
		Email:             principal.Email,
		Name:              principal.Name,
		Roles:             principal.Roles,
	}
	for _, o := range principal.Organizations {
		resp.Organizations = append(resp.Organizations, UserInfoOrganization{ID: o.ID, Name: o.Name})
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
