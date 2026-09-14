/**
 * The preview build's Vite plugin (preview/node/vitePlugin.ts), run through a
 * REAL `vite build`: an invalid fixture fails the build, and so does a
 * preview.html whose CSP is not the preview CSP, or a build without one.
 */
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdirSync, mkdtempSync, readdirSync, realpathSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { build } from 'vite';
import { PREVIEW_META_CSP } from './headers.ts';
import { previewFixtures } from './vitePlugin.ts';

const schema = {
  $schema: 'http://json-schema.org/draft-07/schema#',
  type: 'object',
  required: ['route', 'ops'],
  additionalProperties: false,
  properties: {
    route: { type: 'string', pattern: '^/' },
    ops: {
      type: 'object',
      additionalProperties: false,
      properties: { getThing: { type: 'object', required: ['result'] } },
    },
  },
};

const page = (csp: string): string =>
  '<!doctype html><html><head><meta charset="UTF-8" />' +
  `<meta http-equiv="Content-Security-Policy" content="${csp}" />` +
  '<title>t</title></head><body><script type="module" src="./entry.js"></script></body></html>';

interface Project {
  readonly root: string;
  readonly outDir: string;
  readonly entryHtml: string;
}

function project(fixture: unknown, html = page(PREVIEW_META_CSP), entryName = 'preview.html'): Project {
  // realpath: macOS's tmpdir is a symlink, and Vite wants the input under its root.
  const root = realpathSync(mkdtempSync(join(tmpdir(), 'preview-plugin-')));
  writeFileSync(join(root, entryName), html);
  writeFileSync(join(root, 'entry.js'), 'document.title = "built";\n');
  writeFileSync(join(root, 'fixtures.schema.json'), JSON.stringify(schema));
  mkdirSync(join(root, 'fixtures', 'web-client', 'home'), { recursive: true });
  writeFileSync(
    join(root, 'fixtures', 'web-client', 'home', 'resting.json'),
    JSON.stringify(fixture)
  );
  return { root, outDir: join(root, 'dist-preview'), entryHtml: join(root, entryName) };
}

async function buildPreview(p: Project): Promise<void> {
  await build({
    root: p.root,
    configFile: false,
    logLevel: 'silent',
    base: './',
    plugins: [
      previewFixtures({
        fixturesRoot: join(p.root, 'fixtures'),
        surface: 'web-client',
        schemaPath: join(p.root, 'fixtures.schema.json'),
      }),
    ],
    build: { outDir: p.outDir, emptyOutDir: true, rollupOptions: { input: p.entryHtml } },
  });
}

const valid = { route: '/', ops: { getThing: { result: {} } } };

void test('a valid preview builds: index.html and the schema, never preview.html', async () => {
  const p = project(valid);
  await buildPreview(p);
  const out = readdirSync(p.outDir).sort();
  assert.ok(out.includes('index.html'), out.join(','));
  assert.ok(out.includes('fixtures.schema.json'), out.join(','));
  assert.ok(!out.includes('preview.html'), out.join(','));
});

void test('one invalid fixture fails the build', async () => {
  await assert.rejects(
    buildPreview(project({ route: 'no-slash', ops: {} })),
    /preview fixtures fail the OAS-generated schema/
  );
  await assert.rejects(
    buildPreview(project({ route: '/', ops: { getThing: {}, notAnOp: { result: 1 } } })),
    /preview fixtures fail the OAS-generated schema/
  );
});

void test('a preview.html whose CSP is not the preview CSP fails the build', async () => {
  const weak = "connect-src 'none'; frame-src 'none'; worker-src 'none'";
  await assert.rejects(buildPreview(project(valid, page(weak))), /must carry the preview CSP/);
  await assert.rejects(
    buildPreview(project(valid, page(PREVIEW_META_CSP).replace(/<meta http-equiv[^>]*>/, ''))),
    /exactly one Content-Security-Policy <meta>, found 0/
  );
});

void test('a preview build without preview.html fails', async () => {
  await assert.rejects(
    buildPreview(project(valid, page(PREVIEW_META_CSP), 'other.html')),
    /emitted no preview.html/
  );
});
