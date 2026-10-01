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
    const viewportPadding = 8;
    const gap = 8;
    const surrounds = items.map((item) => {
      const surround = 8;
      const left = Math.max(0, item.box.x - surround);
      const top = Math.max(0, item.box.y - surround);
      const right = Math.min(window.innerWidth, item.box.x + item.box.width + surround);
      const bottom = Math.min(window.innerHeight, item.box.y + item.box.height + surround);
      return { left, top, right, bottom };
    });

    const overlaps = (first, second) => first.left < second.right && first.right > second.left && first.top < second.bottom && first.bottom > second.top;
    const clamp = (value, min, max) => Math.min(Math.max(min, value), max);
    const occupied = [...surrounds];

    for (const [index, item] of items.entries()) {
      const { left, top, right, bottom } = surrounds[index];
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

      document.body.append(highlight);

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
        visibility: "hidden",
      });

      document.body.append(label);
      const labelWidth = label.offsetWidth;
      const labelHeight = label.offsetHeight;
      const candidate = (x, y) => ({
        left: clamp(x, viewportPadding, window.innerWidth - labelWidth - viewportPadding),
        top: clamp(y, viewportPadding, window.innerHeight - labelHeight - viewportPadding),
        right: 0,
        bottom: 0,
      });
      const above = candidate(left, top - labelHeight - gap);
      const below = candidate(left, bottom + gap);
      const preferred = item.placement === "bottom" ? [below, above] : [above, below];
      const options = [
        ...preferred,
        candidate(right + gap, top),
        candidate(left - labelWidth - gap, top),
        candidate(right - labelWidth, top - labelHeight - gap),
        candidate(right - labelWidth, bottom + gap),
      ].map((position) => ({ ...position, right: position.left + labelWidth, bottom: position.top + labelHeight }));
      let position = options.find((option) => !occupied.some((area) => overlaps(option, area)));

      if (!position) {
        for (let y = viewportPadding; y <= window.innerHeight - labelHeight - viewportPadding && !position; y += gap) {
          for (let x = viewportPadding; x <= window.innerWidth - labelWidth - viewportPadding; x += gap) {
            const option = { left: x, top: y, right: x + labelWidth, bottom: y + labelHeight };
            if (!occupied.some((area) => overlaps(option, area))) {
              position = option;
              break;
            }
          }
        }
      }

      if (!position) throw new Error(`No clear label position for annotation: ${item.label}`);
      label.style.left = `${position.left}px`;
      label.style.top = `${position.top}px`;
      label.style.visibility = "visible";
      occupied.push(position);
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
    name: "library-controls",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/?q=${encodeURIComponent("Pride and Prejudice")}&sort=title&dir=asc`, { waitUntil: "networkidle" });
    },
    annotations: [
      { selector: "#q", label: "Search titles, authors, or series", placement: "bottom" },
      { selector: "#sort", label: "Choose how results are ordered", placement: "bottom" },
    ],
  },
  {
    name: "book-modal",
    prepare: openBookModal,
  },
  {
    name: "book-download",
    prepare: openBookModal,
    annotations: [{ selector: ".modal-overlay:visible .modal-actions-cell", label: "Download the original EPUB or Kobo KEPUB", placement: "top" }],
  },
  {
    name: "book-shelves",
    prepare: openBookModal,
    annotations: [{ selector: ".modal-overlay:visible button.shelf-toggle", label: "Add to Favourites", placement: "top" }],
  },
  {
    name: "favourites-library",
    async prepare(page) {
      await openBookModal(page);
      await page.locator(".modal-overlay:visible button.shelf-toggle").click();
      await page.locator(".modal-overlay:visible button.shelf-toggle.is-on").waitFor();
      await page.goto(`${baseURL}/favorites`, { waitUntil: "networkidle" });
    },
    annotations: [{ selector: "h1:not(.site-title)", label: "Books saved to Favourites", placement: "bottom" }],
  },
  {
    name: "account-appearance",
    async prepare(page) {
      await login(page);
      await page.evaluate(() => openModal("account-modal"));
      await page.locator("#account-modal:visible").waitFor();
    },
    annotations: [{ selector: "#account-modal:visible #theme-mode", label: "Choose Auto, Light, or Dark", placement: "bottom" }],
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
    name: "account-bookmark",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/account/bookmark`, { waitUntil: "networkidle" });
    },
    annotations: [{ selector: 'form[action="/account/bookmark/regenerate"] button', label: "Create one e-reader bookmark link", placement: "top" }],
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
    name: "admin-server",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/admin/server`, { waitUntil: "networkidle" });
    },
    annotations: [
      { selector: 'form[action="/admin/server/rescan"] button', label: "Check configured folders for changes", placement: "top" },
      { selector: 'form[action="/admin/server/reimport"] button', label: "Rebuild the index when needed", placement: "bottom" },
    ],
  },
  {
    name: "admin-smtp",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/admin/smtp`, { waitUntil: "networkidle" });
    },
    annotations: [
      { selector: 'form[action="/admin/settings/smtp"]', label: "Save your mail provider settings", placement: "top" },
      { selector: 'form[action="/admin/settings/smtp/test"]', label: "Send a test before inviting users", placement: "bottom" },
    ],
  },
  {
    name: "enrichment-hardcover",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/admin/integrations?provider=hardcover`, { waitUntil: "networkidle" });
    },
    annotations: [{ selector: 'input[name="hardcover_token"]', label: "Paste the Hardcover API token", placement: "bottom" }],
  },
  {
    name: "enrichment-chaptarr",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/admin/integrations?provider=chaptarr`, { waitUntil: "networkidle" });
    },
    annotations: [{ selector: 'input[name="hide_no_chaptarr_match"]', label: "Hide books without a provider match", placement: "bottom" }],
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
  {
    name: "admin-add-user",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/admin/users`, { waitUntil: "networkidle" });
      await page.evaluate(() => openModal("add-user-modal"));
      await page.locator("#add-user-modal:visible").waitFor();
    },
    annotations: [{ selector: "#add-user-modal:visible", label: "Create a member and choose their role", placement: "top" }],
  },
  {
    name: "admin-manage-user",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/admin/users`, { waitUntil: "networkidle" });
      const modalID = await page.locator('[id^="manage-user-"]').first().getAttribute("id");
      await page.evaluate((id) => openModal(id), modalID);
      await page.locator('.modal-overlay:visible [name="role"]').waitFor();
    },
    annotations: [
      { selector: '.modal-overlay:visible [name="role"]', label: "Assign the least-privileged role", placement: "bottom" },
      { selector: '.modal-overlay:visible [name="can_bookmark"]', label: "Control e-reader bookmark access", placement: "bottom" },
    ],
  },
  {
    name: "admin-invite-user",
    async prepare(page) {
      await login(page);
      await page.goto(`${baseURL}/admin/users`, { waitUntil: "networkidle" });
      await page.evaluate(() => openModal("invite-user-modal"));
      await page.locator("#invite-user-modal:visible").waitFor();
    },
    annotations: [{ selector: "#invite-user-modal:visible", label: "Send a role-specific email invitation", placement: "top" }],
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
