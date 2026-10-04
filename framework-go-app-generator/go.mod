module github.com/mixofreality-studio/archistrator-platform/framework-go-app-generator

go 1.25.0

require (
	github.com/google/jsonschema-go v0.4.3
	github.com/mixofreality-studio/archistrator-platform/framework-go v0.16.0
	github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator v0.4.0
	github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel v0.2.3
	go.temporal.io/sdk v1.44.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/facebookgo/clock v0.0.0-20150410010913-600d898af40a // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/mock v1.6.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.3.2 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	github.com/nexus-rpc/sdk-go v0.6.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/robfig/cron v1.2.0 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	go.temporal.io/api v1.62.12 // indirect
	golang.org/x/mod v0.34.0 // indirect
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	golang.org/x/time v0.11.0 // indirect
	golang.org/x/tools v0.43.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260401024825-9d38bb4040a9 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260401024825-9d38bb4040a9 // indirect
	google.golang.org/grpc v1.80.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

// Sibling modules in this repo. Each require pins the published release whose
// API this module's code uses, so a consumer that resolves this module as a
// dependency (where these replaces are ignored) gets a compatible minimum
// version; the unversioned replaces make the standalone module build
// (GOWORK=off, as CI runs it) resolve the local checkout instead.
//   - framework-go: the compile-proof sample (internal/sample/order) imports its
//     real manager/resourceaccess packages; testgen reads the committed test
//     plans through its scenario and methodcheck packages.
//   - framework-go-http-generator: transportgen consumes its exported route
//     planner (httpgen.PlanOps) as the single source of route/verb/param truth.
//   - framework-go-projectmodel: the typed project.json model every emitter reads.
replace (
	github.com/mixofreality-studio/archistrator-platform/framework-go => ../framework-go
	github.com/mixofreality-studio/archistrator-platform/framework-go-http-generator => ../framework-go-http-generator
	github.com/mixofreality-studio/archistrator-platform/framework-go-projectmodel => ../framework-go-projectmodel
)
