// Package deploynaming holds the deployment substrate catalog and Go
// identifier-naming conventions shared VERBATIM between configgen (the
// emitted config.gen.go) and composegen (the emitted main.gen.go). Both
// emitters must derive the SAME Go field name from the SAME infra/setting
// declaration — composegen's cfg.<Field> references have to match the field
// configgen actually emits. Hoisting the shared pieces here (rather than two
// copies kept in sync "by convention") makes that drift structurally
// impossible instead of merely documented.
package deploynaming

import "strings"

// SubstrateInput is one configuration input a substrate declares: its
// canonical UPPER_SNAKE name plus an OPTIONAL default. A non-empty Default
// means the input is never "missing" — LoadConfig falls back to it instead of
// hard-requiring the env var, and it is excluded from MissingFor/DormantWarnings'
// required-var accounting. An empty Default preserves today's behavior (the
// input is required per its owning decl's presence/profile coverage).
//
// Field named Name (not "Token") deliberately: gosec's G101 hardcoded-
// credentials heuristic pattern-matches a "Token"-named struct field against
// its string literal value, flagging entries like PRIVATE_KEY_PEM/JWKS_URL as
// false-positive hardcoded secrets even though these are catalog INPUT NAMES,
// not credential values.
type SubstrateInput struct {
	Name    string
	Default string
}

// SubstrateCatalog is the MECHANISM half of the deployment split: it maps a
// declared infrastructure substrate to its ordered set of configuration
// INPUTS. project.json declares only intent (which substrates, which
// profiles, which env overrides); this catalog supplies the concrete inputs
// each substrate needs. Every input is a plain string on the wire — typed
// coercion (e.g. the github-app installation id to int64, the PEM to a
// resolved secret) is a composition-root concern the hand adapter owns, NOT
// the generator.
//
// The input token is the canonical UPPER_SNAKE name; configgen derives both
// the Go field name (PascalCase) and the default env-var name
// (<PREFIX>_<INFRAKEY>_<INPUT>) from it, and a per-declaration env override
// may replace the latter for backwards compatibility. composegen threads the
// SAME cfg fields the config file exposes for those inputs.
//
// Only "temporal" carries input defaults today (the archistrator dev cluster's
// well-known Temporal frontend address/namespace); every other substrate is
// unchanged (no default — required per presence/profile coverage as before).
var SubstrateCatalog = map[string][]SubstrateInput{
	"temporal": {
		{Name: "HOSTPORT", Default: "temporal-frontend.temporal.svc:7233"},
		{Name: "NAMESPACE", Default: "default"},
	},
	"postgres":   {{Name: "URL"}},
	"github-app": {{Name: "APP_ID"}, {Name: "PRIVATE_KEY_PEM"}, {Name: "ACCOUNT"}, {Name: "APP_SLUG"}, {Name: "INSTALLATION_ID"}, {Name: "API_BASE_URL"}},
	"keycloak":   {{Name: "JWKS_URL"}, {Name: "ISSUER"}},
	"otel":       {}, // env-driven by the OTEL_* SDK convention; no declared inputs
}

// Initialisms are the input/key tokens rendered all-caps in a derived Go
// field name (idiomatic-Go naming), rather than Title-cased.
var Initialisms = map[string]bool{
	"ID":   true,
	"URL":  true,
	"PEM":  true,
	"API":  true,
	"JWKS": true,
}

// PascalToken renders an UPPER_SNAKE / kebab / lower token as a PascalCase Go
// identifier fragment, uppercasing known initialisms (e.g. "APP_ID" ->
// "AppID", "github-app" -> "GithubApp", "HOSTPORT" -> "Hostport").
func PascalToken(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' })
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		if Initialisms[strings.ToUpper(p)] {
			b.WriteString(strings.ToUpper(p))
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]) + strings.ToLower(p[1:]))
	}
	return b.String()
}

// UpperFirst uppercases the first rune of an already-camelCase identifier
// (e.g. "listenAddr" -> "ListenAddr").
func UpperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
