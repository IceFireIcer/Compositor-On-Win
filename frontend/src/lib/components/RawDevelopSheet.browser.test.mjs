import { afterAll, beforeAll, beforeEach, expect, it, vi } from "vitest";
import { createServer } from "vite";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";

// Browser updates can exceed Vitest's 1s default under concurrent CI load.
// This sheet mounts a component through Vite on first paint, which measured
// over 12s with 71 other test files running, so the poll waits well past that.
const poll = (callback) => expect.poll(callback, { timeout: 30000 });
// The beforeEach below boots a browser page through Vite on every test, so it
// needs the same budget as the tests themselves (Vitest's hook default is 10s).
vi.setConfig({ testTimeout: 30000, hookTimeout: 30000 });
const require = createRequire(import.meta.url);
// Install separately with npm install --prefix <PLAYWRIGHT_ROOT> playwright.
const { chromium } = require(path.join(process.env.PLAYWRIGHT_ROOT ?? path.join(tmpdir(), "compositor-playwright-smoke"), "node_modules/playwright"));
let server, browser, page;

beforeAll(async () => {
  // Concurrent browser fixtures must not overwrite each other's Svelte cache.
  server = await createServer({ cacheDir: "node_modules/.vite-raw-develop-browser", server: { port: 0, strictPort: false }, optimizeDeps: { noDiscovery: true, include: [] }, plugins: [{ name: "raw-develop-browser-fixture", configureServer(vite) {
  vite.middlewares.use("/raw-develop-test", (_req, res) => {
    res.setHeader("Content-Type", "text/html");
    res.end('<!doctype html><html><body><div id="sheet"></div></body></html>');
  });
  } }] });
  await server.listen();
  browser = await chromium.launch({ executablePath: process.env.EDGE_PATH ?? "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", headless: true });
}, 30000);

afterAll(async () => {
  await browser?.close();
  await server?.close();
}, 30000);

beforeEach(async () => {
  await page?.close();
  page = await browser.newPage();
  await page.goto(`${server.resolvedUrls.local[0]}raw-develop-test`);
  // A string keeps Vitest's SSR transform out of the browser imports.
  await page.evaluate(`(async () => {
    const { mount } = await import("/node_modules/.vite-raw-develop-browser/deps/svelte.js");
    const { default: Sheet } = await import("/src/lib/components/RawDevelopSheet.svelte");
    const { rawDevelop } = await import("/src/lib/state/rawdevelop.ts");
    window.setSheet = (asShot) => rawDevelop.set({
      path: "C:/raw/a.cr2", name: "a", fullWidth: 6000, fullHeight: 4000,
      asShot, jpegURL: null, busy: false, error: null,
    });
    mount(Sheet, { target: document.getElementById("sheet") });
  })()`);
});

/** 相机支持降噪、没有镜头配置文件（LibRaw 的实际情况）。 */
const asShot = {
  exposure: 0, temperature: 5400, tint: -6, boost: 1,
  noiseReduction: 0.1, lensCorrection: false, highlightRecovery: false,
  asShotTemperature: 5400, asShotTint: -6, asShotNoiseReduction: 0.1,
  asShotLensCorrection: false, asShotHighlightRecovery: false,
  supportsNoiseReduction: true, supportsLensCorrection: false, supportsHighlightRecovery: true,
};

it("能力位为 false 的控件置灰，并给出原因", async () => {
  await page.evaluate(`window.setSheet(${JSON.stringify(asShot)})`);
  // 5 个滑杆 + 2 个开关。
  await poll(() => page.locator("#sheet .controls input").count()).toBe(7);

  // 降噪与高光恢复可用。
  const noise = page.locator("#sheet .controls label", { hasText: "降噪" });
  expect(await noise.locator("input").isDisabled()).toBe(false);
  expect(await page.locator("#sheet .toggles label", { hasText: "高光恢复" }).locator("input").isDisabled()).toBe(false);
  // 镜头校正没有配置文件：置灰 + 原因。
  const lens = page.locator("#sheet .toggles label", { hasText: "镜头校正" });
  expect(await lens.locator("input").isDisabled()).toBe(true);
  expect(await lens.getAttribute("title")).toBe("此文件没有镜头配置文件");
  // 能力位为 true 的降噪给出的是功能说明，而不是原因。
  expect(await noise.getAttribute("title")).toBe("平滑颗粒，代价是细微细节");
  // 原版的标签宽 110。
  const width = await page.evaluate(() =>
    getComputedStyle(document.querySelector("#sheet .controls .name")).width);
  expect(width).toBe("110px");
});

it("相机不支持降噪时该滑杆也置灰，Reset 在 asShot 时禁用", async () => {
  await page.evaluate(`window.setSheet(${JSON.stringify({
    ...asShot, supportsNoiseReduction: false, supportsHighlightRecovery: false,
  })})`);
  await poll(() => page.locator("#sheet .controls input").count()).toBe(7);

  const noise = page.locator("#sheet .controls label", { hasText: "降噪" });
  expect(await noise.locator("input").isDisabled()).toBe(true);
  expect(await noise.getAttribute("title")).toBe("此相机不支持该功能");
  expect(await page.locator("#sheet .toggles label", { hasText: "高光恢复" }).locator("input").isDisabled()).toBe(true);
  // 三个新参数都在 asShot 起点上 → Reset 禁用。
  expect(await page.locator("#sheet .actions button", { hasText: "重置" }).isDisabled()).toBe(true);
});
