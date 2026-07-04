// Self-test for the layer-boundary gate. Runs ESLint's node API over two fixture
// trees using a boundaries-only config built from the same ELEMENTS / BOUNDARY_RULES
// the factory ships. The valid tree must produce zero boundary errors; the invalid
// tree must produce exactly the intended violation on each file. This is the analogue
// of framework-go/arch's self-test. (Full-factory wiring — react/type-checked rules —
// is validated end-to-end when the webApp lints against the published config.)

import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { ESLint } from 'eslint';
import boundaries from 'eslint-plugin-boundaries';
import tseslint from 'typescript-eslint';
import { ELEMENTS, BOUNDARY_RULES } from './index.js';

const here = path.dirname(fileURLToPath(import.meta.url));

const boundaryConfig = {
  files: ['src/**/*.{ts,tsx}'],
  languageOptions: { parser: tseslint.parser },
  plugins: { boundaries },
  settings: {
    'import/resolver': { node: { extensions: ['.ts', '.tsx', '.js', '.jsx'] } },
    'boundaries/ignore': ['src/main.tsx', 'src/vite-env.d.ts'],
    'boundaries/elements': ELEMENTS,
  },
  rules: {
    'boundaries/dependencies': ['error', { default: 'disallow', rules: BOUNDARY_RULES }],
    'boundaries/no-unknown': 'error',
    'boundaries/no-unknown-files': 'error',
  },
};

/** @returns {Promise<Map<string, Set<string>>>} basename -> set of boundary ruleIds */
async function lintTree(dir) {
  const eslint = new ESLint({
    cwd: path.join(here, 'fixtures', dir),
    overrideConfigFile: true,
    overrideConfig: [boundaryConfig],
  });
  const results = await eslint.lintFiles(['src/**/*.{ts,tsx}']);
  const byFile = new Map();
  for (const r of results) {
    for (const m of r.messages) {
      if (m.ruleId && m.ruleId.startsWith('boundaries/')) {
        const key = path.basename(path.dirname(r.filePath)) + '/' + path.basename(r.filePath);
        if (!byFile.has(key)) byFile.set(key, new Set());
        byFile.get(key).add(m.ruleId);
      }
    }
  }
  return byFile;
}

const failures = [];
function expect(cond, msg) {
  if (!cond) failures.push(msg);
}

const valid = await lintTree('valid');
expect(valid.size === 0, `valid tree should have 0 boundary errors, got: ${JSON.stringify([...valid], (_k, v) => (v instanceof Set ? [...v] : v))}`);

const invalid = await lintTree('invalid');
const has = (file, rule) => invalid.get(file)?.has(rule) ?? false;
expect(has('api/client.ts', 'boundaries/dependencies'), 'api -> hooks (upward) should be flagged');
expect(has('hooks/useThing.ts', 'boundaries/dependencies'), 'hooks -> components (upward) should be flagged');
expect(has('components/Card.ts', 'boundaries/dependencies'), 'components -> api should be flagged');
expect(has('routes/Home.ts', 'boundaries/dependencies'), 'routes -> api should be flagged');
expect(has('misc/orphan.ts', 'boundaries/no-unknown-files'), 'unclassified file should be flagged');

if (failures.length > 0) {
  console.error('FAIL:\n' + failures.map((f) => '  - ' + f).join('\n'));
  console.error('\ninvalid tree boundary errors:', JSON.stringify([...invalid], (_k, v) => (v instanceof Set ? [...v] : v), 2));
  process.exit(1);
}
console.log('PASS: boundary gate fires correctly on all fixtures (valid clean, invalid flagged).');
