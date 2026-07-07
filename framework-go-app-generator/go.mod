module github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator

go 1.25.0

require github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel v0.0.0

// framework-go-projectmodel is a sibling module in this repo. The replace
// makes the standalone module build (GOWORK=off) resolve it from the local
// checkout rather than a published version.
replace github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel => ../framework-go-projectmodel
