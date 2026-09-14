/**
 * A 401 on the session probe (UserProvider) in a preview shows the preview
 * error page, once, instead of reloading forever over the same fixture 401.
 * The control: the same app outside a preview still reloads to sign in.
 */
import { after, before, test } from 'node:test';
import assert from 'node:assert/strict';
import type { Browser } from 'playwright';
import { PREVIEW_META_CSP } from '../../src/preview/node/headers.ts';
import {
  bundle,
  html,
  js,
  launch,
  page,
  serveApp,
  type Incident,
  type Served,
} from './harness.ts';

let browser: Browser;
let app: Served;

before(async () => {
  const entry = await bundle('./sessionEntry.tsx');
  app = await serveApp({
    '/': { type: html, body: page(PREVIEW_META_CSP) },
    '/entry.js': { type: js, body: entry },
  });
  browser = await launch();
});

after(async () => {
  await browser.close();
  await app.close();
});

const documentLoads = (): number => app.hits.filter((h) => h.startsWith('/?')).length;

void test('in a preview, a 401 shows the preview error page and never reloads', async () => {
  const p = await browser.newPage();
  const before = documentLoads();
  await p.goto(`${app.origin}/?preview`);
  const errorPage = p.getByTestId('preview-error-page');
  await errorPage.waitFor();
  assert.match((await errorPage.textContent()) ?? '', /A preview cannot sign in/);
  await p.waitForTimeout(800);
  assert.equal(documentLoads() - before, 1, 'the preview reloaded');
  const incidents: Incident[] = await p.evaluate(() => [
    ...(window.__ARCHISTRATOR_PREVIEW__?.incidents ?? []),
  ]);
  assert.deepEqual(incidents, [
    {
      kind: 'unauthenticated',
      detail: 'the session probe answered 401; a preview cannot sign in',
    },
  ]);
  assert.equal(await p.getByTestId('app').count(), 0);
  await p.close();
});

void test('outside a preview, the same 401 still reloads to sign in', async () => {
  const p = await browser.newPage();
  const before = documentLoads();
  await p.goto(`${app.origin}/?plain`);
  await p.waitForTimeout(1500);
  assert.ok(documentLoads() - before >= 2, `loads: ${String(documentLoads() - before)}`);
  await p.close();
});
