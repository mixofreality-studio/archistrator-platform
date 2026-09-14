/**
 * The preview guards in real Chromium, installed by the real side-effect entry
 * (`…/preview/install`). Each door is tried, and each must be refused (where a
 * page can refuse it), reported as an incident, and reach nothing: the logger,
 * on another origin, must hear nothing and no second page may open.
 */
import { after, before, test } from 'node:test';
import assert from 'node:assert/strict';
import type { Browser, Page } from 'playwright';
import { PREVIEW_META_CSP } from '../../src/preview/node/headers.ts';
import {
  bundle,
  html,
  js,
  launch,
  page as pageHtml,
  serveApp,
  serveLogger,
  type Incident,
  type Served,
} from './harness.ts';

let browser: Browser;
let app: Served;
let logger: Served;

before(async () => {
  const entry = await bundle('./guardsEntry.ts');
  app = await serveApp({
    '/': { type: html, body: pageHtml(PREVIEW_META_CSP) },
    '/entry.js': { type: js, body: entry },
  });
  logger = await serveLogger();
  browser = await launch();
});

after(async () => {
  await browser.close();
  await app.close();
  await logger.close();
});

interface Opened {
  readonly page: Page;
  readonly popups: string[];
}

async function open(): Promise<Opened> {
  const context = await browser.newContext();
  const page = await context.newPage();
  // Subscribed after the test's own page exists: any page from here on is one
  // the preview opened (a popup, a new tab).
  const popups: string[] = [];
  context.on('page', (p) => {
    popups.push(p.url());
  });
  await page.goto(`${app.origin}/?screen=a&state=b`);
  await page.waitForFunction(() => window.__ARCHISTRATOR_PREVIEW__ !== undefined);
  logger.hits.length = 0;
  return { page, popups };
}

function incidents(page: Page): Promise<Incident[]> {
  return page.evaluate(() => [...(window.__ARCHISTRATOR_PREVIEW__?.incidents ?? [])]);
}

async function settle(o: Opened): Promise<void> {
  await o.page.waitForTimeout(600);
  assert.deepEqual(o.popups, [], 'no second page may open');
  assert.deepEqual(logger.hits, [], 'nothing may reach the other origin');
  assert.equal(new URL(o.page.url()).origin, app.origin, 'the page never left its origin');
}

void test('every network channel of the page throws and is reported', async () => {
  const o = await open();
  const out = await o.page.evaluate(async (target) => {
    const tries: Record<string, () => unknown> = {
      fetch: () => fetch(`${target}/fetch`),
      xhr: () => new XMLHttpRequest(),
      ws: () => new WebSocket('ws://localhost:1/ws'),
      sse: () => new EventSource(`${target}/sse`),
      beacon: () => navigator.sendBeacon(`${target}/beacon`),
      beaconCall: () => Navigator.prototype.sendBeacon.call(navigator, `${target}/beacon2`),
      open: () => window.open(`${target}/popup`),
    };
    const result: Record<string, string> = {};
    for (const [name, attempt] of Object.entries(tries)) {
      try {
        await attempt();
        result[name] = 'sent';
      } catch (e) {
        result[name] = e instanceof Error ? e.name : String(e);
      }
    }
    return result;
  }, logger.origin);
  for (const [name, outcome] of Object.entries(out)) {
    assert.equal(outcome, 'PreviewNetworkBlockedError', `${name}: ${outcome}`);
  }
  const kinds = (await incidents(o.page)).map((i) => i.kind);
  assert.deepEqual(kinds, Array(7).fill('network-blocked'));
  await settle(o);
});

void test('a link that would open a new tab is refused and reported', async () => {
  const o = await open();
  await o.page.evaluate((target) => {
    const a = document.createElement('a');
    a.href = '/index.html?screen=a&state=b';
    a.target = '_blank';
    a.textContent = 'new tab';
    document.body.append(a);
    const b = document.createElement('a');
    b.href = `${target}/new-tab`;
    b.target = '_blank';
    b.textContent = 'away';
    document.body.append(b);
  }, logger.origin);
  await o.page.getByText('new tab').click();
  await o.page.getByText('away').click();
  await o.page.getByText('away').click({ modifiers: ['Meta'] });
  const got = await incidents(o.page);
  assert.deepEqual(
    got.map((i) => i.kind),
    ['navigation-blocked', 'navigation-blocked', 'navigation-blocked']
  );
  assert.ok(got[1]?.detail.endsWith('/new-tab'), JSON.stringify(got));
  await settle(o);
});

void test('a same-tab exit to another origin is refused and reported, however it is made', async () => {
  const attempts: Record<string, string> = {
    'link click': `const a = document.createElement('a'); a.href = T + '/link'; document.body.append(a); a.click();`,
    'location.href': `location.href = T + '/href';`,
    'location.assign': `location.assign(T + '/assign');`,
    'location.replace': `location.replace(T + '/replace');`,
    'meta refresh': `document.head.insertAdjacentHTML('beforeend', '<meta http-equiv="refresh" content="0;url=' + T + '/refresh">');`,
    'form get': `const f = document.createElement('form'); f.method = 'get'; f.action = T + '/form'; document.body.append(f); f.submit();`,
  };
  for (const [name, script] of Object.entries(attempts)) {
    const o = await open();
    await o.page.evaluate(`(() => { const T = ${JSON.stringify(logger.origin)}; ${script} })()`);
    await o.page.waitForTimeout(400);
    const got = await incidents(o.page);
    assert.ok(
      got.some((i) => i.kind === 'navigation-blocked' || i.kind === 'csp-violation'),
      `${name}: ${JSON.stringify(got)}`
    );
    await settle(o);
    await o.page.context().close();
  }
});

void test('a scripted reload is refused and reported; a same-origin state link is allowed', async () => {
  const o = await open();
  const loads = app.hits.filter((h) => h.startsWith('/?')).length;
  await o.page.evaluate(() => {
    location.reload();
  });
  await o.page.waitForTimeout(500);
  assert.equal(app.hits.filter((h) => h.startsWith('/?')).length, loads, 'the page reloaded');
  const got = await incidents(o.page);
  assert.equal(got[0]?.kind, 'navigation-blocked');
  assert.match(got[0]?.detail ?? '', /^reload /);

  await o.page.evaluate(() => {
    location.href = '?screen=c&state=d';
  });
  await o.page.waitForURL(/screen=c&state=d/);
  await settle(o);
});

void test('a blank frame is reported, and its window is guarded before a script can use it', async () => {
  const o = await open();
  const out = await o.page.evaluate(async (target) => {
    const result: Record<string, string> = {};
    const attempt = async (name: string, run: () => unknown): Promise<void> => {
      try {
        await run();
        result[name] = 'sent';
      } catch (e) {
        result[name] = e instanceof Error ? e.name : String(e);
      }
    };
    const f = document.createElement('iframe');
    document.body.appendChild(f);
    const w = f.contentWindow as (Window & typeof globalThis) | null;
    if (w === null) throw new Error('no contentWindow');
    await attempt('fetch', () => w.fetch(`${target}/f-fetch`));
    await attempt('xhr', () => new w.XMLHttpRequest());
    await attempt('ws', () => new w.WebSocket('ws://localhost:1/f-ws'));
    await attempt('beacon', () => w.navigator.sendBeacon(`${target}/f-beacon`));
    await attempt('beaconCall', () =>
      w.Navigator.prototype.sendBeacon.call(navigator, `${target}/f-beacon2`)
    );
    await attempt('open', () => w.open(`${target}/f-open`));

    // By index, after an innerHTML insertion: no contentWindow getter involved.
    const holder = document.createElement('div');
    document.body.append(holder);
    holder.innerHTML = '<iframe></iframe>';
    await attempt('indexOpen', () => (window[1] as Window).open(`${target}/i-open`));

    // Through a Range, then by index again.
    const range = document.createRange();
    range.selectNodeContents(document.body);
    range.insertNode(document.createElement('iframe'));
    await attempt('rangeFetch', () => (window[2] as Window).fetch(`${target}/r-fetch`));

    // A frame's own frame.
    const inner = w.document.createElement('iframe');
    w.document.body.appendChild(inner);
    await attempt('nestedOpen', () => (inner.contentWindow as Window).open(`${target}/n-open`));
    return result;
  }, logger.origin);
  for (const [name, outcome] of Object.entries(out)) {
    assert.equal(outcome, 'PreviewNetworkBlockedError', `${name}: ${outcome}`);
  }
  const got = await incidents(o.page);
  assert.equal(got.filter((i) => i.kind === 'frame-blocked').length, 4, JSON.stringify(got));
  assert.equal(got.filter((i) => i.kind === 'network-blocked').length, 9, JSON.stringify(got));
  await settle(o);
});

void test('whatever the CSP refuses is reported, never silent', async () => {
  const o = await open();
  await o.page.evaluate((target) => {
    const img = new Image();
    img.src = `${target}/img.png`;
    document.body.append(img);
  }, logger.origin);
  await o.page.waitForTimeout(300);
  const got = await incidents(o.page);
  assert.ok(
    got.some((i) => i.kind === 'csp-violation' && i.detail.startsWith('img-src ')),
    JSON.stringify(got)
  );
  await settle(o);
});
