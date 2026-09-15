import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parse } from "yaml";

const catalogURL = new URL("./library.yml", import.meta.url);
const catalog = parse(await readFile(catalogURL, "utf8"));
const directory = path.join(path.dirname(fileURLToPath(catalogURL)), "covers");
await mkdir(directory, { recursive: true });

for (const [name, source] of Object.entries(catalog.cover_sources)) {
  const response = await fetch(source);
  if (!response.ok) throw new Error(`download ${name}: ${response.status}`);
  if (!response.headers.get("content-type")?.startsWith("image/")) throw new Error(`download ${name}: expected image content`);
  await writeFile(path.join(directory, catalog.covers[name]), Buffer.from(await response.arrayBuffer()));
}
