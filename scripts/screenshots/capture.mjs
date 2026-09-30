import { mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const directory = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(directory, "../..");
const baseURL = process.env.SCREENSHOT_BASE_URL || "http://127.0.0.1:8080";
const output = process.env.SCREENSHOT_OUTPUT || path.join(root, "docs/assets/images/generated");
const username = process.env.LIBRARY_USER || "admin";
const password = process.env.LIBRARY_PASS || "fixture-password";
const buildTag = process.env.SCREENSHOT_BUILD_TAG;

const viewports = [
  { name: "desktop", width: 1440, height: 810 },
  { name: "ereader", width: 768, height: 1024 },
];

await mkdir(output, { recursive: true });

async function login(page) {
  await page.goto(`${baseURL}/login`, { waitUntil: "networkidle" });
  await page.locator('input[name="username"]').fill(username);
  await page.locator('input[name="password"]').fill(password);
  await Promise.all([page.waitForURL(`${baseURL}/`), page.locator('button[type="submit"]').click()]);
  await assertBuildTag(page);
}

async function assertBuildTag(page) {
  if (buildTag) await page.locator(".footer", { hasText: buildTag }).waitFor();
}

async function applyAnnotations(page, annotations = []) {
  const targets = [];
  for (const annotation of annotations) {
    const target = page.locator(annotation.selector).first();
    await target.waitFor();
    const box = await target.boundingBox();
    if (!box) throw new Error(`Annotation target is not visible: ${annotation.selector}`);
    targets.push({ ...annotation, box });
  }

  if (!targets.length) return;
  await page.evaluate((items) => {
    for (const item of items) {
      const surround = 8;
      const left = Math.max(0, item.box.x - surround);
      const top = Math.max(0, item.box.y - surround);
      const right = Math.min(window.innerWidth, item.box.x + item.box.width + surround);
      const bottom = Math.min(window.innerHeight, item.box.y + item.box.height + surround);
      const highlight = document.createElement("div");
      highlight.setAttribute("data-screenshot-annotation", "highlight");
      Object.assign(highlight.style, {
        position: "fixed",
        zIndex: "2147483646",
        pointerEvents: "none",
        boxSizing: "border-box",
        left: `${left}px`,
        top: `${top}px`,
        width: `${Math.max(0, right - left)}px`,
        height: `${Math.max(0, bottom - top)}px`,
        border: "3px solid #ef4444",
        borderRadius: "8px",
        background: "transparent",
      });

      const label = document.createElement("div");
      label.setAttribute("data-screenshot-annotation", "label");
      label.textContent = item.label;
      Object.assign(label.style, {
        position: "fixed",
        zIndex: "2147483647",
        pointerEvents: "none",
        boxSizing: "border-box",
        maxWidth: "min(280px, calc(100vw - 16px))",
        padding: "6px 9px",
        border: "none",
        borderRadius: "4px",
        background: "rgba(0, 0, 0, 0.78)",
        color: "#ef4444",
        font: "600 14px/1.2 system-ui, sans-serif",
      });

      document.body.append(highlight, label);
      const gap = 8;
      const labelTop = item.placement === "bottom" ? bottom + gap : top - label.offsetHeight - gap;
      label.style.left = `${Math.min(Math.max(8, left), window.innerWidth - label.offsetWidth - 8)}px`;
      label.style.top = `${Math.min(Math.max(8, labelTop), window.innerHeight - label.offsetHeight - 8)}px`;
    }
  }, targets);
}

async function openBookModal(page) {
  await login(page);
  await page.goto(`${baseURL}/?q=${encodeURIComponent("Pride and Prejudice")}`, { waitUntil: "networkidle" });
  const card = page.locator(".card", { hasText: "Pride and Prejudice" });
  const modalID = (await card.getAttribute("id")).replace("card-", "book-");
  await page.evaluate((id) => openModal(id), modalID);
  await page.locator(".modal-overlay:visible .modal-box-wide").waitFor();
}

const scenarios = [
  { name: "login", prepare: (page) => page.goto(`${baseURL}/login`, { waitUntil: "networkidle" }) },
  { name: "library-grid", prepare: login },
  {
    name: "library-dark",
    async prepare(page) {
      await page.context().addCookies([{ name: "eink-library-theme", value: "dark", url: baseURL }]);
      await login(page);
    },
  },
  {
    name: "book-modal",
    prepare: openBookModal,
  },
  {
    name: "book-shelves",
    prepare: openBookModal,
    annotations: [{ selector: ".modal-overlay:visible button.shelf-toggle", label: "Add to Favourites", placement: "top" }],
  },
  {
    name: "account-preferences",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/account/password`, { waitUntil: "networkidle" });
    },
    annotations: [
      { selector: 'input[name="new_password"]', label: "Choose a new password", placement: "bottom" },
      { selector: "h2", label: "Optional weekly new-book digest", placement: "bottom" },
    ],
  },
  {
    name: "admin-settings",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/admin/settings`, { waitUntil: "networkidle" });
    },
  },
  {
    name: "admin-configuration",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/admin/settings`, { waitUntil: "networkidle" });
    },
    annotations: [{ selector: 'input[name="public_url"]', label: "Public HTTPS URL for email links", placement: "bottom" }],
  },
  {
    name: "metadata-edit",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/?q=${encodeURIComponent("Pride and Prejudice")}`, { waitUntil: "networkidle" });
      const card = page.locator(".card", { hasText: "Pride and Prejudice" });
      const bookID = (await card.getAttribute("id")).replace("card-", "");
      await page.goto(`${baseURL}/books/${bookID}/edit-metadata`, { waitUntil: "networkidle" });
    },
    annotations: [{ selector: 'input[name="title"]', label: "Correct metadata fields by hand", placement: "bottom" }],
  },
  {
    name: "authors",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/authors`, { waitUntil: "networkidle" });
    },
  },
  {
    name: "series",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/series`, { waitUntil: "networkidle" });
    },
  },
  {
    name: "admin-users",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/admin/users`, { waitUntil: "networkidle" });
    },
  },
];

const browser = await chromium.launch({ headless: true });
try {
  for (const viewport of viewports) {
    for (const scenario of scenarios) {
      const page = await browser.newPage({ viewport: { width: viewport.width, height: viewport.height }, deviceScaleFactor: 1 });
      await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
      await scenario.prepare(page);
      await page.evaluate(() => document.fonts.ready);
      await applyAnnotations(page, scenario.annotations);
      await page.screenshot({ path: path.join(output, `${scenario.name}-${viewport.name}.png`), type: "png" });
      await page.close();
    }
  }
} finally {
  await browser.close();
}
