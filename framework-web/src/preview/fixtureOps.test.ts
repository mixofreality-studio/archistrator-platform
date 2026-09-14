/**
 * The fixture transport: result, error, pending-forever, a mutation that changes
 * nothing, and the LOUD miss — bound to an app's own ApiError class.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { FIXTURE_MISS_CODE, createFixtureTransport } from './fixtureOps.ts';

class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

const OP_BINDINGS = {
  systemDesignGetProject: { method: 'GET', path: '/api/v1/system-design/get-project/{projectID}' },
  systemDesignListProjects: { method: 'GET', path: '/api/v1/system-design/list-projects' },
  constructionGetSessionState: {
    method: 'GET',
    path: '/api/v1/construction/get-session-state/{projectID}/{activityID}',
  },
  constructionSubmitPhaseDecision: {
    method: 'POST',
    path: '/api/v1/construction/submit-phase-decision/{projectID}/{activityID}',
  },
} as const;
type OpId = keyof typeof OP_BINDINGS;

const { fixtureOpsClient, FixtureMissError } = createFixtureTransport({
  ApiError,
  bindings: OP_BINDINGS,
});

const PENDING = Symbol('pending');

async function settledWithin<T>(p: Promise<T>, ms: number): Promise<T | typeof PENDING> {
  return Promise.race([
    p,
    new Promise<typeof PENDING>((resolve) => {
      setTimeout(() => {
        resolve(PENDING);
      }, ms);
    }),
  ]);
}

interface Demo {
  Name: string;
  Meta: { count: number };
}

void test('a result fixture resolves with a fresh copy every call', async () => {
  const fixtures = { systemDesignGetProject: { result: { Name: 'demo', Meta: { count: 1 } } } };
  const ops = fixtureOpsClient(fixtures);
  const first = await ops.call<Demo>('systemDesignGetProject');
  assert.deepEqual(first, { Name: 'demo', Meta: { count: 1 } });
  first.Name = 'mutated by a component';
  first.Meta.count = 99;
  const second = await ops.call<Demo>('systemDesignGetProject');
  assert.deepEqual(second, { Name: 'demo', Meta: { count: 1 } });
  assert.deepEqual(fixtures.systemDesignGetProject.result, { Name: 'demo', Meta: { count: 1 } });
});

void test("an error fixture rejects with the app's real ApiError", async () => {
  const ops = fixtureOpsClient({
    systemDesignGetProject: { error: { status: 409, code: 'failedPrecondition', message: 'x' } },
    systemDesignListProjects: { error: { status: 503 } },
  });
  await assert.rejects(ops.call('systemDesignGetProject'), (err: unknown) => {
    assert.ok(err instanceof ApiError);
    assert.equal(err.status, 409);
    assert.equal(err.code, 'failedPrecondition');
    assert.equal(err.message, 'x');
    return true;
  });
  await assert.rejects(ops.call('systemDesignListProjects'), (err: unknown) => {
    assert.ok(err instanceof ApiError);
    assert.equal(err.status, 503);
    assert.equal(err.code, 'internal');
    assert.equal(err.message, 'request failed with status 503');
    return true;
  });
});

void test('a pending fixture never settles', async () => {
  const ops = fixtureOpsClient({ systemDesignGetProject: { pending: true } });
  assert.equal(await settledWithin(ops.call('systemDesignGetProject'), 50), PENDING);
});

void test('a mutation answers from its fixture and changes no state', async () => {
  const fixtures = {
    systemDesignGetProject: { result: { Name: 'demo' } },
    constructionSubmitPhaseDecision: { result: null },
  };
  const before = structuredClone(fixtures);
  const ops = fixtureOpsClient(fixtures);
  await ops.call('constructionSubmitPhaseDecision', {
    path: { projectID: 'demo', activityID: 'C-x' },
    body: { decision: 'approve' },
  });
  await ops.callForBody('constructionSubmitPhaseDecision', { body: { decision: 'reject' } });
  assert.deepEqual(await ops.call('systemDesignGetProject'), { Name: 'demo' });
  assert.deepEqual(fixtures, before);
});

void test('a call with no fixture fails LOUDLY: FixtureMissError, never a 404', async () => {
  const missed: OpId[] = [];
  const ops = fixtureOpsClient(
    { systemDesignGetProject: { result: {} } },
    {
      onMiss: (opId) => {
        missed.push(opId);
      },
    }
  );
  await assert.rejects(ops.call('constructionGetSessionState'), (err: unknown) => {
    assert.ok(err instanceof FixtureMissError);
    assert.ok(err instanceof ApiError, "renders as the app's ordinary error");
    assert.equal(err.name, 'FixtureMissError');
    assert.equal(err.status, 500, 'a 404 would read as "nothing here" and hide the miss');
    assert.equal(err.code, FIXTURE_MISS_CODE);
    assert.equal(err.opId, 'constructionGetSessionState');
    assert.match(err.message, /constructionGetSessionState/);
    assert.match(err.message, /GET \/api\/v1\/construction\/get-session-state\//);
    return true;
  });
  assert.deepEqual(missed, ['constructionGetSessionState']);
});

void test('a mutation with no fixture misses too', async () => {
  const ops = fixtureOpsClient({});
  await assert.rejects(ops.call('constructionSubmitPhaseDecision'), FixtureMissError);
});

void test('an inherited property is not a fixture', async () => {
  const fixtures = Object.create({ systemDesignGetProject: { result: 'from the prototype' } }) as {
    systemDesignGetProject?: { result: unknown };
  };
  const ops = fixtureOpsClient(fixtures);
  await assert.rejects(ops.call('systemDesignGetProject'), FixtureMissError);
});

void test('each app binding gets its own miss class', () => {
  class OtherError extends Error {
    constructor(_status: number, _code: string, message: string) {
      super(message);
    }
  }
  const other = createFixtureTransport({ ApiError: OtherError, bindings: OP_BINDINGS });
  const miss = new other.FixtureMissError('systemDesignGetProject');
  assert.ok(miss instanceof OtherError);
  assert.ok(!(miss instanceof ApiError));
  assert.ok(!(miss instanceof FixtureMissError));
});
