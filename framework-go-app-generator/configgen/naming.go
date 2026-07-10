// naming.go
package configgen

import (
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// pascalToken renders an UPPER_SNAKE / kebab / lower token as a PascalCase Go
// identifier fragment, uppercasing known initialisms (e.g. "APP_ID" -> "AppID",
// "github-app" -> "GithubApp", "HOSTPORT" -> "Hostport").
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

// upperSnakeKey renders a lowercase/kebab infra key as UPPER_SNAKE
// (e.g. "github-app" -> "GITHUB_APP", "temporal" -> "TEMPORAL").
func upperSnakeKey(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, "-", "_"))
}

// upperSnakeFromCamel renders a camelCase setting name as UPPER_SNAKE
// (e.g. "shutdownTimeout" -> "SHUTDOWN_TIMEOUT").
func upperSnakeFromCamel(s string) string {
	return strings.ToUpper(strings.ReplaceAll(projectmodel.Kebab(s), "-", "_"))
}
