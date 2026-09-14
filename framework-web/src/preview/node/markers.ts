/**
 * The string markers that identify preview-only code in a bundle. Each is a
 * string literal in exactly one preview-only runtime module, and string literals
 * survive minification (a symbol name would not: the minifier renames
 * `fixtureOpsClient`).
 *
 * Fixture DATA must never contain one; validateFixtureTree refuses such a
 * fixture. Fixtures are bundled into the preview, so a marker in a fixture's text
 * would satisfy the preview's positive control even after the code carrying it
 * was gone. Mutation testing found exactly that (archistrator preview P1, M2c).
 */
export const BUNDLE_MARKERS: readonly string[] = [
  // The fixture transport's miss error (preview/fixtureOps.ts sets this.name).
  'FixtureMissError',
  // The preview network guard's message prefix (preview/networkGuard.ts).
  'archistrator-preview-network-guard',
];
