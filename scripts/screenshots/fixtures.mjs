import { createWriteStream } from "node:fs";
import { mkdir, readFile } from "node:fs/promises";
import path from "node:path";
import { finished } from "node:stream/promises";
import { fileURLToPath } from "node:url";
import { parse } from "yaml";
import yazl from "yazl";

const fixtureTime = new Date("2024-01-02T03:04:05Z");
export async function writeLibrary(root) {
  const catalogURL = new URL("./library.yml", import.meta.url);
  const catalog = parse(await readFile(catalogURL, "utf8"));
  const coverDirectory = path.join(path.dirname(fileURLToPath(catalogURL)), "covers");
  const allBooks = catalog.books.flatMap((book) => Array.from(
    { length: catalog.copies_per_book },
    (_, copy) => ({ ...book, copy, description: catalog.mocked_hardcover[book.cover]?.description }),
  ));
  await Promise.all(allBooks.map((book) => writeEPUB(root, book, readFile(path.join(coverDirectory, catalog.covers[book.cover])))));
}

async function writeEPUB(root, book, cover) {
  const author = book.authors[0];
  const destination = path.join(root, author, `${safeName(book.title)} ${book.copy + 1}.epub`);
  await mkdir(path.dirname(destination), { recursive: true });

  const zip = new yazl.ZipFile();
  zip.addBuffer(Buffer.from("application/epub+zip"), "mimetype", { compress: false });
  zip.addBuffer(Buffer.from(`<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0"><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`), "META-INF/container.xml", { mtime: fixtureTime });
  zip.addBuffer(Buffer.from(opf(book)), "OEBPS/content.opf", { mtime: fixtureTime });
  zip.addBuffer(Buffer.from("<html><body><h1>Fixture chapter</h1><p>Synthetic text for the screenshot harness.</p></body></html>"), "OEBPS/chapter.xhtml", { mtime: fixtureTime });
  zip.addBuffer(await cover, "OEBPS/cover.jpg", { mtime: fixtureTime });

  const output = createWriteStream(destination);
  zip.outputStream.pipe(output);
  zip.end();
  await finished(output);
}

function opf(book) {
  const creators = book.authors.map((author) => `<dc:creator>${xml(author)}</dc:creator>`).join("");
  const series = book.series ? `<meta name="calibre:series" content="${xml(book.series)}"/><meta name="calibre:series_index" content="${book.series_index}"/>` : "";
  const description = book.description ? `<dc:description>${xml(book.description)}</dc:description>` : "";
  const cover = `<item id="cover" href="cover.jpg" media-type="image/jpeg"${book.epub2 ? "" : " properties=\"cover-image\""}/>`;
  if (book.epub2) {
    return `<?xml version="1.0" encoding="UTF-8"?><package xmlns="http://www.idpf.org/2007/opf" version="2.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><meta name="cover" content="cover"/><dc:title>${xml(book.title)}</dc:title>${creators}<dc:date>${book.published}</dc:date>${description}${series}</metadata><manifest>${cover}<item id="chapter" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="chapter"/></spine></package>`;
  }
  return `<?xml version="1.0" encoding="UTF-8"?><package xmlns="http://www.idpf.org/2007/opf" version="3.0"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>${xml(book.title)}</dc:title>${creators}<dc:language>en</dc:language><dc:publisher>Sample Library</dc:publisher><dc:date>${book.published}</dc:date>${description}${series}</metadata><manifest>${cover}<item id="chapter" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="chapter"/></spine></package>`;
}

function safeName(value) {
  return value.replaceAll(/[^a-z0-9]+/gi, " ").trim();
}

function xml(value) {
  return value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const output = process.argv[process.argv.indexOf("--output") + 1];
  if (!output) throw new Error("--output is required");
  await writeLibrary(output);
}
