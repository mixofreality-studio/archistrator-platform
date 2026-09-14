/**
 * The FIXTURE transport: the third OpsClient, beside restOpsClient (the browser
 * SPA) and mcpOpsClient (the MCP-hosted app), both in ops.gen.ts. It answers
 * every op from fixture data and never touches the network. Only the preview
 * build (src/previewShell/) uses it; the production build's bundle check
 * (archistrator-preview-check-bundle dist) asserts dist/ never contains it.
 *
 * The runtime is framework-web's (`createFixtureTransport`); this module binds
 * it to this app's op table and its ApiError, so a fixture error or a miss is
 * the app's own ApiError and renders through its ordinary error handling. Each
 * op's fixture is exactly one of:
 *
 *   - `{ result }`        the call resolves with a fresh deep copy of `result`;
 *   - `{ error }`         the call rejects with the real ApiError;
 *   - `{ pending: true }` the call never settles, which holds a real loading state.
 *
 * A mutation answers the same way and changes no state. A call with NO fixture
 * is LOUD: it rejects with FixtureMissError (an ApiError with status 500, never
 * 404, because a 404 reads as "nothing here") and reports the op to `onMiss`.
 *
 * Scaffolded by framework-go-app-generator/webgen; the app's own code from here on.
 */
import {
  createFixtureTransport,
  type FixtureFile as PreviewFixtureFile,
  type FixtureOps as PreviewFixtureOps,
  type FixtureOpsOptions as PreviewFixtureOpsOptions,
} from '@mixofreality-studio/archistrator-platform-framework-web/preview';
import { ApiError } from '../contracts/errors.ts';
import { OP_BINDINGS, type OpId } from './ops.gen.ts';

export {
  FIXTURE_MISS_CODE,
  type FixtureError,
  type FixtureOp,
} from '@mixofreality-studio/archistrator-platform-framework-web/preview';

/** Every op a fixture answers, keyed by OpId. */
export type FixtureOps = PreviewFixtureOps<OpId>;

/** One screen state's fixture file (<fixtures root>/<surface>/<screen>/<state>.json). */
export type FixtureFile = PreviewFixtureFile<OpId>;

export type FixtureOpsOptions = PreviewFixtureOpsOptions<OpId>;

export const { fixtureOpsClient, FixtureMissError } = createFixtureTransport({
  ApiError,
  bindings: OP_BINDINGS,
});

/** A call whose op has no fixture. */
export type FixtureMissError = InstanceType<typeof FixtureMissError>;
