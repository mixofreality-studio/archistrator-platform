// Package deploynaming holds the setting-naming convention shared VERBATIM between configgen (the
// emitted config.gen.go) and composegen (the emitted main.gen.go): both must derive the SAME Go
// field name from the SAME setting declaration, so composegen's cfg.<Field> references match the
// field configgen emits.
//
// The substrate catalog and the infra naming (projectmodel.Substrate.Inputs,
// projectmodel.InfraFieldName, projectmodel.InfraEnvName) live in projectmodel, which every
// consumer of the deployment model shares, not only these two emitters.
package deploynaming

import "strings"

// UpperFirst uppercases the first rune of an already-camelCase identifier
// (e.g. "listenAddr" -> "ListenAddr").
func UpperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
