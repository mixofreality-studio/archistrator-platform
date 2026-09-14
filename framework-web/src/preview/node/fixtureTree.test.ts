/**
 * The fixture tree validator, over a small schema of the shape webgen generates:
 * layout, JSON, schema, and the bundle-marker ban.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { compileFixtureValidator, validateFixtureTree } from './fixtureTree.ts';
import { BUNDLE_MARKERS } from './markers.ts';

const answer = (result: object): object => ({
  oneOf: [
    { type: 'object', required: ['result'], additionalProperties: false, properties: { result } },
    {
      type: 'object',
      required: ['error'],
      additionalProperties: false,
      properties: {
        error: {
          type: 'object',
          required: ['status'],
          additionalProperties: false,
          properties: { status: { type: 'integer', minimum: 400, maximum: 599 } },
        },
      },
    },
    {
      type: 'object',
      required: ['pending'],
      additionalProperties: false,
      properties: { pending: { const: true } },
    },
  ],
});

const schema = {
  $schema: 'http://json-schema.org/draft-07/schema#',
  type: 'object',
  required: ['route', 'ops'],
  additionalProperties: false,
  properties: {
    route: { type: 'string', pattern: '^/' },
    note: { type: 'string' },
    ops: {
      type: 'object',
      additionalProperties: false,
      properties: {
        getThing: answer({ $ref: '#/definitions/Thing' }),
      },
    },
  },
  definitions: {
    Thing: {
      type: 'object',
      required: ['id'],
      properties: { id: { type: 'string', format: 'int64' } },
    },
  },
};

const validate = compileFixtureValidator(schema);
const SURFACE = 'web-client';

function tree(files: Record<string, string>): string {
  const root = mkdtempSync(join(tmpdir(), 'fixtures-'));
  for (const [name, text] of Object.entries(files)) {
    mkdirSync(join(root, SURFACE, name, '..'), { recursive: true });
    writeFileSync(join(root, SURFACE, name), text);
  }
  return root;
}

const run = (files: Record<string, string>): ReturnType<typeof validateFixtureTree> =>
  validateFixtureTree(tree(files), { surface: SURFACE, validate });

void test('valid states pass; an unknown format is ignored, not an error', () => {
  const r = run({
    'landing/resting.json': JSON.stringify({ route: '/', ops: { getThing: { result: { id: '1' } } } }),
    'landing/loading.json': JSON.stringify({ route: '/', ops: { getThing: { pending: true } } }),
    'landing/load-error.json': JSON.stringify({ route: '/', ops: { getThing: { error: { status: 503 } } } }),
  });
  assert.deepEqual(r.errors, []);
  assert.equal(r.files.length, 3);
});

void test('a missing surface directory is a build with no states', () => {
  const r = validateFixtureTree(mkdtempSync(join(tmpdir(), 'empty-')), { surface: SURFACE, validate });
  assert.deepEqual(r, { files: [], errors: [] });
});

void test('drift, an unknown op, bad JSON and a bad layout are each reported', () => {
  const r = run({
    'landing/drifted.json': JSON.stringify({ route: '/', ops: { getThing: { result: { id: 1 } } } }),
    'landing/unknown-op.json': JSON.stringify({ route: '/', ops: { getThng: { pending: true } } }),
    'landing/not-json.json': '{',
    'landing/Bad_State.json': JSON.stringify({ route: '/', ops: {} }),
    'Bad_Screen/x.json': JSON.stringify({ route: '/', ops: {} }),
  });
  const joined = r.errors.join('\n');
  assert.match(joined, /drifted\.json: \/ops\/getThing/);
  assert.match(joined, /unknown-op\.json/);
  assert.match(joined, /not-json\.json: not JSON/);
  assert.match(joined, /Bad_State\.json: expected a kebab <state>\.json file/);
  assert.match(joined, /Bad_Screen: expected a kebab <screen> directory/);
});

void test('a fixture whose text carries a bundle marker is refused', () => {
  // Fixtures are bundled into the preview; a marker in their text would keep the
  // preview bundle check green with the marked code gone (archistrator P1, M2c).
  for (const marker of BUNDLE_MARKERS) {
    const r = run({ 'landing/resting.json': JSON.stringify({ route: '/', note: `mentions ${marker}`, ops: {} }) });
    assert.equal(r.errors.length, 1, marker);
    assert.match(r.errors[0] ?? '', /bundle marker/);
  }
});
