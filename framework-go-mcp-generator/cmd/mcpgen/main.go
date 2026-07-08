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

	"github.com/mixofreality-studio/archistrator-platform/framework-go-mcp-generator/mcpgen"
	projectmodel "github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel"
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
	doc, err := projectmodel.Parse(raw)
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
	base := projectmodel.Kebab(doc.ManagerBase())
	writeOutput(*outDir, base, res)
}

// writeOutput creates outDir and writes the generated MCP tool registration source.
func writeOutput(outDir, base string, res mcpgen.Result) {
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		fatal(err)
	}
	// base derives from the contract document; pin it to a single path element so
	// a malformed manager name can never escape outDir.
	base = filepath.Base(base)
	goPath := filepath.Join(outDir, base+"_tools.gen.go")
	if err := os.WriteFile(goPath, res.ToolsGo, 0o600); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s\n", goPath)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "mcpgen:", err)
	os.Exit(1)
}
