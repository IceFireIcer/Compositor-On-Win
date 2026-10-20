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
  server = await createServer({ cacheDir: "node_modules/.vite-v06-filterdialog-browser", server: { port: 0, strictPort: false }, optimizeDeps: { noDiscovery: true, include: [] }, plugins: [{ name: "v06-filterdialog-browser-fixture", configureServer(vite) {
    vite.middlewares.use("/v06-filterdialog-test", (_req, res) => {
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
  await page.goto(`${server.resolvedUrls.local[0]}v06-filterdialog-test`);
});

// 票 53/V06：滤镜对话框的数值输入改用 NumericField。挂载时给 window.go 一个空壳，
// 让 updateFilterSettings 排出的预览 RPC 正常落地（本测试只关心表单状态）。
async function mountDialog(kind) {
  await page.evaluate(`(async () => {
    const { mount, tick } = await import("/node_modules/.vite-v06-filterdialog-browser/deps/svelte.js");
    window.go = { bridge: { Service: new Proxy({}, { get: () => () => Promise.resolve("{}") }) } };
    const { filterSession, defaultFilterSettings } = await import("/src/lib/state/filters.ts");
    const { default: FilterDialog } = await import("/src/lib/components/FilterDialog.svelte");
    filterSession.set({
      tabId: "tab-1", kind: "${kind}", label: "测试滤镜", layerId: "layer-1", seed: 7,
      settings: defaultFilterSettings("${kind}"), preparing: false, destructive: true,
      editLayerId: null, error: null, operation: null,
    });
    mount(FilterDialog, { target: document.getElementById("root") });
    await tick();
  })()`);
  await poll(() => page.locator(".dialog").count()).toBe(1);
}

// 从 store 里读回设置 —— 证明 onchange 真的写进了会话，而不只是输入框显示变了。
async function settingsField(field) {
  return page.evaluate(`(async () => {
    const { filterSession } = await import("/src/lib/state/filters.ts");
    let session = null;
    filterSession.subscribe((value) => { session = value; })();
    return session?.settings.${field};
  })()`);
}

async function dragLabelRight(field, pixels) {
  const box = await field.locator("label").boundingBox();
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + pixels, box.y + box.height / 2, { steps: 4 });
  await page.mouse.up();
}

it("scrubs a vignette number field by dragging its label", async () => {
  await mountDialog("vignette");
  // 晕影的分支里 5 个数值输入全部变成 NumericField。
  await poll(() => page.locator(".numeric-field").count()).toBe(5);

  const field = page.locator(".numeric-field").filter({ hasText: "数量 %" });
  const input = field.locator("input");
  await poll(() => input.inputValue()).toBe("35");
  // min/max/step 是原 input 的属性原样搬过来的。
  expect(await input.getAttribute("min")).toBe("0");
  expect(await input.getAttribute("max")).toBe("100");
  expect(await input.getAttribute("step")).toBe("1");

  // 拖标签右移 7px → 35 + 7 = 42（灵敏度 1、步进 1）。
  await dragLabelRight(field, 7);
  await poll(() => input.inputValue()).toBe("42");
  // 显示值来自 store 回写，所以这条同时证明 onchange 写进了会话设置。
  await poll(() => settingsField("vignetteAmount")).toBe(42);
});

it("clamps a vignette number field to the original input's range", async () => {
  await mountDialog("vignette");
  const input = page.locator(".numeric-field").filter({ hasText: "数量 %" }).locator("input");
  await poll(() => input.inputValue()).toBe("35");

  // 超上界：钳到 100，不回绕。
  await input.fill("999");
  await input.blur();
  await poll(() => input.inputValue()).toBe("100");
  await poll(() => settingsField("vignetteAmount")).toBe(100);

  // 超下界：钳到 0。
  await input.fill("-50");
  await input.blur();
  await poll(() => input.inputValue()).toBe("0");
  await poll(() => settingsField("vignetteAmount")).toBe(0);

  // 解析不出来（清空）保留旧值，而不是写进 NaN。
  await input.fill("");
  await input.blur();
  await poll(() => input.inputValue()).toBe("0");
  await poll(() => settingsField("vignetteAmount")).toBe(0);
});

it("leaves the unlabeled RGB triplets and the sliders native", async () => {
  await mountDialog("vignette");
  await poll(() => page.locator(".numeric-field").count()).toBe(5);

  // RGB 三元组没有标签（NumericField 必须有 label），保持原生，且百分比缩放不变。
  const tiny = await page.evaluate(`[...document.querySelectorAll("input.tiny")].map((el) => ({
    type: el.type, insideField: el.closest(".numeric-field") !== null,
  }))`);
  expect(tiny).toEqual([
    { type: "number", insideField: false },
    { type: "number", insideField: false },
    { type: "number", insideField: false },
  ]);
  // 滑块全部保留，并补上 aria-label（原来是靠外层 label 给可访问名的）。
  const ranges = await page.evaluate(`[...document.querySelectorAll(".dialog input[type=range]")].map((el) => el.getAttribute("aria-label"))`);
  expect(ranges.length).toBe(5);
  expect(ranges.every((label) => typeof label === "string" && label.length > 0)).toBe(true);

  // 标签文本没有被复制两遍（原来的 <span> 文案搬进了 NumericField）。
  const counts = await page.evaluate(`(() => {
    const text = document.querySelector(".dialog").textContent;
    return { amount: (text.match(/数量 %/g) ?? []).length, highlights: (text.match(/高光/g) ?? []).length };
  })()`);
  expect(counts.amount).toBe(1);
  expect(counts.highlights).toBe(1);
});

it("covers the parameter-heavy scanlines form", async () => {
  await mountDialog("scanlines");
  // 扫描线分支有 12 个数值输入。
  await poll(() => page.locator(".numeric-field").count()).toBe(12);

  const field = page.locator(".numeric-field").filter({ hasText: "行间距 px" });
  const input = field.locator("input");
  await poll(() => input.inputValue()).toBe("4");
  await dragLabelRight(field, 7);
  await poll(() => input.inputValue()).toBe("11");
  await poll(() => settingsField("scanlines.lineSpacing")).toBe(11);
});
it("converts the Levels pip row and its four neighbours", async () => {
  await mountDialog("adjust:Levels");
  // 黑场/灰场/白场 + 输出黑/输出白。
  await poll(() => page.locator(".numeric-field").count()).toBe(5);
  // 吸管按钮仍留在黑场旁边（原来它在 <span>黑场</span> 里面）。
  await poll(() => page.locator(".pip").count()).toBe(1);

  const field = page.locator(".numeric-field").filter({ hasText: "黑场" });
  const input = field.locator("input");
  await poll(() => input.inputValue()).toBe("0");
  expect(await input.getAttribute("max")).toBe("254");
  await dragLabelRight(field, 7);
  await poll(() => input.inputValue()).toBe("7");
  await poll(() => settingsField("adjustment.levels.ranges[0].black")).toBe(7);
});
