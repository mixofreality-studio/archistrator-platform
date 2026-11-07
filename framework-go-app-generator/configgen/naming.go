package configgen

import (
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator/internal/deploynaming"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
)

// upperFirst is shared verbatim with composegen via internal/deploynaming — see its doc comment
// for the full rationale. Infra field and env names are projectmodel's (InfraFieldName,
// InfraEnvName).
var upperFirst = deploynaming.UpperFirst

// upperSnakeFromCamel renders a camelCase setting name as UPPER_SNAKE
// (e.g. "shutdownTimeout" -> "SHUTDOWN_TIMEOUT").
func upperSnakeFromCamel(s string) string {
	return strings.ToUpper(strings.ReplaceAll(projectmodel.Kebab(s), "-", "_"))
}
