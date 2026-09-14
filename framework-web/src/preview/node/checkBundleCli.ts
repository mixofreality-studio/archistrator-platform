#!/usr/bin/env node
/**
 * archistrator-preview-check-bundle <dir> [--preview]
 *
 * Without --preview: <dir> (the production build) must carry no preview marker.
 * With --preview: <dir> (the preview build) must carry every marker (the positive
 * control). Exits 1 on a failure, 2 on bad usage.
 */
import { resolve } from 'node:path';
import { checkBundle } from './checkBundle.ts';

const args = process.argv.slice(2);
const dir = args.find((a) => !a.startsWith('--'));
if (dir === undefined) {
  console.error('usage: archistrator-preview-check-bundle <dir> [--preview]');
  process.exit(2);
}
const result = checkBundle(resolve(dir), args.includes('--preview') ? 'preview' : 'production');
if (result.ok) {
  console.log(`check-prod-bundle: ok — ${result.message}`);
} else {
  console.error(`check-prod-bundle: FAIL — ${result.message}`);
  process.exit(1);
}
