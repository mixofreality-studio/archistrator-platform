import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import {
  PREVIEW_RESPONSE_HEADERS,
  previewFixtures,
} from '@mixofreality-studio/archistrator-platform-framework-web/preview/node';

// The PREVIEW build (design-renderer-data.md §2′.1): the same <App/> as the SPA,
// booted by src/previewShell/main.tsx over the fixture transport, with
// ?screen=<id>&state=<id> choosing the fixture. A sibling of the production config.
//
//   - `base: './'`, so the bundle can be served from any origin root or path prefix.
//   - outDir `dist-preview/`, NEVER `dist/`: production ships dist/ only, and the
//     production build asserts dist/ carries no preview code
//     (archistrator-preview-check-bundle dist).
//   - Fixtures are bundled at build time (import.meta.glob through the
//     `@preview-fixtures` alias), so a preview needs no fetch. The root is
//     ARCHISTRATOR_PREVIEW_FIXTURES (relative to the webApp), defaulting to preview/fixtures,
//     where the frontend activity's Design phase records the designed fixtures.
//   - Every fixture is validated against preview/fixtures.schema.json (generated
//     from the server OAS by webgen) when the build starts; one invalid fixture
//     fails the build. The schema ships beside the bundle.
//
// Serve a build with `npx vite preview -c vite.preview.config.ts`. `vite dev` over
// this config is not a supported target: the preview's network guard refuses the
// Vite client's HMR WebSocket, as it refuses every other connection.
//
// Scaffolded by framework-go-app-generator/webgen; the app's own code from here on.

const here = dirname(fileURLToPath(import.meta.url));
const fixturesEnv = process.env['ARCHISTRATOR_PREVIEW_FIXTURES'];
const fixturesRoot = resolve(here, fixturesEnv ?? 'preview/fixtures');

export default defineConfig({
  base: './',
  plugins: [
    react(),
    previewFixtures({
      fixturesRoot,
      surface: 'web-client',
      schemaPath: resolve(here, 'preview/fixtures.schema.json'),
    }),
  ],
  resolve: { alias: { '@preview-fixtures': fixturesRoot } },
  build: {
    outDir: 'dist-preview',
    emptyOutDir: true,
    rollupOptions: { input: resolve(here, 'preview.html') },
  },
  preview: { headers: PREVIEW_RESPONSE_HEADERS },
});
