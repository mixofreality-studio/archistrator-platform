module github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator

go 1.25.0

require (
	github.com/mixofreality-studio/archistrator-platform/framework-go v0.1.0
	gopkg.in/yaml.v3 v3.0.1
)

require github.com/google/uuid v1.6.0

// framework-go is a sibling module in this repo. The generated wiring/middleware
// layer compiles against its real utilities/security package (internal/sample/wiring
// is the compile proof). The replace makes the standalone module build (GOWORK=off)
// resolve it from the local checkout.
replace github.com/mixofreality-studio/archistrator-platform/framework-go v0.1.0 => ../framework-go
