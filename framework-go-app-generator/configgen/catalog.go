// catalog.go
package configgen

// substrateCatalog is the MECHANISM half of the deployment split: it maps a
// declared infrastructure substrate to its ordered set of configuration
// INPUTS. project.json declares only intent (which substrates, which profiles,
// which env overrides); this catalog supplies the concrete inputs each
// substrate needs. Every input is a plain string on the wire — typed coercion
// (e.g. the github-app installation id to int64, the PEM to a resolved secret)
// is a composition-root concern the hand adapter owns, NOT the generator.
//
// The input token is the canonical UPPER_SNAKE name; the emitter derives both
// the Go field name (PascalCase) and the default env-var name
// (<PREFIX>_<INFRAKEY>_<INPUT>) from it, and a per-declaration env override may
// replace the latter for backwards compatibility.
var substrateCatalog = map[string][]string{
	"temporal":   {"HOSTPORT", "NAMESPACE"},
	"postgres":   {"URL"},
	"github-app": {"APP_ID", "PRIVATE_KEY_PEM", "ACCOUNT", "APP_SLUG", "INSTALLATION_ID", "API_BASE_URL"},
	"keycloak":   {"JWKS_URL", "ISSUER"},
	"otel":       {}, // env-driven by the OTEL_* SDK convention; no declared inputs
}

// initialisms are the input/key tokens rendered all-caps in the derived Go
// field name (idiomatic-Go naming), rather than Title-cased.
var initialisms = map[string]bool{
	"ID":   true,
	"URL":  true,
	"PEM":  true,
	"API":  true,
	"JWKS": true,
}
