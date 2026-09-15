import { cp, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";

const [source, destination, contributing] = process.argv.slice(2);
if (!source || !destination || !contributing) {
  throw new Error("usage: prepare-source.mjs <docs-source> <destination> <contributing-source>");
}

await rm(destination, { force: true, recursive: true });
await cp(source, destination, { recursive: true });

const frontMatter = "---\ntitle: Contributing\nnav_order: 8\ngh_edit_link: false\n---\n\n";
const contributionGuide = await readFile(contributing, "utf8");
await writeFile(path.join(destination, "contributing.md"), frontMatter + contributionGuide);
