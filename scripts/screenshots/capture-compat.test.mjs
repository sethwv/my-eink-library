import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { loadCaptureSpec, supportsCaptureScenario } from "./capture-compat.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");

test("loads historical release shims through their compatibility boundary", async () => {
	const spec = await loadCaptureSpec("/does-not-exist", "v0.0.2");
	assert.equal(spec.scenarios[0].id, "login");
	assert.equal(spec.scenarios.at(-1).id, "admin-users");
	assert.equal(await supportsCaptureScenario("v0.0.2", "first-admin-setup"), false);
});

test("loads the current catalog after the compatibility boundary", async () => {
	const spec = await loadCaptureSpec(root, "v0.0.3");
	assert.ok(Array.isArray(spec.scenarios));
	assert.ok(spec.scenarios.length > 0);
});
