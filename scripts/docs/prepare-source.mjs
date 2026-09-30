import { cp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";

const [source, destination, contributing, changelog, brandingSource, sitePath, activeSlug, ref, siteBasePath, manifestJSON] = process.argv.slice(2);
if (!source || !destination || !contributing || !changelog || !brandingSource || sitePath === undefined || !activeSlug || !ref || !siteBasePath || !manifestJSON) {
  throw new Error("usage: prepare-source.mjs <docs-source> <destination> <contributing-source> <changelog-source> <main-docs-source> <site-path> <active-slug> <git-ref> <site-base-path> <version-manifest-json>");
}

const versions = JSON.parse(manifestJSON);
if (!Array.isArray(versions) || !versions.every(({ slug: versionSlug, label: versionLabel, path: versionPath }) => versionSlug && versionLabel && typeof versionPath === "string")) {
  throw new Error("version manifest must contain slugs, paths, and labels");
}

const escapeHTML = (value) => value.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;");
const escapeYAML = (value) => JSON.stringify(value);
const normalizedBasePath = siteBasePath.replace(/\/$/, "");
const versionPath = sitePath ? `${normalizedBasePath}/${sitePath}` : normalizedBasePath;

await rm(destination, { force: true, recursive: true });
await cp(source, destination, { recursive: true });

// The current docs shell intentionally applies to every version. Content,
// screenshot data, and generated screenshots remain in the tag source.
await cp(path.join(brandingSource, "_config.yml"), path.join(destination, "_config.yml"));
await cp(path.join(brandingSource, "_includes", "head_custom.html"), path.join(destination, "_includes", "head_custom.html"));
await cp(path.join(brandingSource, "assets", "css"), path.join(destination, "assets", "css"), { force: true, recursive: true });
await cp(path.join(brandingSource, "assets", "js"), path.join(destination, "assets", "js"), { force: true, recursive: true });

const frontMatter = "---\ntitle: Contributing\nnav_order: 8\nsearch_exclude: true\ngh_edit_link: false\n---\n\n";
const contributionGuide = await readFile(contributing, "utf8");
await writeFile(path.join(destination, "contributing.md"), frontMatter + contributionGuide);

const changelogFrontMatter = "---\ntitle: Changelog\nnav_order: 9\nsearch_exclude: true\ngh_edit_link: false\n---\n\n";
const changelogContents = await readFile(changelog, "utf8");
const changelogWithTOC = changelogContents.replace(/^# Changelog\r?\n/, `# Changelog
{: .no_toc }

## Contents
{: .no_toc }

1. TOC
{:toc}

`);
await writeFile(path.join(destination, "changelog.md"), changelogFrontMatter + changelogWithTOC);

const configPath = path.join(destination, "_config.yml");
const config = await readFile(configPath, "utf8");
await writeFile(configPath, `${config}\nbaseurl: ${escapeYAML(versionPath)}\ngh_edit_branch: ${escapeYAML(ref)}\n`);

const renderOption = (version) => {
  const selected = version.slug === activeSlug ? " selected" : "";
  const destinationPath = version.path ? `${normalizedBasePath}/${version.path}/` : `${normalizedBasePath}/`;
  return `<option value="${escapeHTML(destinationPath)}" data-version-root="${escapeHTML(destinationPath)}"${selected}>${escapeHTML(version.label)}</option>`;
};
const currentVersions = versions.filter((version) => version.slug === "latest" || version.slug === "main");
const releasedVersions = versions.filter((version) => version.slug !== "latest" && version.slug !== "main");
const options = [
  ...currentVersions.map(renderOption),
  "<optgroup label=\"Releases\">",
  ...releasedVersions.map(renderOption),
  "</optgroup>",
].join("\n");
const selector = `\n<div class="docs-version-switcher">\n  <label for="docs-version">Version</label>\n  <select id="docs-version" data-docs-version data-page-path="{{ page.url }}">\n${options}\n  </select>\n</div>\n`;

const includes = path.join(destination, "_includes");
await mkdir(includes, { recursive: true });
await writeFile(path.join(includes, "docs_version_switcher.html"), selector);

const sidebar = `{%- comment -%}
  Keep the version selector inside the navigation so it is available in the
  expanded mobile menu, rather than in the page footer.
{%- endcomment -%}
<header class="side-bar">
  <div class="site-header">
    <a href="{{ '/' | relative_url }}" class="site-title lh-tight">{% include title.html %}</a>
    <button id="menu-button" class="site-button btn-reset" aria-label="Menu" aria-expanded="false">
      <svg viewBox="0 0 24 24" class="icon" aria-hidden="true"><use xlink:href="#svg-menu"></use></svg>
    </button>
  </div>

  {% include_cached components/site_nav.html %}
  {% include docs_version_switcher.html %}

  <div class="d-md-block d-none site-footer">
  {% capture nav_footer_custom %}
    {%- include nav_footer_custom.html -%}
  {% endcapture %}
  {% if nav_footer_custom != "" %}
    {{ nav_footer_custom }}
  {% else %}
    <div class="docs-theme-credit">This site uses <a href="https://github.com/just-the-docs/just-the-docs">Just the Docs</a></div>
    <div class="docs-sidebar-copyright">
      <div>my-eink-library is licensed under <a href="https://github.com/sethwv/my-eink-library/blob/main/LICENSE">AGPL-3.0-only</a></div>
      <div>&copy; <script>document.write(new Date().getFullYear());</script> swvn.io</div>
    </div>
  {% endif %}
  </div>
</header>
`;
const components = path.join(includes, "components");
await mkdir(components, { recursive: true });
await writeFile(path.join(components, "sidebar.html"), sidebar);

const assets = path.join(destination, "assets", "docs-version");
await mkdir(assets, { recursive: true });
await writeFile(path.join(assets, "switcher.css"), `
.docs-version-switcher { margin: 0.75rem 0; padding: 0 1rem; }
.docs-version-switcher label { color: var(--muted, #5c5962); display: block; font-family: "DM Mono", monospace; font-size: 0.62rem; letter-spacing: 0.04em; margin-bottom: 0.3rem; text-transform: uppercase; }
.docs-version-switcher select { background: var(--paper-deep, #fff); border: 1px solid var(--line, #d5d5d5); border-radius: 0; box-sizing: border-box; color: var(--ink, inherit); font-family: "DM Mono", monospace; font-size: 0.72rem; line-height: 1.2; max-width: 100%; padding: 0.4rem 0.5rem; width: 100%; }
@media (min-width: 50rem) { .docs-version-switcher { padding: 0 2rem; } }
`);
await writeFile(path.join(assets, "switcher.js"), `document.addEventListener("change", (event) => {
  const selector = event.target;
  if (!selector.matches("[data-docs-version]")) return;

  const option = selector.options[selector.selectedIndex];
  const root = option.dataset.versionRoot;
  const pagePath = selector.dataset.pagePath || "/";
  const destination = new URL(root.replace(/\\/$/, "") + pagePath, window.location.origin);

  fetch(destination, { method: "HEAD" })
    .then((response) => window.location.assign(response.ok ? destination : option.value))
    .catch(() => window.location.assign(option.value));
});
`);

const headPath = path.join(includes, "head_custom.html");
let head = "";
try {
  head = await readFile(headPath, "utf8");
} catch (error) {
  if (error.code !== "ENOENT") throw error;
}
await writeFile(headPath, `${head}\n<link rel="stylesheet" href="{{ '/assets/docs-version/switcher.css' | relative_url }}">\n<script src="{{ '/assets/docs-version/switcher.js' | relative_url }}" defer></script>\n`);
