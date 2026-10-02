import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { promisify } from 'node:util';

const run = promisify(execFile);
const root = path.resolve(import.meta.dirname, '../..');

test('omits the releases group when main is the only documentation version', async () => {
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'sideload-library-docs-'));
  try {
    const manifest = JSON.stringify([{ slug: 'main', path: 'main', label: 'Development (main)' }]);
    await run(process.execPath, [
      path.join(root, 'scripts/docs/prepare-source.mjs'),
      path.join(root, 'docs'),
      path.join(temporary, 'output'),
      path.join(root, 'CONTRIBUTING.md'),
      path.join(root, 'CHANGELOG.md'),
      path.join(root, 'docs'),
      '',
      'main',
      'main',
      manifest,
    ]);

    const selector = await readFile(path.join(temporary, 'output/_includes/docs_version_switcher.html'), 'utf8');
    assert.match(selector, /Development \(main\)/);
    assert.doesNotMatch(selector, /<optgroup label="Releases">/);
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
});
