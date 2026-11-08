module github.com/mixofreality-studio/archistrator-platform/framework-go-conformance

go 1.25.4

require (
	github.com/mixofreality-studio/archistrator-platform/framework-go v0.22.0
	github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator v0.24.1
	github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel v0.5.0
	github.com/mixofreality-studio/archistrator-platform/method-assets v0.24.0
)

require (
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator v0.5.0 // indirect
	golang.org/x/mod v0.34.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/tools v0.43.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// Sibling modules in this repo. Each require pins the published release whose API
// this module's code uses; the unversioned replaces make the standalone module
// build (GOWORK=off, as CI runs it) resolve the local checkout instead.
//   - framework-go-app-generator: the chain runs its modelgen/nullgen/testgen.
//   - method-assets: the harness seats the scaffold a project is born with
//     (methodassets.ScaffoldFiles) and runs the chain against it. This module
//     exists so framework-go-app-generator need not require method-assets, whose
//     scaffold pins app-generator.
//   - framework-go, framework-go-projectmodel: the typed model and the
//     methodcheck/scenario packages the run021 test reads.
replace (
	github.com/mixofreality-studio/archistrator-platform/framework-go => ../framework-go
	github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator => ../framework-go-app-generator
	github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator => ../framework-go-http-generator
	github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel => ../framework-go-projectmodel
	github.com/mixofreality-studio/archistrator-platform/method-assets => ../method-assets
)
