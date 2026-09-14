/**
 * The response headers a preview bundle is served with (design-renderer-data.md
 * §2′.3), for `vite preview` over a preview build. frame-ancestors is 'none'
 * here; the Go preview listener names the embedding app's public origin instead.
 * `style-src 'unsafe-inline'` is for emotion/MUI; `connect-src 'none'` is safe
 * because fixtures are bundled.
 */
export const PREVIEW_CSP = [
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
  "frame-ancestors 'none'",
].join('; ');

export const PREVIEW_RESPONSE_HEADERS: Readonly<Record<string, string>> = {
  'Content-Security-Policy': PREVIEW_CSP,
  'Referrer-Policy': 'no-referrer',
  'X-Content-Type-Options': 'nosniff',
  'Cross-Origin-Resource-Policy': 'same-origin',
};
