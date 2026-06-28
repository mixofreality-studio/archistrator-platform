// Command mcpgen reads a contract.schema.json and writes the generated MCP tool
// registration source (.go).
//
// Usage:
//
//	mcpgen -contract path/to/contract.schema.json -out-dir gen \
//	       -manager-import github.com/.../internal/manager/project \
//	       -package mcptools
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-mcp-generator/contract"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-mcp-generator/mcpgen"
)

func main() {
	contractPath := flag.String("contract", "", "path to contract.schema.json")
	outDir := flag.String("out-dir", ".", "output directory")
	managerImport := flag.String("manager-import", "", "Go import path of the manager package")
	managerAlias := flag.String("manager-alias", "", "import alias for the manager package")
	pkg := flag.String("package", "", "package name for the generated tools")
	fwManagerImport := flag.String("framework-manager-import", "", "framework-go manager import path")
	securityImport := flag.String("security-import", "", "framework-go security import path")
	mcpImport := flag.String("mcp-import", "", "mcp sdk import path (defaults to official go-sdk)")
	flag.Parse()

	if *contractPath == "" {
		fmt.Fprintln(os.Stderr, "mcpgen: -contract is required")
		os.Exit(2)
	}
	raw, err := os.ReadFile(*contractPath)
	if err != nil {
		fatal(err)
	}
	doc, err := contract.Parse(raw)
	if err != nil {
		fatal(err)
	}
	res, err := mcpgen.Generate(doc, mcpgen.Options{
		Package:                *pkg,
		ManagerImport:          *managerImport,
		ManagerAlias:           *managerAlias,
		FrameworkManagerImport: *fwManagerImport,
		SecurityImport:         *securityImport,
		MCPImport:              *mcpImport,
	})
	if err != nil {
		fatal(err)
	}
	base := contract.Kebab(doc.ManagerBase())
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal(err)
	}
	goPath := filepath.Join(*outDir, base+"_tools.gen.go")
	if err := os.WriteFile(goPath, res.ToolsGo, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s\n", goPath)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "mcpgen:", err)
	os.Exit(1)
}
