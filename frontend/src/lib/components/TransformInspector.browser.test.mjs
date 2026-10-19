import { afterAll, beforeAll, expect, it } from "vitest";
import { createServer } from "vite";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";

// 票 54/V06：数值字段的三种调法 —— 拖标签 scrub、方向键步进、输入钳制。
//
// numeric.ts 里那三个纯函数本来就有单测，但「表单有没有真的接上」只有挂组件才测得到；
// 对等矩阵 V06 记的正是「纯函数 + 测试齐备，零生产消费者」。
const poll = (callback) => expect.poll(callback, { timeout: 10000 });
const require = createRequire(import.meta.url);
const { chromium } = require(path.join(process.env.PLAYWRIGHT_ROOT ?? path.join(tmpdir(), "compositor-playwright-smoke"), "node_modules", "playwright"));
let server, browser;

beforeAll(async () => {
  server = await createServer({ cacheDir: "node_modules/.vite-inspector-browser", server: { port: 0, strictPort: false }, optimizeDeps: { noDiscovery: true, include: [] }, plugins: [{ name: "inspector-browser-fixture", configureServer(vite) {
    vite.middlewares.use("/inspector-test", (_req, res) => {
      res.setHeader("Content-Type", "text/html");
      res.end('<!doctype html><html><body style="margin:0"><div id="inspector" style="width:320px"></div></body></html>');
    });
  } }] });
  await server.listen();
  browser = await chromium.launch({ executablePath: process.env.EDGE_PATH ?? "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", headless: true });
}, 40000);

afterAll(async () => {
  await browser?.close();
  await server?.close();
}, 40000);

it("scrubs, steps and clamps the transform fields", async () => {
  const page = await browser.newPage({ viewport: { width: 900, height: 700 } });
  try {
    await page.goto(`${server.resolvedUrls.local[0]}inspector-test`);
    await page.evaluate(`(async () => {
      const { mount, tick } = await import("/node_modules/.vite-inspector-browser/deps/svelte.js");
      const { default: Inspector } = await import("/src/lib/components/TransformInspector.svelte");
      const { applyDocumentSnapshot } = await import("/src/lib/state/document.ts");
      const { workspace } = await import("/src/lib/state/workspace.ts");
      const { beginTransformSession, transformDraft } = await import("/src/lib/state/transform.ts");
      transformDraft.subscribe((value) => { window.__draft = value; });
      const doc = { documentID: "tab-a", width: 200, height: 200, resolution: 72, activeLayerID: "layer", layers: [{
        id: "layer", name: "Layer", isVisible: true, opacity: 1, blendMode: "Normal", imageFile: "layer.png",
        transform: { origin: [10, 20], size: [100, 50], sampling: "Nearest" } }] };
      const tab = { id: "tab-a", name: "Transform", width: 200, height: 200, resolution: 72, dirty: false };
      workspace.set({ activeId: "tab-a", tabs: [tab] });
      applyDocumentSnapshot(JSON.stringify({ rev: 1, filterRev: 0, doc }));
      if (!beginTransformSession()) throw new Error("会话未开始");
      mount(Inspector, { target: document.getElementById("inspector") });
      await tick();
    })()`);
    const x = page.locator("#transform-x");
    await poll(() => x.inputValue()).toBe("10");

    // 拖标签：右移 7px → 10 + 7 = 17（灵敏度 1、步进 1）。
    const label = page.locator('label[for="transform-x"]');
    const box = await label.boundingBox();
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width / 2 + 7, box.y + box.height / 2, { steps: 4 });
    await page.mouse.up();
    await poll(() => x.inputValue()).toBe("17");

    // 方向键：↑ 加一步。
    await x.focus();
    await page.keyboard.press("ArrowUp");
    await poll(() => x.inputValue()).toBe("18");
    // Shift+↑ 走十步（ArrowStepper 把步长乘十，不是调细）。
    await page.keyboard.press("Shift+ArrowUp");
    await poll(() => x.inputValue()).toBe("28");
    // ↓ 减一步。
    await page.keyboard.press("ArrowDown");
    await poll(() => x.inputValue()).toBe("27");

    // 采样质量三档（票 33/L06）：下拉必须真的改草稿。三档缺 UI 曾是矩阵里记的一条。
    const sampling = page.locator("#transform-sampling");
    // 夹具里那一层的 transform 本来就写着 Nearest，所以初始值就是它（不是默认的 High quality）。
    await poll(() => sampling.inputValue()).toBe("Nearest");
    await sampling.selectOption("Smooth");
    await poll(() => page.evaluate(() => window.__draft && window.__draft.sampling)).toBe("Smooth");
    await sampling.selectOption("High quality");
    await poll(() => page.evaluate(() => window.__draft && window.__draft.sampling)).toBe("High quality");

    // 输入超范围：钳到 30000，不回绕。
    //
    // 注意：<input type="number" max> 自己也会钳到 30000，所以这条是**行为**断言，不是判别性的
    // 那条 —— direct() 的钳制由 numeric.test.ts 的单测管；这里钉的是「接线之后超范围不会变成
    // 999999」这个用户可见的结果。
    await x.fill("999999");
    await x.blur();
    await poll(() => x.inputValue()).toBe("30000");
  } finally {
    await page.close();
  }
}, 30000);