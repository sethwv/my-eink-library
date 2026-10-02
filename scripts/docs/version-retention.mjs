function parseVersion(tag) {
  const match = /^v(\d+)\.(\d+)\.(\d+)$/.exec(tag);
  if (!match) throw new Error(`invalid release tag: ${tag}`);
  return match.slice(1).map(Number);
}

function compareTags(first, second) {
  const left = parseVersion(first);
  const right = parseVersion(second);
  for (const [index, part] of left.entries()) {
    if (part !== right[index]) return right[index] - part;
  }
  return 0;
}

export const minimumDocumentationTag = 'v0.0.3';
export const documentationBranchPrefix = 'dev/docs/';

export function eligibleDocumentationVersions(tags) {
  return tags.filter((tag) => compareTags(tag, minimumDocumentationTag) <= 0);
}

export function documentationBranches(remoteRefs) {
  return remoteRefs
    .map((line) => line.trim().split(/\s+/).at(-1))
    .filter((ref) => ref?.startsWith(`refs/heads/${documentationBranchPrefix}`))
    .map((ref) => ref.slice('refs/heads/'.length))
    .sort((first, second) => first.localeCompare(second));
}

export function branchArtifactSlug(ref) {
  return `branch-${Buffer.from(ref).toString('base64url')}`;
}

// retainedVersions returns tags newest first. The current release line keeps
// every patch; historical lines collapse to their final release.
export function retainedVersions(tags, mode) {
  if (!['minor', 'major'].includes(mode)) throw new Error(`unknown documentation retention mode: ${mode}`);
  const sorted = [...tags].sort(compareTags);
  if (!sorted.length) return [];
  const current = parseVersion(sorted[0]);
  const seen = new Set();

  return sorted.filter((tag) => {
    const version = parseVersion(tag);
    const currentLine = mode === 'minor'
      ? version[0] === current[0] && version[1] === current[1]
      : version[0] === current[0];
    if (currentLine) return true;

    const line = mode === 'minor' ? `${version[0]}.${version[1]}` : `${version[0]}`;
    if (seen.has(line)) return false;
    seen.add(line);
    return true;
  });
}

export async function prunePublishedSites(root, retainedTags, retainedBranches) {
  await Promise.all([
    pruneNamespace(path.join(root, 'tag'), retainedTags),
    pruneNamespace(path.join(root, 'branch'), retainedBranches),
  ]);
}

async function pruneNamespace(root, retained) {
  const retainedSet = new Set(retained);
  let entries;
  try {
    entries = await readdir(root, { withFileTypes: true });
  } catch (error) {
    if (error.code === 'ENOENT') return;
    throw error;
  }

  for (const entry of entries) {
    if (!entry.isDirectory()) continue;
    await pruneNamespaceEntry(root, entry.name, retainedSet);
  }
}

async function pruneNamespaceEntry(root, relativePath, retained) {
  const directory = path.join(root, relativePath);
  const entries = await readdir(directory, { withFileTypes: true });
  if (entries.some((entry) => entry.isFile() && entry.name === 'index.html')) {
    if (!retained.has(relativePath)) await rm(directory, { recursive: true, force: true });
    return;
  }

  for (const entry of entries) {
    if (entry.isDirectory()) await pruneNamespaceEntry(root, path.join(relativePath, entry.name), retained);
  }
}
import { readdir, rm } from 'node:fs/promises';
import path from 'node:path';
