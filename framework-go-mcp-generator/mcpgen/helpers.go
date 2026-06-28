package mcpgen

import "strings"

// writeHelpers emits the shared error-mapping helper. The MCP layer surfaces a
// framework manager.Error as a tool error carrying the stable Kind name plus the
// non-leaking Detail; any other error is returned as-is for the SDK to wrap.
func writeHelpers(b *strings.Builder) {
	b.WriteString(helpersSrc)
}

const helpersSrc = `// --- error mapping ---------------------------------------------------------

func mapManagerError(err error) error {
	var me *fwmanager.Error
	if errors.As(err, &me) {
		return fmt.Errorf("%s: %s", me.Kind.String(), me.Detail)
	}
	return err
}
`
