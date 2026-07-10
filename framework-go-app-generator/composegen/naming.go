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
