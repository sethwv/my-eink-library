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
      { slug: 'branch-preview', path: 'branch/preview', label: 'dev/preview', category: 'doc-branch' },
      { slug: 'main', path: 'branch/main', label: 'Development (main)', category: 'development' },
    ], 'branch-preview');
    assert.match(selector, /Latest \(v0\.1\.0\)/);
    assert.match(selector, /<optgroup label="Releases">/);
    assert.match(selector, /<optgroup label="Branches">/);
    assert.match(selector, /value="\/branch\/preview\/"[^>]* selected/);
    assert.match(selector, /value="\/branch\/main\/"[^>]*>Development \(main\)<\/option>/);
    assert.ok(selector.indexOf('Releases') < selector.indexOf('Branches'));
    const sidebar = await readFile(path.join(temporary, 'output/_includes/components/sidebar.html'), 'utf8');
    assert.match(sidebar, /class="site-title lh-tight">\{% include title\.html %\}<\/a>/);
    const config = await readFile(path.join(temporary, 'output/_config.yml'), 'utf8');
    assert.match(config, /logo: "\/assets\/brand\/lockups\/horizontal-moss-transparent-text-plus-150\.png"/);
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
});

test('omits empty release and branch groups', async () => {
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'sideload-library-docs-'));
  try {
    const selector = await prepare(temporary, [
      { slug: 'main', path: 'branch/main', label: 'Development (main)', category: 'development' },
    ], 'main');
    assert.match(selector, /Development \(main\)/);
    assert.doesNotMatch(selector, /<optgroup/);
  } finally {
    await rm(temporary, { recursive: true, force: true });
  }
});
