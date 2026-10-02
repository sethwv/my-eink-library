import assert from 'node:assert/strict';
import { mkdtemp, mkdir, readdir, rm, writeFile } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';

import { branchArtifactSlug, documentationBranches, eligibleDocumentationVersions, prunePublishedSites, retainedVersions } from './version-retention.mjs';

const tags = ['v1.0.0', 'v1.0.1', 'v1.1.0', 'v1.1.1', 'v2.0.0', 'v2.0.1'];

test('minor mode retains all patches in the current minor and final older minors', () => {
  assert.deepEqual(retainedVersions(tags, 'minor'), ['v2.0.1', 'v2.0.0', 'v1.1.1', 'v1.0.1']);
});

test('major mode retains all releases in the current major and final older majors', () => {
  assert.deepEqual(retainedVersions(tags, 'major'), ['v2.0.1', 'v2.0.0', 'v1.1.1']);
});

test('excludes releases before the documentation cutoff', () => {
  assert.deepEqual(eligibleDocumentationVersions(['v0.0.1', 'v0.0.2', 'v0.0.3', 'v0.1.0']), ['v0.0.3', 'v0.1.0']);
});

test('allows a release-free documentation site', () => {
  assert.deepEqual(eligibleDocumentationVersions(['v0.0.1', 'v0.0.2']), []);
  assert.deepEqual(retainedVersions([], 'minor'), []);
});

test('discovers documentation branches and creates safe artifact slugs', () => {
  assert.deepEqual(documentationBranches([
    'deadbeef\trefs/heads/dev/redesign/navbar',
    'cafebabe\trefs/heads/main',
    'baddcafe\trefs/heads/dev/preview',
  ]), ['dev/preview', 'dev/redesign/navbar']);
  assert.equal(branchArtifactSlug('dev/redesign/navbar'), 'branch-ZGV2L3JlZGVzaWduL25hdmJhcg');
});

test('prunes obsolete published tag and branch directories', async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), 'sideload-library-docs-'));
  try {
    await Promise.all([
      'tag/v1.0.0',
      'tag/v1.0.1',
      'branch/preview',
      'branch/redesign/navbar',
      'main',
    ].map((name) => mkdir(path.join(root, name), { recursive: true })));
    await Promise.all([
      'tag/v1.0.0/index.html',
      'tag/v1.0.1/index.html',
      'branch/preview/index.html',
      'branch/redesign/navbar/index.html',
    ].map((name) => writeFile(path.join(root, name), 'site')));
    await prunePublishedSites(root, ['v1.0.1'], ['redesign/navbar']);
    assert.deepEqual((await readdir(path.join(root, 'tag'))).sort(), ['v1.0.1']);
    assert.deepEqual((await readdir(path.join(root, 'branch'))).sort(), ['redesign']);
    assert.deepEqual((await readdir(path.join(root, 'branch/redesign'))).sort(), ['navbar']);
    assert.deepEqual((await readdir(root)).sort(), ['branch', 'main', 'tag']);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
