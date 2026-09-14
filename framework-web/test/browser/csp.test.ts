/**
 * The preview page is network-dead on its own (preview P2 review I1): webgen's
 * preview.html template, served with NO response headers, lets no image,
 * script, stylesheet, CSS url(), font, prefetch, preload, dynamic import(),
 * media, frame, worker or request reach a logging server on another origin.
 * So do the response headers alone. The control, the same page with no policy
 * at all, reaches the logger through every one of those doors, so the test can
 * see a leak.
 */
import { after, before, test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import type { Browser } from 'playwright';
import { PREVIEW_META_CSP, PREVIEW_RESPONSE_HEADERS } from '../../src/preview/node/headers.ts';
import { html, js, launch, repoRoot, serveApp, serveLogger, type Served } from './harness.ts';

const TEMPLATE = join(
  repoRoot,
  '..',
  'framework-go-app-generator',
  'webgen',
  'templates',
  'preview.html.tmpl'
);

/** The doors, each a request path on the logger. */
const DOORS = [
  'img',
  'script.js',
  'style.css',
  'cssurl',
  'font',
  'prefetch',
  'preload',
  'dynimport.js',
  'video',
  'frame',
  'worker.js',
  'fetch',
  'beacon',
  'imgset',
];

// Runs in the page, as its own same-origin module (script-src 'self' allows it).
const attack = (target: string): string => `
const T = ${JSON.stringify(target)};
const add = (html) => document.body.insertAdjacentHTML('beforeend', html);
add('<img src="' + T + '/img">');
add('<img srcset="' + T + '/imgset 1x">');
add('<script src="' + T + '/script.js"></script>');
const s = document.createElement('script'); s.src = T + '/script.js?dom'; document.head.append(s);
document.head.insertAdjacentHTML('beforeend', '<link rel="stylesheet" href="' + T + '/style.css">');
document.head.insertAdjacentHTML('beforeend', '<link rel="prefetch" href="' + T + '/prefetch">');
document.head.insertAdjacentHTML('beforeend', '<link rel="preload" as="image" href="' + T + '/preload">');
const st = document.createElement('style');
st.textContent = '@font-face { font-family: leak; src: url(' + T + '/font); } ' +
  'body { background: url(' + T + '/cssurl); font-family: leak; }';
document.head.append(st);
add('<p style="font-family: leak">x</p>');
add('<video src="' + T + '/video" autoplay muted></video>');
add('<iframe src="' + T + '/frame"></iframe>');
try { new Worker(T + '/worker.js'); } catch {}
try { fetch(T + '/fetch').catch(() => {}); } catch {}
try { navigator.sendBeacon(T + '/beacon'); } catch {}
import(T + '/dynimport.js').catch(() => {});
`;

let browser: Browser;
let logger: Served;

before(async () => {
  logger = await serveLogger();
  browser = await launch();
});

after(async () => {
  await browser.close();
  await logger.close();
});

function templatePage(): string {
  return readFileSync(TEMPLATE, 'utf8')
    .replaceAll('@@APP@@', 'app')
    .replace('/src/previewShell/main.tsx', '/attack.js');
}

function withoutMeta(page: string): string {
  const stripped = page.replace(/<meta\s+http-equiv="Content-Security-Policy"[^>]*>/, '');
  assert.notEqual(stripped, page, 'the template has no CSP meta to strip');
  return stripped;
}

/** Serves `body` (with `headers`) and returns the doors the logger heard. */
async function doorsReached(body: string, headers: Record<string, string> = {}): Promise<string[]> {
  const app = await serveApp({
    '/': { type: html, body, headers },
    '/attack.js': { type: js, body: attack(logger.origin) },
  });
  logger.hits.length = 0;
  const page = await browser.newPage();
  await page.goto(`${app.origin}/`);
  await page.waitForTimeout(2000);
  await page.close();
  await app.close();
  return [...new Set(logger.hits.map((h) => (h.split('?')[0] ?? '').slice(1)))].sort();
}

void test("the template's CSP is framework-web's PREVIEW_META_CSP", () => {
  const meta = /http-equiv="Content-Security-Policy"\s+content="([^"]*)"/.exec(
    readFileSync(TEMPLATE, 'utf8')
  );
  assert.equal(meta?.[1], PREVIEW_META_CSP);
});

void test('control: with no policy at all, every door reaches the other origin', async () => {
  const reached = await doorsReached(withoutMeta(templatePage()));
  // Whatever headless Chromium fetches at all must be visible to the logger;
  // the doors below are the ones the review found open, and all must show.
  for (const door of ['img', 'script.js', 'style.css', 'cssurl', 'font', 'prefetch', 'dynimport.js']) {
    assert.ok(reached.includes(door), `the control never saw ${door}: ${reached.join(',')}`);
  }
  assert.ok(reached.every((d) => DOORS.includes(d)), reached.join(','));
});

void test('the preview page alone, served with no headers, reaches nothing', async () => {
  assert.deepEqual(await doorsReached(templatePage()), []);
});

void test('the response headers alone reach nothing', async () => {
  assert.deepEqual(
    await doorsReached(withoutMeta(templatePage()), { ...PREVIEW_RESPONSE_HEADERS }),
    []
  );
});
