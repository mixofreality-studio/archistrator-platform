// Builds dist/host.js, the bundle the Go package embeds. Its first line records the sha256 of the
// sources (src/host.ts, index.html, package-lock.json, in that order); mcpapphost_test.go re-checks
// it with no node, so a changed source with a stale bundle fails the test.
//
//   npm ci && node build.mjs
import { createHash } from 'node:crypto';
import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { build } from 'esbuild';

const here = dirname(fileURLToPath(import.meta.url));
const sources = ['src/host.ts', 'index.html', 'package-lock.json'];
const hash = createHash('sha256');
for (const name of sources) hash.update(readFileSync(resolve(here, name)));

const result = await build({
  entryPoints: [resolve(here, 'src/host.ts')],
  bundle: true,
  format: 'iife',
  platform: 'browser',
  target: 'es2022',
  minify: true,
  legalComments: 'none',
  write: false,
  logLevel: 'warning',
});
const out = result.outputFiles[0];
if (out === undefined) throw new Error('esbuild produced no output');
writeFileSync(resolve(here, 'dist/host.js'), `// source-sha256: ${hash.digest('hex')}\n${out.text}`);
