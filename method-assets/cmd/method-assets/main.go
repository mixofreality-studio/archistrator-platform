package main

import (
	"flag"
	"fmt"
	"os"

	methodassets "github.com/mixofreality-studio/archistrator-platform/method-assets"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "install" {
		fmt.Fprintln(os.Stderr, "usage: method-assets install --dest <repo>")
		os.Exit(2)
	}
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	dest := fs.String("dest", "", "target repo root")
	_ = fs.Parse(os.Args[2:])
	if *dest == "" {
		fmt.Fprintln(os.Stderr, "install: --dest is required")
		os.Exit(2)
	}
	if err := methodassets.Materialize(*dest); err != nil {
		fmt.Fprintln(os.Stderr, "install:", err)
		os.Exit(1)
	}
	fmt.Println("materialized .claude into", *dest)
}
