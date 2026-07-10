package composegen

import (
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// initialisms are the input/key tokens rendered all-caps in a derived Go field
// name. This mirrors configgen.initialisms so composegen references the SAME
// config field names configgen emits (the two share the field-naming
// convention; a drift here silently breaks the cfg.<Field> references).
var initialisms = map[string]bool{
	"ID":   true,
	"URL":  true,
	"PEM":  true,
	"API":  true,
	"JWKS": true,
}

// pascalToken renders an UPPER_SNAKE / kebab / lower token as a PascalCase Go
// identifier fragment (configgen.pascalToken's counterpart): "APP_ID" -> "AppID",
// "github-app" -> "GithubApp", "HOSTPORT" -> "Hostport".
func pascalToken(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' })
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		if initialisms[strings.ToUpper(p)] {
			b.WriteString(strings.ToUpper(p))
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]) + strings.ToLower(p[1:]))
	}
	return b.String()
}

// upperFirst uppercases the first rune of an already-camelCase identifier
// (e.g. "listenAddr" -> "ListenAddr").
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// infraFieldName is the config.gen.go field for one infra input: the configgen
// convention pascalToken(infraKey)+pascalToken(input) (e.g. temporal+HOSTPORT ->
// "TemporalHostport", postgres+URL -> "PostgresURL").
func infraFieldName(infraKey, input string) string {
	return pascalToken(infraKey) + pascalToken(input)
}

// settingFieldName is the config.gen.go field for one setting: upperFirst(name)
// (e.g. "listenAddr" -> "ListenAddr").
func settingFieldName(name string) string { return upperFirst(name) }

// pkgAlias is a component package's import alias / selector: the last segment of
// its goPackage (e.g. "internal/manager/order" -> "order").
func pkgAlias(goPackage string) string {
	if i := strings.LastIndex(goPackage, "/"); i >= 0 {
		return goPackage[i+1:]
	}
	return goPackage
}

// webPkgBase is the generated web-handler package segment for a Manager: the
// last segment of its goPackage (archistrator convention:
// internal/manager/systemdesign -> internal/client/web/systemdesign).
func webPkgBase(goPackage string) string { return pkgAlias(goPackage) }

// hasSetting reports whether the deployment declares a top-level setting whose
// name matches (case-insensitively) dep — the plain-dep-from-config test.
func hasSetting(d *projectmodel.Deployment, dep string) (string, bool) {
	for _, s := range d.Settings {
		if strings.EqualFold(s.Name, dep) {
			return settingFieldName(s.Name), true
		}
	}
	return "", false
}
