# @mixofreality-studio/archistrator-platform-eslint-config-web

Reusable strict ESLint flat config **+ a layered import-boundary gate** for
archistrator TS web apps. The TS counterpart to the Go `framework-go/arch` analyzer:
one shared artifact that gives every app identical strictness and enforces the same
architectural discipline mechanically.

## What it enforces

**Code-quality baseline** (bundled, one import): `@eslint/js` recommended,
`typescript-eslint` strictTypeChecked + stylisticTypeChecked, React + hooks + refresh,
`jsx-a11y` strict, Prettier compatibility, plus the archistrator custom rule set
(`no-explicit-any`, `explicit-function-return-type`, `strict-boolean-expressions`,
`switch-exhaustiveness-check`, …).

**Layered architecture** (via `eslint-plugin-boundaries`). Dumb views; all business
logic lives on the server. Six element types, downward-only imports:

| Layer        | Folder            | May import                                     |
|--------------|-------------------|------------------------------------------------|
| `routes`     | `src/routes/` (+ `src/App.tsx`) | components, hooks, contracts, utilities |
| `components` | `src/components/` | components, hooks, contracts, utilities        |
| `hooks`      | `src/hooks/`      | hooks, **api**, contracts, utilities           |
| `api`        | `src/api/`        | contracts, utilities  *(the IO client only)*   |
| `contracts`  | `src/contracts/`  | contracts  *(pure data types/adapters; leaf)*  |
| `utilities`  | `src/utilities/`  | utilities, contracts  *(cross-cutting; leaf)*  |

Rules, all `error`:
- **Only `hooks` may import `api`** (the IO client). Routes/components get data via
  hooks or props — the "dumb views" guarantee.
- **No upward imports** (`hooks` → `components`/`routes` is rejected, etc.).
- **Sideways allowed within a layer** (components compose sibling components).
- **Everything must classify** — a file outside a known layer fails lint
  (`boundaries/no-unknown-files`), mirroring the Go analyzer's "no misc bucket". Only
  `src/main.tsx` and `src/vite-env.d.ts` are exempt.

## Usage

```js
// app eslint.config.js
import archWeb from '@mixofreality-studio/archistrator-platform-eslint-config-web';

export default archWeb({
  tsconfigRootDir: import.meta.dirname,
  ignores: ['src/contracts/schema.ts'], // generated files
});
```

Any app using the canonical `src/<layer>` layout needs no boundary configuration.

## Self-test

`npm test` runs the boundary gate over `fixtures/valid` (must be clean) and
`fixtures/invalid` (each file must be flagged). Full-factory wiring (React +
type-checked rules) is validated end-to-end when a consuming app lints against it.
