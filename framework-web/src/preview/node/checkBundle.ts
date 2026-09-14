/**
 * The production bundle must never ship the preview's fixture transport or its
 * network guard (design-renderer-data.md §2′.5 item 3). The production build goes
 * to dist/ (what an app's image and embeds ship); the preview build goes to
 * dist-preview/, which neither ships.
 *
 *   checkBundle('dist', 'production')        dist/ contains NO marker
 *   checkBundle('dist-preview', 'preview')   dist-preview/ contains EVERY marker
 *
 * The preview run is the POSITIVE CONTROL: it proves the markers survive
 * minification, so the production run cannot pass just because it looks for
 * something that never appears in any bundle.
 */
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { extname, join, relative } from 'node:path';
import { BUNDLE_MARKERS } from './markers.ts';

export type BundleMode = 'production' | 'preview';

export interface BundleCheck {
  readonly ok: boolean;
  readonly message: string;
}

const TEXT = new Set(['.js', '.mjs', '.cjs', '.html', '.css', '.json', '.map', '.txt']);

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    return statSync(path).isDirectory() ? walk(path) : [path];
  });
}

function notABuild(dir: string, files: readonly string[]): string | undefined {
  if (!files.some((f) => f.endsWith('index.html'))) {
    return `${dir}/ has no index.html; this is not a finished build`;
  }
  if (!files.some((f) => extname(f) === '.js')) {
    return `${dir}/ has no JavaScript; this is not a finished build`;
  }
  return undefined;
}

function markerHits(dir: string, files: readonly string[]): Map<string, string[]> {
  const hits = new Map(BUNDLE_MARKERS.map((m) => [m, [] as string[]]));
  for (const file of files) {
    if (!TEXT.has(extname(file))) continue;
    const text = readFileSync(file, 'utf8');
    for (const [marker, where] of hits) {
      if (text.includes(marker)) where.push(relative(dir, file));
    }
  }
  return hits;
}

export function checkBundle(dir: string, mode: BundleMode): BundleCheck {
  if (!existsSync(dir)) return { ok: false, message: `${dir}/ does not exist; build it first` };
  const files = walk(dir);
  const broken = notABuild(dir, files);
  if (broken !== undefined) return { ok: false, message: broken };
  const hits = markerHits(dir, files);
  if (mode === 'preview') {
    const missing = BUNDLE_MARKERS.filter((m) => hits.get(m)?.length === 0);
    return missing.length > 0
      ? {
          ok: false,
          message:
            `the preview bundle lacks ${missing.join(', ')}. The production check greps for ` +
            'these markers, so it would pass vacuously; keep them in the preview-only modules.',
        }
      : { ok: true, message: `${dir}/ carries every marker (${BUNDLE_MARKERS.join(', ')})` };
  }
  const shipped = BUNDLE_MARKERS.filter((m) => (hits.get(m)?.length ?? 0) > 0);
  return shipped.length > 0
    ? {
        ok: false,
        message:
          'the production bundle ships preview-only code:\n' +
          shipped.map((m) => `  ${m} in ${(hits.get(m) ?? []).join(', ')}`).join('\n') +
          '\nOnly the preview entry may import the fixture transport or the preview guards.',
      }
    : { ok: true, message: `${dir}/ (${String(files.length)} files) carries no preview marker` };
}
