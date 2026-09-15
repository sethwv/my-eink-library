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
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/?q=${encodeURIComponent("Pride and Prejudice")}`, { waitUntil: "networkidle" });
      const card = page.locator('.card', { hasText: "Pride and Prejudice" });
      const modalID = (await card.getAttribute("id")).replace("card-", "book-");
      await page.evaluate((id) => openModal(id), modalID);
      await page.locator('.modal-overlay:visible .modal-box-wide').waitFor();
    },
  },
  {
    name: "admin-settings",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/admin/settings`, { waitUntil: "networkidle" });
    },
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
      await page.screenshot({ path: path.join(output, `${scenario.name}-${viewport.name}.png`), type: "png" });
      await page.close();
    }
  }
} finally {
  await browser.close();
}
