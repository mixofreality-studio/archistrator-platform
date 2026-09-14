/**
 * Validates a tree of preview fixtures against the fixture JSON Schema webgen
 * generates from the server OAS (preview/fixtures.schema.json).
 *
 * The layout is `<root>/<surface>/<screen>/<state>.json`, screen and state
 * kebab-case. Every file must be JSON, pass the schema, and carry no bundle
 * marker (markers.ts). A missing `<root>/<surface>` is not an error: it is a
 * build with no states.
 *
 * The schema is read the way the app's TypeScript reads the OAS (webgen already
 * rewrote refs and oneOf for that), and unknown OAS formats such as int64 are
 * ignored rather than failing the compile.
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';
import Ajv from 'ajv';
import type { ErrorObject } from 'ajv';
import { BUNDLE_MARKERS } from './markers.ts';

/** A compiled fixture validator; after a failed call, `errors` says why. */
export interface FixtureValidator {
  (data: unknown): boolean;
  errors?: ErrorObject[] | null;
}

export function compileFixtureValidator(schema: unknown): FixtureValidator {
  const ajv = new Ajv({ allErrors: true, jsonPointers: true, unknownFormats: 'ignore' });
  const validate = ajv.compile(schema as object);
  const check: FixtureValidator = (data: unknown): boolean => {
    const ok = validate(data) === true;
    check.errors = validate.errors ?? null;
    return ok;
  };
  return check;
}

export interface FixtureTreeOptions {
  readonly surface: string;
  readonly validate: FixtureValidator;
}

export interface FixtureTreeResult {
  readonly files: readonly string[];
  readonly errors: readonly string[];
}

const KEBAB = /^[a-z0-9]+(-[a-z0-9]+)*$/;
const MAX_ERRORS_PER_FILE = 12;

function describeErrors(errors: readonly ErrorObject[] | null | undefined): string[] {
  const lines = (errors ?? []).map(
    (e) => `${e.dataPath === '' ? '/' : e.dataPath} ${e.message ?? 'is invalid'}`
  );
  const unique = [...new Set(lines)];
  return unique.length > MAX_ERRORS_PER_FILE
    ? [...unique.slice(0, MAX_ERRORS_PER_FILE), `…and ${String(unique.length - MAX_ERRORS_PER_FILE)} more`]
    : unique;
}

/** The problems with one state file; [] when it passes. */
function checkStateFile(path: string, rel: string, validate: FixtureValidator): string[] {
  const text = readFileSync(path, 'utf8');
  // Fixtures are bundled into the preview: a bundle marker in their text would
  // satisfy the bundle check's positive control on its own.
  const marker = BUNDLE_MARKERS.find((m) => text.includes(m));
  if (marker !== undefined) {
    return [
      `${rel}: contains the bundle marker "${marker}". Fixture data must not ` +
        '(it would mask the preview bundle check)',
    ];
  }
  let data: unknown;
  try {
    data = JSON.parse(text);
  } catch (err) {
    return [`${rel}: not JSON (${err instanceof Error ? err.message : String(err)})`];
  }
  return validate(data) ? [] : describeErrors(validate.errors).map((line) => `${rel}: ${line}`);
}

function stateFiles(screenDir: string, rel: (p: string) => string, errors: string[]): string[] {
  const files: string[] = [];
  for (const name of readdirSync(screenDir).sort()) {
    const path = join(screenDir, name);
    const state = name.replace(/\.json$/, '');
    if (!name.endsWith('.json') || !statSync(path).isFile() || !KEBAB.test(state)) {
      errors.push(`${rel(path)}: expected a kebab <state>.json file`);
      continue;
    }
    files.push(path);
  }
  return files;
}

export function validateFixtureTree(root: string, options: FixtureTreeOptions): FixtureTreeResult {
  const files: string[] = [];
  const errors: string[] = [];
  const dir = join(root, options.surface);
  if (!existsSync(dir)) return { files, errors };
  const rel = (p: string): string => relative(root, p);
  for (const screen of readdirSync(dir).sort()) {
    const screenDir = join(dir, screen);
    if (!statSync(screenDir).isDirectory() || !KEBAB.test(screen)) {
      errors.push(`${rel(screenDir)}: expected a kebab <screen> directory under ${options.surface}/`);
      continue;
    }
    for (const path of stateFiles(screenDir, rel, errors)) {
      files.push(path);
      errors.push(...checkStateFile(path, rel(path), options.validate));
    }
  }
  return { files, errors };
}
