import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const migrationDirectory = fileURLToPath(new URL("./capture-compat/migrations/", import.meta.url));

export async function loadCaptureSpec(root, version) {
	const spec = { root, version, scenarios: null };
  const migration = (await loadMigrations()).find(({ upTo }) => isAtOrBefore(version, upTo));
  if (migration) await migration.apply(spec);
	if (!spec.scenarios) {
		const { parse } = await import("yaml");
		spec.scenarios = parse(await readFile(path.join(root, "docs/_data/screenshots.yml"), "utf8"));
  }
  return spec;
}

async function loadMigrations() {
  const files = await readdir(migrationDirectory);
  const migrations = await Promise.all(files.filter((file) => file.endsWith(".mjs")).map(async (file) => {
    const migration = await import(pathToFileURL(path.join(migrationDirectory, file)).href);
    if (!migration.upTo || typeof migration.apply !== "function") {
      throw new Error(`Invalid capture compatibility migration: ${file}`);
    }
    return migration;
  }));
  return migrations.sort((first, second) => compareVersions(first.upTo, second.upTo));
}

function isAtOrBefore(version, threshold) {
  return compareVersions(version, threshold) <= 0;
}

function compareVersions(first, second) {
  const parseVersion = (value) => /^v(\d+)\.(\d+)\.(\d+)$/.exec(value)?.slice(1).map(Number);
  const left = parseVersion(first);
  const right = parseVersion(second);
  if (!left || !right) return Number.NaN;
  for (const [index, part] of left.entries()) {
    if (part !== right[index]) return part - right[index];
  }
  return 0;
}
