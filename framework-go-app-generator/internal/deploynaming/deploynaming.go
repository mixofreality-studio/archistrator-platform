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
var SubstrateCatalog = map[string][]string{
	"temporal":   {"HOSTPORT", "NAMESPACE"},
	"postgres":   {"URL"},
	"github-app": {"APP_ID", "PRIVATE_KEY_PEM", "ACCOUNT", "APP_SLUG", "INSTALLATION_ID", "API_BASE_URL"},
	"keycloak":   {"JWKS_URL", "ISSUER"},
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
