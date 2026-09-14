/**
 * The FIXTURE transport: the third OpsClient an app's shells can mount, beside the
 * generated REST and MCP transports (webgen's src/api/ops.gen.ts). It answers
 * every op from fixture data and never touches the network. Only a preview build
 * uses it; the production bundle check (archistrator-preview-check-bundle)
 * asserts a production bundle never contains it.
 *
 * Design: design-renderer-data.md §2′.1. Each op's fixture is exactly one of:
 *
 *   - `{ result }`        the call resolves with a fresh deep copy of `result`;
 *   - `{ error }`         the call rejects with the app's real ApiError, so the app
 *                         shows its ordinary error handling;
 *   - `{ pending: true }` the call never settles, which holds a real loading state.
 *
 * A mutation answers the same way and changes no state: the fixture map is never
 * written, and every result is a copy. An Approve click in a preview runs the
 * real handler and the real toast, and changes nothing anywhere.
 *
 * A call with NO fixture is LOUD: it rejects with FixtureMissError (a subclass of
 * the app's ApiError with status 500, never 404, because several hooks read a 404
 * as "nothing here" and would hide the miss) and reports the op to `onMiss`.
 *
 * The runtime is generic; `createFixtureTransport` binds it to one app's op table
 * and ApiError class (webgen scaffolds that binding as src/api/fixtureOps.ts).
 */

/** The wire error a fixture injects. `status` decides, as it does on the wire. */
export interface FixtureError {
  readonly status: number;
  readonly code?: string;
  readonly message?: string;
}

/** One op's canned answer. */
export type FixtureOp =
  | { readonly result: unknown }
  | { readonly error: FixtureError }
  | { readonly pending: true };

/** Every op a fixture answers, keyed by OpId. */
export type FixtureOps<Op extends string = string> = Readonly<Partial<Record<Op, FixtureOp>>>;

/** One screen state's fixture file (<fixtures root>/<surface>/<screen>/<state>.json). */
export interface FixtureFile<Op extends string = string> {
  /** The concrete route the state opens at, e.g. "/project/demo/construction". */
  readonly route: string;
  readonly title?: string;
  readonly note?: string;
  readonly ops: FixtureOps<Op>;
}

/** The structural params of one op call; the same shape as the generated OpParams. */
export interface FixtureOpParams {
  path?: Record<string, string> | undefined;
  query?: Record<string, unknown> | undefined;
  body?: unknown;
}

/** The fixture transport's call surface; structurally the generated OpsClient. */
export interface FixtureOpsClient<Op extends string> {
  call<R = unknown>(op: Op, params?: FixtureOpParams): Promise<R>;
  callForBody<R = unknown>(op: Op, params?: FixtureOpParams): Promise<R>;
}

export interface FixtureOpsOptions<Op extends string> {
  /** Told about every miss, before the call rejects. The preview shell raises its alarm. */
  readonly onMiss?: (opId: Op, params: FixtureOpParams) => void;
}

/** The code a miss carries. The bundle check greps for the class NAME, FixtureMissError. */
export const FIXTURE_MISS_CODE = 'fixture_miss';

/** An app's ApiError class: (status, code, message). */
export type ApiErrorClass<E extends Error> = new (status: number, code: string, message: string) => E;

/** How REST reaches an op; the miss message names it. */
export interface OpRoute {
  readonly method: string;
  readonly path: string;
}

/** A FixtureMissError class bound to one app: an instance is also the app's ApiError. */
export type FixtureMissErrorClass<Op extends string, E extends Error> = new (
  opId: Op
) => E & { readonly opId: Op };

export interface FixtureTransport<Op extends string, E extends Error> {
  readonly fixtureOpsClient: (
    fixtures: FixtureOps<Op>,
    options?: FixtureOpsOptions<Op>
  ) => FixtureOpsClient<Op>;
  readonly FixtureMissError: FixtureMissErrorClass<Op, E>;
}

export interface FixtureTransportConfig<Op extends string, E extends Error> {
  /** The app's ApiError: errors and misses are instances of it. */
  readonly ApiError: ApiErrorClass<E>;
  /** The app's op table (OP_BINDINGS). */
  readonly bindings: Readonly<Record<Op, OpRoute>>;
}

function fixtureFor<Op extends string>(fixtures: FixtureOps<Op>, op: Op): FixtureOp | undefined {
  return Object.prototype.hasOwnProperty.call(fixtures, op) ? fixtures[op] : undefined;
}

/** Binds the fixture transport to one app's op table and ApiError class. */
export function createFixtureTransport<Op extends string, E extends Error>(
  config: FixtureTransportConfig<Op, E>
): FixtureTransport<Op, E> {
  const Base = config.ApiError as unknown as ApiErrorClass<Error>;

  class FixtureMissError extends Base {
    readonly opId: Op;

    constructor(opId: Op) {
      const route = config.bindings[opId];
      super(
        500,
        FIXTURE_MISS_CODE,
        `FixtureMissError: no fixture answers ${opId} (${route.method} ${route.path})`
      );
      this.name = 'FixtureMissError';
      this.opId = opId;
    }
  }

  const answer = <R>(fixture: FixtureOp): Promise<R> => {
    if ('pending' in fixture) {
      return new Promise<R>(() => {
        // Never settles: the state under preview IS the loading state.
      });
    }
    if ('error' in fixture) {
      const { status, code, message } = fixture.error;
      return Promise.reject(
        new config.ApiError(
          status,
          code ?? 'internal',
          message ?? `request failed with status ${String(status)}`
        )
      );
    }
    return Promise.resolve(structuredClone(fixture.result) as R);
  };

  const fixtureOpsClient = (
    fixtures: FixtureOps<Op>,
    options: FixtureOpsOptions<Op> = {}
  ): FixtureOpsClient<Op> => {
    const call = <R = unknown>(op: Op, params: FixtureOpParams = {}): Promise<R> => {
      const fixture = fixtureFor(fixtures, op);
      if (fixture === undefined) {
        options.onMiss?.(op, params);
        return Promise.reject(new FixtureMissError(op));
      }
      return answer<R>(fixture);
    };
    // A fixture's `result` is JSON, so it always carries a body: `callForBody`
    // answers exactly as `call` does (the REST transport's empty-2xx refusal has
    // no fixture equivalent).
    return { call, callForBody: call };
  };

  return {
    fixtureOpsClient,
    FixtureMissError: FixtureMissError as unknown as FixtureMissErrorClass<Op, E>,
  };
}
