/**
 * The preview's Content-Security-Policy and response headers (the §2′.3 table of
 * the preview design).
 *
 *   - PREVIEW_META_CSP is what a preview page carries in its own
 *     `<meta http-equiv="Content-Security-Policy">` (webgen's preview.html). It
 *     is the WHOLE policy less frame-ancestors, which a meta cannot carry, so the
 *     page alone, served with no headers at all, fetches nothing from another
 *     origin: no request, image, script, style, font, prefetch, worker or frame.
 *     The previewFixtures build plugin fails a build whose page differs.
 *   - PREVIEW_CSP adds frame-ancestors for the response header. It is 'none'
 *     here, for `vite preview`; the Go preview listener names the embedding
 *     app's public origin instead.
 *
 * `style-src 'unsafe-inline'` is for emotion/MUI; `connect-src 'none'` is safe
 * because fixtures are bundled. No CSP directive governs navigation: see
 * navigationGuard.ts for what a preview does about a same-tab exit.
 */
const DOCUMENT_DIRECTIVES = [
  "default-src 'self'",
  "script-src 'self'",
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data: blob:",
  "font-src 'self' data:",
  "connect-src 'none'",
  "form-action 'none'",
  "base-uri 'none'",
  "object-src 'none'",
  "frame-src 'none'",
  "worker-src 'none'",
];

export const PREVIEW_META_CSP = DOCUMENT_DIRECTIVES.join('; ');

export const PREVIEW_CSP = [...DOCUMENT_DIRECTIVES, "frame-ancestors 'none'"].join('; ');

/** Denies the powerful features a preview never needs (§2′.3). */
export const PREVIEW_PERMISSIONS_POLICY =
  'camera=(), microphone=(), geolocation=(), payment=(), usb=()';

export const PREVIEW_RESPONSE_HEADERS: Readonly<Record<string, string>> = {
  'Content-Security-Policy': PREVIEW_CSP,
  'Permissions-Policy': PREVIEW_PERMISSIONS_POLICY,
  'Referrer-Policy': 'no-referrer',
  'X-Content-Type-Options': 'nosniff',
  'Cross-Origin-Resource-Policy': 'same-origin',
};

const CSP_META = /<meta\b[^>]*\bhttp-equiv\s*=\s*["']?Content-Security-Policy["']?[^>]*>/gi;

/**
 * What is wrong with a preview page's own CSP, or undefined when it carries
 * exactly one CSP `<meta>`, equal to PREVIEW_META_CSP, ahead of every script,
 * stylesheet and image. The previewFixtures build plugin fails a build on it:
 * the scaffolded preview.html is the app's own file afterwards, and a loosened
 * policy there would open the page whenever it is served without the headers.
 */
export function previewPageCspProblem(html: string): string | undefined {
  const metas = [...html.matchAll(CSP_META)];
  const meta = metas[0];
  if (metas.length !== 1 || meta === undefined) {
    return `must carry exactly one Content-Security-Policy <meta>, found ${String(metas.length)}`;
  }
  const content = /\bcontent\s*=\s*(?:"([^"]*)"|'([^']*)')/i.exec(meta[0]);
  const csp = (content?.[1] ?? content?.[2] ?? '').replace(/\s+/g, ' ').trim();
  if (csp !== PREVIEW_META_CSP) {
    return `must carry the preview CSP "${PREVIEW_META_CSP}" in its <meta>, found "${csp}"`;
  }
  const firstLoad = html.search(/<(script|link|style|img)\b/i);
  if (firstLoad !== -1 && firstLoad < meta.index) {
    return 'must put its CSP <meta> ahead of every script, stylesheet and image';
  }
  return undefined;
}
