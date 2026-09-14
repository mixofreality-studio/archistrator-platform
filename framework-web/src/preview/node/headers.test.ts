/**
 * The preview's CSP and response headers (preview/node/headers.ts), pinned to
 * the §2′.3 table directive by directive. A loosened directive fails here; the
 * browser tests (test/browser/) prove the same policy keeps the network closed.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  PREVIEW_CSP,
  PREVIEW_META_CSP,
  PREVIEW_PERMISSIONS_POLICY,
  PREVIEW_RESPONSE_HEADERS,
  previewPageCspProblem,
} from './headers.ts';

const DOCUMENT_POLICY: Readonly<Record<string, string>> = {
  'default-src': "'self'",
  'script-src': "'self'",
  'style-src': "'self' 'unsafe-inline'",
  'img-src': "'self' data: blob:",
  'font-src': "'self' data:",
  'connect-src': "'none'",
  'form-action': "'none'",
  'base-uri': "'none'",
  'object-src': "'none'",
  'frame-src': "'none'",
  'worker-src': "'none'",
};

function directives(csp: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const part of csp.split(';')) {
    const [name, ...sources] = part.trim().split(/\s+/);
    if (name === undefined || name === '') continue;
    assert.equal(out[name], undefined, `directive ${name} appears twice`);
    out[name] = sources.join(' ');
  }
  return out;
}

void test('the page CSP is exactly the §2′.3 policy less frame-ancestors', () => {
  assert.deepEqual(directives(PREVIEW_META_CSP), DOCUMENT_POLICY);
});

void test('the response CSP is the page CSP plus frame-ancestors', () => {
  assert.deepEqual(directives(PREVIEW_CSP), {
    ...DOCUMENT_POLICY,
    'frame-ancestors': "'none'",
  });
  assert.equal(PREVIEW_RESPONSE_HEADERS['Content-Security-Policy'], PREVIEW_CSP);
});

void test('the response headers deny powerful features and leak nothing', () => {
  assert.equal(
    PREVIEW_PERMISSIONS_POLICY,
    'camera=(), microphone=(), geolocation=(), payment=(), usb=()'
  );
  assert.deepEqual(PREVIEW_RESPONSE_HEADERS, {
    'Content-Security-Policy': PREVIEW_CSP,
    'Permissions-Policy': PREVIEW_PERMISSIONS_POLICY,
    'Referrer-Policy': 'no-referrer',
    'X-Content-Type-Options': 'nosniff',
    'Cross-Origin-Resource-Policy': 'same-origin',
  });
});

const page = (head: string): string =>
  `<!doctype html><html><head><meta charset="UTF-8" />${head}<title>t</title></head>` +
  '<body><div id="root"></div><script type="module" src="./x.js"></script></body></html>';
const meta = (csp: string): string =>
  `<meta\n      http-equiv="Content-Security-Policy"\n      content="${csp}"\n    />`;

void test('a preview page must carry exactly the page CSP, ahead of every load', () => {
  assert.equal(previewPageCspProblem(page(meta(PREVIEW_META_CSP))), undefined);
  assert.match(previewPageCspProblem(page('')) ?? '', /exactly one .* found 0/);
  assert.match(
    previewPageCspProblem(page(meta(PREVIEW_META_CSP) + meta(PREVIEW_META_CSP))) ?? '',
    /found 2/
  );
  const loosened = PREVIEW_META_CSP.replace("img-src 'self' data: blob:", 'img-src *');
  assert.match(previewPageCspProblem(page(meta(loosened))) ?? '', /must carry the preview CSP/);
  assert.match(
    previewPageCspProblem(page(meta("connect-src 'none'; frame-src 'none'"))) ?? '',
    /must carry the preview CSP/
  );
  assert.match(
    previewPageCspProblem(page('<script src="./early.js"></script>' + meta(PREVIEW_META_CSP))) ??
      '',
    /ahead of every script/
  );
});
