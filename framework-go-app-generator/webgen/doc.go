// Package webgen is the web half of app generation: from a server's generated
// OpenAPI document (httpgen) and its generated MCP tool tables (mcpemit/mcpgen),
// it emits the webApp's transport seam and everything preview mode needs, so
// every generated app gets "the real components, rendered in preview mode with
// mock data" (design-renderer-data.md §2′, step P2).
//
// Generated (rewritten every run; `webgen check` fails on drift):
//
//   - src/api/ops.gen.ts: the OpsClient, OP_BINDINGS and the REST and MCP
//     transports. Every /api/v1/<mgr>/<op> path binds OpId camel(mgr)+Pascal(op);
//     its MCP tool is bound only when the server's tool tables register it, so the
//     MCP transport refuses loudly instead of calling a tool that does not exist.
//     The hand-declared composition routes (webgen.json) ride the same table.
//   - preview/fixtures.schema.json: the preview fixtures' JSON Schema, from the
//     OAS 200 schemas (FixtureSchema).
//   - src/previewShell/fixtures.gen.test.ts: validates every fixture against it.
//
// Scaffolded (written once, then the app's own code): the preview entry
// (src/previewShell/main.tsx, preview.html), vite.preview.config.ts, and the
// transport's app-side modules (errors.ts, opsContext.tsx, opTypes.ts,
// fixtureOps.ts). The runtime they import — the fixture transport, the network
// and navigation guards, the incident alarm, the fixture registry, the Vite
// fixture plugin and the production-bundle check — is framework-web's
// (@mixofreality-studio/archistrator-platform-framework-web/preview,
// /preview/install and /preview/node).
//
// Skeleton writes a schema-valid minimal fixture for a screen state's ops, which
// the ui-designer then fills in with meaningful values.
//
// The reference is archistrator's webApp at bcb20abf, whose JavaScript
// generators (scripts/gen-ops.mjs, op-bindings.mjs, mcp-tools.mjs,
// composition-routes.mjs, fixture-schema.mjs) this package replaces. The golden
// test regenerates archistrator's files from its committed OAS and tool tables
// and pins every difference from the committed bytes, with its reason, in
// testdata/webgen/archistrator-bcb20abf/*.diff.
//
// Not generated here: src/contracts/schema.ts stays openapi-typescript's output
// (the app's gen:api), because re-implementing openapi-typescript byte for byte
// is a separate job from the preview seam.
package webgen
