import { afterAll, beforeAll, beforeEach, expect, it, vi } from "vitest";
import { createServer } from "vite";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";

// Browser updates can exceed Vitest's 1s default under concurrent CI load.
const poll = (callback) => expect.poll(callback, { timeout: 10000 });
vi.setConfig({ testTimeout: 30000 });
const require = createRequire(import.meta.url);
// Install separately with npm install --prefix <PLAYWRIGHT_ROOT> playwright.
const { chromium } = require(path.join(process.env.PLAYWRIGHT_ROOT ?? path.join(tmpdir(), "compositor-playwright-smoke"), "node_modules", "playwright"));
let server, browser, page;

beforeAll(async () => {
  // Concurrent browser fixtures must not overwrite each other's Svelte cache.
  server = await createServer({ cacheDir: "node_modules/.vite-toolbar-browser", server: { port: 0, strictPort: false }, optimizeDeps: { noDiscovery: true, include: [] }, plugins: [{ name: "tool-options-browser-fixture", configureServer(vite) {
    vite.middlewares.use("/tool-options-test", (_req, res) => {
      res.setHeader("Content-Type", "text/html");
      res.end('<!doctype html><html><body><div id="bar" style="width:320px"></div></body></html>');
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
  await page.goto(`${server.resolvedUrls.local[0]}tool-options-test`);
  // A string keeps Vitest's SSR transform out of the browser imports.
  await page.evaluate(`(async () => {
    const { mount, tick } = await import("/node_modules/.vite-toolbar-browser/deps/svelte.js");
    const { default: Bar } = await import("/src/lib/components/ToolOptionsBar.svelte");
    const { activeTool } = await import("/src/lib/state/tools.ts");
    window.setTool = (tool) => activeTool.set(tool);
    mount(Bar, { target: document.getElementById("bar") });
    await tick();
  })()`);
  await poll(() => page.locator(".options").count()).toBe(1);
});
// 量一次工具栏在给定宽度下的几何。
async function measure(width) {
  await page.evaluate((w) => { document.getElementById("bar").style.width = `${w}px`; }, width);
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  return page.evaluate(() => {
    const bar = document.querySelector(".options");
    return {
      scrollWidth: bar.scrollWidth,
      clientWidth: bar.clientWidth,
      scrollHeight: bar.scrollHeight,
      clientHeight: bar.clientHeight,
    };
  });
}

it("scrolls horizontally at a narrow width instead of squashing its controls", async () => {
  // 票 92：工具选项栏可横向滚动且没有多余滚动条。
  // 用画笔：它是选项最多的一支，默认工具那点控件在 240px 下本来就放得下。
  await page.evaluate(() => window.setTool("brush"));
  await poll(() => page.locator(".options").textContent()).toMatch(/Diameter/);
  const narrow = await measure(240);
  // 横向：内容比客户区宽，所以才需要横滚 —— 控件是被挤到外面而不是被压扁。
  expect(narrow.scrollWidth).toBeGreaterThan(narrow.clientWidth);
  // 纵向：横滚条不该把内容挤出第二条（竖向）滚动条。
  expect(narrow.scrollHeight).toBeLessThanOrEqual(narrow.clientHeight + 1);

  // 每个控件都还有实际宽度（没有被压成 0）。
  const collapsed = await page.evaluate(() =>
    [...document.querySelectorAll(".options label, .options input, .options select")]
      .filter((el) => el.getBoundingClientRect().width < 1)
      .map((el) => el.className || el.tagName));
  expect(collapsed).toEqual([]);
});

it("does not scroll at all when there is room", async () => {
  const wide = await measure(1600);
  // 宽窗口下不该出现任何滚动条：scrollWidth 与 scrollHeight 都不超过客户区。
  expect(wide.scrollWidth).toBeLessThanOrEqual(wide.clientWidth + 1);
  expect(wide.scrollHeight).toBeLessThanOrEqual(wide.clientHeight + 1);
});
