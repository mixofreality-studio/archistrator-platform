// Package frameworkgo is the root of github.com/mixofreality-studio/archistrator-platform/framework-go, the
// shared Go library for systems constructed and operated by aiarch following
// Juval Löwy's The Method. It has no code of its own; the layer packages are:
//
//   - resourceaccess — the ResourceAccess-layer error model (no Temporal/IO).
//   - engine         — the Engine-layer error model (no Temporal/IO).
//   - manager        — the Manager-layer error model AND the error→Temporal
//     bridge. This is the ONLY package that imports go.temporal.io.
//   - arch           — a go/packages-based architecture-rules checker that any
//     consuming module runs as an ordinary go test.
package frameworkgo
