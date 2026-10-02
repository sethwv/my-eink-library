import assert from 'node:assert/strict';
import { mkdtemp, mkdir, readdir, rm } from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';

import { eligibleDocumentationVersions, prunePublishedVersions, retainedVersions } from './version-retention.mjs';

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

test('prunes obsolete published version directories', async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), 'sideload-library-docs-'));
  try {
    await Promise.all(['v1.0.0', 'v1.0.1', 'v2.0.0', 'main'].map((name) => mkdir(path.join(root, name))));
    await prunePublishedVersions(root, ['v1.0.1', 'v2.0.0']);
    assert.deepEqual((await readdir(root)).sort(), ['main', 'v1.0.1', 'v2.0.0']);
  } finally {
    await rm(root, { recursive: true, force: true });
  }
});
