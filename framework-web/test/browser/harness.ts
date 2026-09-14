/**
 * The browser-test harness: the preview runtime is proved in real Chromium
 * (Playwright), not a DOM emulation, because what it guards against is the
 * browser's behaviour: CSP enforcement, frames' own realms, popups and
 * navigations.
 *
 *   - an APP server on http://127.0.0.1:<port> serves the page under test and
 *     its bundled entry (esbuild, from the TypeScript sources);
 *   - a LOGGER on http://localhost:<port>, another origin, records every request
 *     that reaches it. A test that proves "nothing left the page" asserts the
 *     logger heard nothing, next to a positive control that it can hear.
 *
 * `npx playwright install chromium` provides the browser (CI does it).
 */
import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';
import { fileURLToPath } from 'node:url';
import { build } from 'esbuild';
import { chromium, type Browser } from 'playwright';

export const repoRoot = fileURLToPath(new URL('../../', import.meta.url));

/** Bundles a browser entry (TS/TSX) into one ES module. */
export async function bundle(entry: string): Promise<string> {
  const out = await build({
    entryPoints: [fileURLToPath(new URL(entry, import.meta.url))],
    bundle: true,
    format: 'esm',
    platform: 'browser',
    write: false,
    jsx: 'automatic',
    logLevel: 'silent',
    define: { 'process.env.NODE_ENV': '"production"' },
  });
  const file = out.outputFiles[0];
  if (file === undefined) throw new Error(`esbuild emitted nothing for ${entry}`);
  return file.text;
}

export interface Route {
  readonly type: string;
  readonly body: string;
  readonly headers?: Readonly<Record<string, string>>;
}

export interface Served {
  readonly origin: string;
  /** Every request path the server received, in order. */
  readonly hits: string[];
  close(): Promise<void>;
}

function listen(host: string, handler: (req: IncomingMessage, res: ServerResponse) => void): Promise<Server> {
  const server = createServer(handler);
  return new Promise((resolve) => {
    server.listen(0, host, () => {
      resolve(server);
    });
  });
}

function served(server: Server, host: string, hits: string[]): Served {
  const { port } = server.address() as AddressInfo;
  return {
    origin: `http://${host}:${String(port)}`,
    hits,
    close: () =>
      new Promise((resolve) => {
        server.closeAllConnections();
        server.close(() => {
          resolve();
        });
      }),
  };
}

/** The app server: each route by path (the query ignored); anything else 404s. */
export async function serveApp(routes: Readonly<Record<string, Route>>): Promise<Served> {
  const hits: string[] = [];
  const server = await listen('127.0.0.1', (req, res) => {
    const url = req.url ?? '/';
    hits.push(url);
    const route = routes[url.split('?')[0] ?? '/'];
    if (route === undefined) {
      res.writeHead(404).end();
      return;
    }
    res.writeHead(200, { 'Content-Type': route.type, ...route.headers }).end(route.body);
  });
  return served(server, '127.0.0.1', hits);
}

/** The logger, on another origin: answers everything, remembers everything. */
export async function serveLogger(): Promise<Served> {
  const hits: string[] = [];
  const server = await listen('localhost', (req, res) => {
    hits.push(req.url ?? '/');
    const path = req.url ?? '';
    const type = path.includes('.js')
      ? 'text/javascript'
      : path.includes('.css')
        ? 'text/css'
        : 'text/plain';
    res.writeHead(200, { 'Content-Type': type, 'Access-Control-Allow-Origin': '*' }).end('');
  });
  return served(server, 'localhost', hits);
}

export function launch(): Promise<Browser> {
  return chromium.launch();
}

export const html = 'text/html; charset=utf-8';
export const js = 'text/javascript; charset=utf-8';

/** A minimal page: a CSP <meta> (or none) and one module entry. */
export function page(csp: string | null, entry = '/entry.js'): string {
  const meta = csp === null ? '' : `<meta http-equiv="Content-Security-Policy" content="${csp}" />`;
  return (
    `<!doctype html><html><head><meta charset="UTF-8" />${meta}<title>t</title></head>` +
    `<body><div id="root"></div><script type="module" src="${entry}"></script></body></html>`
  );
}

export interface Incident {
  readonly kind: string;
  readonly detail: string;
}
