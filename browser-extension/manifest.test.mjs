import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const manifest = JSON.parse(await readFile(new URL("./manifest.json", import.meta.url), "utf8"));

const forbiddenPermissions = [
  "<all_urls>",
  "tabs",
  "activeTab",
  "scripting",
  "webRequest",
  "cookies",
  "history",
  "clipboardRead",
  "clipboardWrite",
  "notifications",
  "unlimitedStorage"
];

test("manifest freezes the first Manifest V3 target", () => {
  assert.equal(manifest.manifest_version, 3);
  assert.equal(manifest.minimum_chrome_version, "102");
  assert.deepEqual(manifest.permissions, ["contextMenus", "storage"]);
  assert.deepEqual(manifest.host_permissions, ["http://127.0.0.1:8080/*"]);
  assert.deepEqual(manifest.background, {
    service_worker: "service-worker.mjs",
    type: "module"
  });
  assert.equal(manifest.action.default_popup, "popup.html");
});

test("manifest has no content scripts or broad permissions", () => {
  assert.equal(Object.hasOwn(manifest, "content_scripts"), false);
  const declared = new Set([
    ...(manifest.permissions ?? []),
    ...(manifest.host_permissions ?? [])
  ]);
  for (const permission of forbiddenPermissions) {
    assert.equal(declared.has(permission), false, `forbidden permission declared: ${permission}`);
  }
});

test("manifest package contains no remotely hosted executable declarations", () => {
  assert.equal(Object.hasOwn(manifest, "externally_connectable"), false);
  assert.equal(Object.hasOwn(manifest, "content_security_policy"), false);
  assert.equal(Object.hasOwn(manifest, "web_accessible_resources"), false);
});
