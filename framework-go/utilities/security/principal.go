package security

// SecurityPrincipal is the platform's own typed identity, validated from the
// bearer access token a request carried. It is NOT a token and NOT an IdP user
// object — it is an aiarch value type the caller reads from the request context
// (see [PrincipalFrom]) and passes to [Security.Authorize]. The same type
// represents both interactive users and applications; [SecurityPrincipal.Kind]
// discriminates.
//
// The fields are the union of what aiarch products read across the platform:
// products that do role-based decisions read [SecurityPrincipal.Roles]; products
// that scope by tenant/organization read [SecurityPrincipal.Organizations]; a
// product that needs a claim no field captures reaches [SecurityPrincipal.Claims].
type SecurityPrincipal struct {
	Kind          PrincipalKind  // user | application
	Subject       string         // stable opaque subject id ("sub")
	Username      string         // human-facing username ("preferred_username")
	Email         string         // email ("email")
	Name          string         // display name ("name")
	Issuer        string         // the token issuer the validator verified ("iss")
	Roles         []string       // coarse role labels; fine-grained decisions go through Authorize
	Organizations []Organization // tenancy/organization memberships
	Claims        map[string]any // remaining claims — escape hatch for product-specific keys
}

// PrincipalKind classifies a [SecurityPrincipal] as an interactive user or a
// non-interactive application (a service account / client-credentials identity).
type PrincipalKind string

// Principal kinds.
const (
	// PrincipalUser is an interactive end-user identity.
	PrincipalUser PrincipalKind = "user"
	// PrincipalApplication is a non-interactive application/service identity
	// (a client-credentials or service-account token).
	PrincipalApplication PrincipalKind = "application"
)

// Organization is one tenancy/organization a principal belongs to. The platform
// scopes resources by organization; a resource in an organization the principal
// is not a member of is denied (see the default [PolicyDecisionPoint]).
type Organization struct {
	ID   string
	Name string
}

// HasRole reports whether the principal carries the given coarse role label.
func (p SecurityPrincipal) HasRole(role string) bool {
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// IsMemberOf reports whether the principal belongs to an organization matching
// the given id or name. Matching either lets a caller pass whichever identifier
// it holds without first resolving it to the other.
func (p SecurityPrincipal) IsMemberOf(idOrName string) bool {
	if idOrName == "" {
		return false
	}
	for _, o := range p.Organizations {
		if o.ID == idOrName || o.Name == idOrName {
			return true
		}
	}
	return false
}

// IsZero reports whether no principal was resolved (the zero value). [PrincipalFrom]
// returns a zero principal with ok=false when the context carries no principal.
func (p SecurityPrincipal) IsZero() bool {
	return p.Kind == "" && p.Subject == "" && p.Issuer == "" &&
		len(p.Roles) == 0 && len(p.Organizations) == 0 && len(p.Claims) == 0 &&
		p.Username == "" && p.Email == "" && p.Name == ""
}
