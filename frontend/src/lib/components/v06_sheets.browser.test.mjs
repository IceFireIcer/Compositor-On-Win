import { afterAll, beforeAll, beforeEach, expect, it, vi } from "vitest";
import { createServer } from "vite";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";

// 浏览器更新在并发 CI 负载下可能超过 Vitest 的 1s 默认超时。
const poll = (callback) => expect.poll(callback, { timeout: 10000 });
vi.setConfig({ testTimeout: 30000 });
const require = createRequire(import.meta.url);
// 单独安装：npm install --prefix <PLAYWRIGHT_ROOT> playwright。
const { chromium } = require(path.join(process.env.PLAYWRIGHT_ROOT ?? path.join(tmpdir(), "compositor-playwright-smoke"), "node_modules", "playwright"));
let server, browser, page;

beforeAll(async () => {
  // 并发浏览器夹具不能互相覆盖 Svelte 预构建缓存。
  server = await createServer({ cacheDir: "node_modules/.vite-v06-sheets-browser", server: { port: 0, strictPort: false }, optimizeDeps: { noDiscovery: true, include: [] }, plugins: [{ name: "v06-sheets-browser-fixture", configureServer(vite) {
    vite.middlewares.use("/v06-sheets-test", (_req, res) => {
      res.setHeader("Content-Type", "text/html");
      res.end('<!doctype html><html><body><div id="root"></div></body></html>');
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
  await page.goto(`${server.resolvedUrls.local[0]}v06-sheets-test`);
});

// 票 53/V06：两个表单的数值输入改用 NumericField。这里钉的是「表单真的接上了」，
// 三种交互语义（拖标签、↑↓ 步进、输入钳制）本身由 NumericField/numeric 的单测覆盖。
// 字符串形式求值：函数体里的动态 import 会被 Vitest 的 SSR 转换破坏。

it("scrubs and clamps GeometrySheet's number fields", async () => {
  await page.evaluate(`(async () => {
    const { mount, tick } = await import("/node_modules/.vite-v06-sheets-browser/deps/svelte.js");
    const { default: GeometrySheet } = await import("/src/lib/components/GeometrySheet.svelte");
    const { geometrySheet } = await import("/src/lib/state/geometry.ts");
    geometrySheet.set("canvasSize");
    mount(GeometrySheet, { target: document.getElementById("root") });
    await tick();
  })()`);

  const field = page.locator(".numeric-field").filter({ hasText: "宽度（px）" });
  const input = field.locator("input");
  // 没有打开过的文档时 activeSize() 给 100 × 100。
  await poll(() => input.inputValue()).toBe("100");
  // min/max/step 是原 input 的属性原样搬过来的。
  expect(await input.getAttribute("min")).toBe("1");
  expect(await input.getAttribute("max")).toBe("30000");
  expect(await input.getAttribute("step")).toBe("1");

  // 拖标签右移 7px → 100 + 7 = 107（灵敏度 1、步进 1）。显示值来自父组件 state
  // 回写，所以这条同时证明 onchange 真的写进了组件状态。
  const box = await field.locator("label").boundingBox();
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 7, box.y + box.height / 2, { steps: 4 });
  await page.mouse.up();
  await poll(() => input.inputValue()).toBe("107");

  // 输入超上界：钳到 30000，不回绕。
  await input.fill("99999");
  await input.blur();
  await poll(() => input.inputValue()).toBe("30000");
});

it("clamps SelectionEditSheet's radius field before storing it", async () => {
  await page.evaluate(`(async () => {
    const { mount, tick } = await import("/node_modules/.vite-v06-sheets-browser/deps/svelte.js");
    const { default: SelectionEditSheet } = await import("/src/lib/components/SelectionEditSheet.svelte");
    const { selectionEdit } = await import("/src/lib/state/selectionEdit.ts");
    selectionEdit.set({
      kind: "feather", tabId: "tab-1", docId: "doc-1", rev: 1, width: 64, height: 64,
      original: null, preview: null, include: [], exclude: [], fuzziness: 40, invert: false, generation: 0,
    });
    mount(SelectionEditSheet, { target: document.getElementById("root") });
    await tick();
  })()`);

  await poll(() => page.locator("dialog[open]").count()).toBe(1);
  const field = page.locator(".numeric-field").filter({ hasText: "半径（px）" });
  const input = field.locator("input");
  await poll(() => input.inputValue()).toBe("5");
  // 羽化半径的原生上限仍是 250（扩展/收缩是 500），下限 1。
  expect(await input.getAttribute("min")).toBe("1");
  expect(await input.getAttribute("max")).toBe("250");

  // 超上界：钳到 250。
  await input.fill("999");
  await input.blur();
  await poll(() => input.inputValue()).toBe("250");
  // 父组件 state 也被钳制过：↓ 一步是 249，而不是旧值 5 减 1 的 4。
  await input.focus();
  await page.keyboard.press("ArrowDown");
  await poll(() => input.inputValue()).toBe("249");

  // 超下界：钳到 1。
  await input.fill("0");
  await input.blur();
  await poll(() => input.inputValue()).toBe("1");
});

it("keeps the image-size aspect lock wired through NumericField", async () => {
  await page.evaluate(`(async () => {
    const { mount, tick } = await import("/node_modules/.vite-v06-sheets-browser/deps/svelte.js");
    const { default: GeometrySheet } = await import("/src/lib/components/GeometrySheet.svelte");
    const { geometrySheet } = await import("/src/lib/state/geometry.ts");
    geometrySheet.set("imageSize");
    mount(GeometrySheet, { target: document.getElementById("root") });
    await tick();
  })()`);

  const width = page.locator(".numeric-field").filter({ hasText: "宽度（px）" }).locator("input");
  const height = page.locator(".numeric-field").filter({ hasText: "高度（px）" }).locator("input");
  await poll(() => width.inputValue()).toBe("100");
  await poll(() => height.inputValue()).toBe("100");

  // 原来靠 oninput 实时联动长宽比；改用 NumericField 后走提交（change）事件，
  // 但联动本身必须还在：改宽度 200 → 高度跟着变 200（1:1）。
  await width.fill("200");
  await width.blur();
  await poll(() => height.inputValue()).toBe("200");
});
