// contractgen generates _contract_gen.go files from seed-contract JSON.
// Usage: go run ./cmd/contractgen -contracts <dir> -out <dir>
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mixofreality-studio/archistrator-platform/framework-go/contractgen"
)

func main() {
	contractDir := flag.String("contracts", "", "directory containing *.json contract files")
	outDir := flag.String("out", "", "output directory for generated *_contract_gen.go files")
	flag.Parse()

	if *contractDir == "" || *outDir == "" {
		fmt.Fprintln(os.Stderr, "usage: contractgen -contracts <dir> -out <dir>")
		os.Exit(1)
	}

	entries, err := os.ReadDir(*contractDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read contract dir: %v\n", err)
		os.Exit(1)
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(*contractDir, e.Name()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", e.Name(), err)
			os.Exit(1)
		}
		c, err := contractgen.ParseContract(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "parse %s: %v\n", e.Name(), err)
			os.Exit(1)
		}
		if err := contractgen.ValidateContract(c); err != nil {
			fmt.Fprintf(os.Stderr, "validate %s: %v\n", e.Name(), err)
			os.Exit(1)
		}
		src, err := contractgen.Generate(c)
		if err != nil {
			fmt.Fprintf(os.Stderr, "generate %s: %v\n", e.Name(), err)
			os.Exit(1)
		}
		outFile := filepath.Join(*outDir, strings.TrimSuffix(e.Name(), ".json")+"_contract_gen.go")
		if err := os.WriteFile(outFile, src, 0600); err != nil {
			fmt.Fprintf(os.Stderr, "write %s: %v\n", outFile, err)
			os.Exit(1)
		}
		fmt.Printf("wrote %s\n", outFile)
	}
}
