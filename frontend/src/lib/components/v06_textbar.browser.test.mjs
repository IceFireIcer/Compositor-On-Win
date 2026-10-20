import { afterAll, beforeAll, expect, it } from "vitest";
import { createServer } from "vite";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";

// 票 53/V06：文字选项栏的字号/字距/行高也改成 NumericField（拖标签 scrub、输入钳制）。
// 这条只钉住「接线是真的」：拖字号标签会改值、超范围输入会被钳。
const poll = (callback) => expect.poll(callback, { timeout: 10000 });
const require = createRequire(import.meta.url);
const { chromium } = require(path.join(process.env.PLAYWRIGHT_ROOT ?? path.join(tmpdir(), "compositor-playwright-smoke"), "node_modules", "playwright"));
let server, browser;

beforeAll(async () => {
  server = await createServer({ cacheDir: "node_modules/.vite-textbar-browser", server: { port: 0, strictPort: false }, optimizeDeps: { noDiscovery: true, include: [] }, plugins: [{ name: "textbar-browser-fixture", configureServer(vite) {
    vite.middlewares.use("/textbar-test", (_req, res) => {
      res.setHeader("Content-Type", "text/html");
      res.end('<!doctype html><html><body style="margin:0"><div id="bar" style="width:700px"></div></body></html>');
    });
  } }] });
  await server.listen();
  browser = await chromium.launch({ executablePath: process.env.EDGE_PATH ?? "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", headless: true });
}, 40000);

afterAll(async () => {
  await browser?.close();
  await server?.close();
}, 40000);

it("scrubs the font size and clamps out-of-range input", async () => {
  const page = await browser.newPage();
  try {
    await page.goto(`${server.resolvedUrls.local[0]}textbar-test`);
    await page.evaluate(`(async () => {
      const { mount, tick } = await import("/node_modules/.vite-textbar-browser/deps/svelte.js");
      const { default: Bar } = await import("/src/lib/components/TextOptionsBar.svelte");
      mount(Bar, { target: document.getElementById("bar") });
      await tick();
    })()`);

    // 字号字段：label 文本是「字号」，它的输入框在同一 .numeric-field 里。
    const sizeField = page.locator(".numeric-field", { hasText: "字号" }).first();
    const input = sizeField.locator('input[type="number"]');
    await poll(() => input.count()).toBe(1);
    const before = Number(await input.inputValue());

    // 拖标签 +5px（灵敏度 1、步进 1）→ 值 +5。
    const label = sizeField.locator("label").first();
    const box = await label.boundingBox();
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width / 2 + 5, box.y + box.height / 2, { steps: 3 });
    await page.mouse.up();
    await poll(() => input.inputValue()).toBe(String(before + 5));

    // 超范围输入 → 钳到 max=2000。
    await input.fill("99999");
    await input.blur();
    await poll(() => input.inputValue()).toBe("2000");
  } finally {
    await page.close();
  }
}, 30000);
