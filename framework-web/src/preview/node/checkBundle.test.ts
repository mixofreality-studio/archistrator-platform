/**
 * The production-bundle check: a production bundle carrying a marker fails, a
 * preview bundle lacking one fails (the positive control), a clean pair passes,
 * and the markers really are the runtime's literals.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { checkBundle } from './checkBundle.ts';
import { BUNDLE_MARKERS } from './markers.ts';
import { createFixtureTransport } from '../fixtureOps.ts';
import { NETWORK_GUARD_MARKER } from '../networkGuard.ts';

function bundle(files: Record<string, string>): string {
  const dir = mkdtempSync(join(tmpdir(), 'bundle-'));
  for (const [name, text] of Object.entries(files)) {
    mkdirSync(join(dir, name, '..'), { recursive: true });
    writeFileSync(join(dir, name), text);
  }
  return dir;
}

const html = '<!doctype html><div id="root"></div>';

void test('the markers are the literals the preview runtime carries', () => {
  const { FixtureMissError } = createFixtureTransport({
    ApiError: class extends Error {
      constructor(_s: number, _c: string, m: string) {
        super(m);
      }
    },
    bindings: { op: { method: 'GET', path: '/x' } },
  });
  assert.deepEqual(BUNDLE_MARKERS, [new FixtureMissError('op').name, NETWORK_GUARD_MARKER]);
});

void test('a production bundle with no marker passes', () => {
  const r = checkBundle(bundle({ 'index.html': html, 'assets/index.js': 'console.log(1)' }), 'production');
  assert.equal(r.ok, true, r.message);
});

void test('a production bundle that ships preview code fails, naming the file', () => {
  for (const marker of BUNDLE_MARKERS) {
    const dir = bundle({ 'index.html': html, 'assets/index.js': `throw "${marker}"` });
    const r = checkBundle(dir, 'production');
    assert.equal(r.ok, false);
    assert.match(r.message, new RegExp(`${marker} in assets/index\\.js`));
  }
});

void test('the preview control fails when a marker is missing, passes with both', () => {
  const partial = bundle({ 'index.html': html, 'a.js': BUNDLE_MARKERS[0] ?? '' });
  assert.equal(checkBundle(partial, 'preview').ok, false);
  const full = bundle({ 'index.html': html, 'a.js': BUNDLE_MARKERS.join(' ') });
  assert.equal(checkBundle(full, 'preview').ok, true);
});

void test('something that is not a finished build fails in both modes', () => {
  for (const mode of ['production', 'preview'] as const) {
    assert.equal(checkBundle(join(tmpdir(), 'no-such-bundle-dir'), mode).ok, false);
    assert.equal(checkBundle(bundle({ 'a.js': '' }), mode).ok, false, 'no index.html');
    assert.equal(checkBundle(bundle({ 'index.html': html }), mode).ok, false, 'no JavaScript');
  }
});
