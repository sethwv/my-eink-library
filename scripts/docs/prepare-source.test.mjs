import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { promisify } from 'node:util';

const run = promisify(execFile);
const root = path.resolve(import.meta.dirname, '../..');

async function prepare(temporary, versions, activeSlug) {
  await run(process.execPath, [
    path.join(root, 'scripts/docs/prepare-source.mjs'),
    path.join(root, 'docs'),
    path.join(temporary, 'output'),
    path.join(root, 'CONTRIBUTING.md'),
    path.join(root, 'CHANGELOG.md'),
    path.join(root, 'docs'),
    '',
    activeSlug,
    'main',
    JSON.stringify(versions),
  ]);
  return readFile(path.join(temporary, 'output/_includes/docs_version_switcher.html'), 'utf8');
}

test('renders release and documentation branch selector groups', async () => {
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'sideload-library-docs-'));
  try {
    const selector = await prepare(temporary, [
      { slug: 'latest', path: '', label: 'Latest (v0.1.0)', category: 'latest' },
      { slug: 'tag-v0.1.0', path: 'tag/v0.1.0', label: 'v0.1.0', category: 'release' },
      { slug: 'branch-preview', path: 'branch/preview', label: 'docs/preview', category: 'doc-branch' },
      { slug: 'main', path: 'main', label: 'Development (main)', category: 'development' },
    ], 'branch-preview');
    assert.match(selector, /Latest \(v0\.1\.0\)/);
    assert.match(selector, /<optgroup label="Releases">/);
    assert.match(selector, /<optgroup label="Doc Branches">/);
    assert.match(selector, /value="\/branch\/preview\/"[^>]* selected/);
    assert.ok(selector.indexOf('Releases') < selector.indexOf('Doc Branches'));
    await readFile(path.join(temporary, 'output/assets/brand/lockups/horizontal-moss-transparent.png'));
    const sidebar = await readFile(path.join(temporary, 'output/_includes/components/sidebar.html'), 'utf8');
    assert.match(sidebar, /class="site-title docs-sidebar-brand"/);
    assert.match(sidebar, /horizontal-moss-transparent\.png/);
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
});

test('omits empty release and branch groups', async () => {
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'sideload-library-docs-'));
  try {
    const selector = await prepare(temporary, [
      { slug: 'main', path: 'main', label: 'Development (main)', category: 'development' },
    ], 'main');
    assert.match(selector, /Development \(main\)/);
    assert.doesNotMatch(selector, /<optgroup/);
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
});
