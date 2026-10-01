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

export async function prunePublishedVersions(root, retained) {
  const entries = await readdir(root, { withFileTypes: true });
  const retainedSet = new Set(retained);
  await Promise.all(entries
    .filter((entry) => entry.isDirectory() && entry.name.startsWith('v') && !retainedSet.has(entry.name))
    .map((entry) => rm(path.join(root, entry.name), { recursive: true, force: true })));
}
import { readdir, rm } from 'node:fs/promises';
import path from 'node:path';
