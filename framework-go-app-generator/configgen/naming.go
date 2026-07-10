// naming.go
package configgen

import (
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/deploynaming"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// pascalToken and upperFirst are shared verbatim with composegen via
// internal/deploynaming — see its doc comment for the full rationale.
var (
	pascalToken = deploynaming.PascalToken
	upperFirst  = deploynaming.UpperFirst
)

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
