// Command httpgen reads a contract.schema.json and writes the generated REST
// handlers (.go) and OpenAPI 3.1 document (.yaml).
//
// Usage:
//
//	httpgen -contract path/to/contract.schema.json -out-dir gen \
//	        -manager-import github.com/.../internal/manager/project \
//	        -package httpapi
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/contract"
	"github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator/httpgen"
)

func main() {
	contractPath := flag.String("contract", "", "path to contract.schema.json")
	outDir := flag.String("out-dir", ".", "output directory")
	managerImport := flag.String("manager-import", "", "Go import path of the manager package")
	managerAlias := flag.String("manager-alias", "", "import alias for the manager package")
	pkg := flag.String("package", "", "package name for the generated handlers")
	fwManagerImport := flag.String("framework-manager-import", "", "framework-go manager import path (defaults to archistrator-platform)")
	securityImport := flag.String("security-import", "", "framework-go security import path (defaults to archistrator-platform)")
	wiring := flag.Bool("wiring", false, "also emit the component-agnostic auth middleware + HTTP wiring layer (middleware.gen.go + server.gen.go)")
	wiringPkg := flag.String("wiring-package", "", "package name for the emitted wiring layer (default httpserver)")
	flag.Parse()

	if *contractPath == "" {
		fmt.Fprintln(os.Stderr, "httpgen: -contract is required")
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
	res, err := httpgen.Generate(doc, httpgen.Options{
		Package:                *pkg,
		ManagerImport:          *managerImport,
		ManagerAlias:           *managerAlias,
		FrameworkManagerImport: *fwManagerImport,
		SecurityImport:         *securityImport,
	})
	if err != nil {
		fatal(err)
	}
	base := contract.Kebab(doc.ManagerBase())
	writeOutputs(*outDir, base, res)

	if *wiring {
		emitWiring(*outDir, *wiringPkg, *securityImport)
	}
}

// writeOutputs creates outDir and writes the generated handlers (.go) and the
// OpenAPI document (.yaml).
func writeOutputs(outDir, base string, res httpgen.Result) {
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		fatal(err)
	}
	// base derives from the contract document; pin it to a single path element so
	// a malformed manager name can never escape outDir.
	base = filepath.Base(base)
	goPath := filepath.Join(outDir, base+"_handlers.gen.go")
	yamlPath := filepath.Join(outDir, base+".openapi.gen.yaml")
	if err := os.WriteFile(goPath, res.HandlersGo, 0o600); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(yamlPath, res.OpenAPIYAML, 0o600); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s\nwrote %s\n", goPath, yamlPath)
}

// emitWiring generates and writes the component-agnostic wiring layer
// (middleware.gen.go + server.gen.go) into outDir.
func emitWiring(outDir, wiringPkg, securityImport string) {
	wres, err := httpgen.GenerateWiring(httpgen.WiringOptions{
		Package:        wiringPkg,
		SecurityImport: securityImport,
	})
	if err != nil {
		fatal(err)
	}
	mwPath := filepath.Join(outDir, "middleware.gen.go")
	srvPath := filepath.Join(outDir, "server.gen.go")
	if err := os.WriteFile(mwPath, wres.MiddlewareGo, 0o600); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(srvPath, wres.ServerGo, 0o600); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s\nwrote %s\n", mwPath, srvPath)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "httpgen:", err)
	os.Exit(1)
}
