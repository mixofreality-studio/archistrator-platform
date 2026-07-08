module github.com/mixofreality-studio/archistrator-platform/framework-go-mcp-generator

go 1.25.0

require (
	github.com/google/uuid v1.6.0
	github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel v0.1.0
	github.com/modelcontextprotocol/go-sdk v1.6.1
)

require (
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
)

// framework-go-projectmodel is a sibling module in this repo that owns the shared
// schema-first contract parser (previously an embedded contract/ copy here). The
// replace makes the standalone module build (GOWORK=off) resolve it from the local
// checkout.
replace github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel v0.1.0 => ../framework-go-projectmodel
