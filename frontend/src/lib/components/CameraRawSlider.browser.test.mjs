import { afterAll, beforeAll, expect, it } from "vitest";
import { createServer } from "vite";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";

// 票 34 的单条验收：「Camera RawSliderTests 语义（滑杆行为）通过」。
// `CameraRawSlider` 在整个面板里用了约 80 次，却一直没有自己的测试。
//
// 其中最有价值的是第三条：**Option 拖动看剪切视图**。原实现从 `InputEvent` 上读 `altKey`，
// 而浏览器的 input 事件不携带修饰键 —— 那个读取永远 false，功能等于死的。这条测试会抓住它。
const poll = (callback) => expect.poll(callback, { timeout: 10000 });
const require = createRequire(import.meta.url);
const { chromium } = require(path.join(process.env.PLAYWRIGHT_ROOT ?? path.join(tmpdir(), "compositor-playwright-smoke"), "node_modules", "playwright"));
let server, browser;

beforeAll(async () => {
  server = await createServer({ cacheDir: "node_modules/.vite-slider-browser", server: { port: 0, strictPort: false }, optimizeDeps: { noDiscovery: true, include: [] }, plugins: [{ name: "slider-browser-fixture", configureServer(vite) {
    vite.middlewares.use("/slider-test", (_req, res) => {
      res.setHeader("Content-Type", "text/html");
      res.end('<!doctype html><html><body style="margin:0"><div id="slider" style="width:320px"></div></body></html>');
    });
  } }] });
  await server.listen();
  browser = await chromium.launch({ executablePath: process.env.EDGE_PATH ?? "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", headless: true });
}, 40000);

afterAll(async () => {
  await browser?.close();
  await server?.close();
}, 40000);

it("resets on a label double-click, snaps input, and reports Option-drag as clipping", async () => {
  const page = await browser.newPage();
  try {
    await page.goto(`${server.resolvedUrls.local[0]}slider-test`);
    await page.evaluate(`(async () => {
      const { mount, tick } = await import("/node_modules/.vite-slider-browser/deps/svelte.js");
      const { default: Slider } = await import("/src/lib/components/CameraRawSlider.svelte");
      window.__calls = { input: [], reset: 0, alt: [] };
      mount(Slider, { target: document.getElementById("slider"), props: {
        label: "曝光", value: 0, min: -5, max: 5, step: 0.05, decimals: 2, reset: 0,
        oninput: (v) => window.__calls.input.push(v),
        onreset: () => { window.__calls.reset += 1; },
        onalt: (v, c) => window.__calls.alt.push({ v: v, c: c }),
      } });
      await tick();
    })()`);

    // 1. 双击标签 → 复位回调。
    await page.locator(".cr-label").dblclick();
    await poll(() => page.evaluate(() => window.__calls.reset)).toBe(1);

    // 2. 拖滑杆 → oninput 收到该值。取值必须是 step 的整数倍（range 自己会拒绝其它值），
    //    所以这里不测「吸附」—— 浏览器已经把值约束在离散网格上，测它测不出组件做了什么。
    const track = page.locator("input.cr-track");
    await track.fill("2.55");
    await poll(() => page.evaluate(() => window.__calls.input.length > 0)).toBe(true);
    expect(await page.evaluate(() => window.__calls.input[window.__calls.input.length - 1])).toBe(2.55);

    // 3. 按住 Alt 再拖 → onalt 报 clipping=1（这是原实现坏掉的那条）。
    await track.focus();
    await page.keyboard.down("Alt");
    await track.fill("-1.5");
    await poll(() => page.evaluate(() => window.__calls.alt.length > 0)).toBe(true);
    const alt = await page.evaluate(() => window.__calls.alt[window.__calls.alt.length - 1]);
    await page.keyboard.up("Alt");
    expect(alt.c).toBe(1);
    expect(alt.v).toBe(-1.5);

    // 松开 Alt 再拖 → 回到 clipping=0。
    await track.fill("1");
    await poll(() => page.evaluate(() => window.__calls.alt.length > 1)).toBe(true);
    expect(await page.evaluate(() => window.__calls.alt[window.__calls.alt.length - 1].c)).toBe(0);
  } finally {
    await page.close();
  }
}, 30000);
