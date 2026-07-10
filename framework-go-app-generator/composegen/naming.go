package composegen

import (
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/deploynaming"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// pascalToken and upperFirst are shared verbatim with configgen via
// internal/deploynaming, so composegen references the SAME config field names
// configgen emits (the two share the field-naming convention; a drift here can
// no longer silently break the cfg.<Field> references).
var (
	pascalToken = deploynaming.PascalToken
	upperFirst  = deploynaming.UpperFirst
)

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

// parentSeg is the second-to-last segment of a goPackage (e.g.
// "internal/engine/billing" -> "engine"), used to mint a collision-disambiguated
// alias (<parentSeg><base> -> "enginebilling"). Empty when there is no parent.
func parentSeg(goPackage string) string {
	segs := strings.Split(goPackage, "/")
	if len(segs) < 2 {
		return ""
	}
	return segs[len(segs)-2]
}

// variantToken renders a binding variant name as the PascalCase fragment of its
// New<Variant><Interface> constructor. Unlike deploynaming.PascalToken (which
// derives config field names and lower-cases token interiors, so "GitHub" ->
// "Github"), variant names are authored PascalCase in the model, so this
// PRESERVES interior capitals: it splits only on '-'/'_' and upper-cases the
// first rune of each part, leaving the rest verbatim ("GitHub" -> "GitHub",
// "GitHubActions" -> "GitHubActions", "postgres" -> "Postgres", "" -> "" for the
// no-arg stub constructor New<Interface>()). Keeping this local to composegen
// leaves configgen's shared field-naming convention untouched.
func variantToken(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' })
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	return b.String()
}

// goBuiltinTypes are the predeclared type names a bare-type qualification must
// NOT prefix with a package alias.
var goBuiltinTypes = map[string]bool{
	"bool": true, "string": true, "error": true, "any": true,
	"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
	"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"uintptr": true, "byte": true, "rune": true,
	"float32": true, "float64": true, "complex64": true, "complex128": true,
}

// qualifyBareTypes prefixes every UNQUALIFIED, exported (Capitalized) type
// identifier in a Go type string with alias (the owning manager's package), the
// same bare-type qualification transportgen applies to an x-go-type. An
// identifier is left untouched when it is already package-qualified (immediately
// preceded by '.'), a predeclared builtin, or unexported (a parameter name like
// "projectID"). So for the systemDesignManager (alias "systemdesign"),
// "func(projectID ProjectID) (sourcecontrol.RepoRef, bool)" ->
// "func(projectID systemdesign.ProjectID) (sourcecontrol.RepoRef, bool)".
func qualifyBareTypes(goType, alias string) string {
	if alias == "" {
		return goType
	}
	var b strings.Builder
	n := len(goType)
	for i := 0; i < n; {
		if !isIdentStart(goType[i]) {
			b.WriteByte(goType[i])
			i++
			continue
		}
		j := i + 1
		for j < n && isIdentPart(goType[j]) {
			j++
		}
		b.WriteString(qualifyWord(goType[i:j], i > 0 && goType[i-1] == '.', alias))
		i = j
	}
	return b.String()
}

// qualifyWord prefixes one identifier with alias iff it is an unqualified,
// exported, non-builtin type name (see qualifyBareTypes).
func qualifyWord(word string, prevDot bool, alias string) string {
	if prevDot || word[0] < 'A' || word[0] > 'Z' || goBuiltinTypes[word] {
		return word
	}
	return alias + "." + word
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
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
