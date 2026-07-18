module github.com/mixofreality-studio/archistrator-platform/framework-go-infrastructure-keycloak

go 1.25.4

require (
	github.com/MicahParks/keyfunc/v3 v3.8.0
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/mixofreality-studio/archistrator-platform/framework-go v0.1.0
)

require (
	github.com/MicahParks/jwkset v0.11.0 // indirect
	golang.org/x/time v0.11.0 // indirect
)

// framework-go is a sibling module in this repo. The replace makes the
// standalone module build (GOWORK=off) resolve it from the local checkout
// instead of a stale published tag — needed while a breaking framework-go
// change (this branch) has not yet been cut as a new release.
replace github.com/mixofreality-studio/archistrator-platform/framework-go v0.1.0 => ../framework-go
