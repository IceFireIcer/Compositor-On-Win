import { afterAll, beforeAll, beforeEach, expect, it, vi } from "vitest";
import { createServer } from "vite";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";

// 票 92/U39：整壳在**极小窗口**下不溢出、不互相遮挡。
//
// 这个测试挂的是整个 App（不是单个组件），因为要测的正是各区块之间的挤压。后端整个 mock 掉：
// window.runtime 用 Proxy 兜住（任何事件订阅都返回一个取消函数），window.go 给 onMount 要的那几个。
const poll = (callback) => expect.poll(callback, { timeout: 10000 });
vi.setConfig({ testTimeout: 40000 });
const require = createRequire(import.meta.url);
const { chromium } = require(path.join(process.env.PLAYWRIGHT_ROOT ?? path.join(tmpdir(), "compositor-playwright-smoke"), "node_modules", "playwright"));
let server, browser, page;

beforeAll(async () => {
  server = await createServer({ cacheDir: "node_modules/.vite-shell-browser", server: { port: 0, strictPort: false }, optimizeDeps: { noDiscovery: true, include: [] }, plugins: [{ name: "shell-browser-fixture", configureServer(vite) {
    vite.middlewares.use("/shell-test", (_req, res) => {
      res.setHeader("Content-Type", "text/html");
      res.end('<!doctype html><html><head><style>html,body{margin:0;height:100%}</style></head><body><div id="app"></div></body></html>');
    });
  } }] });
  await server.listen();
  browser = await chromium.launch({ executablePath: process.env.EDGE_PATH ?? "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", headless: true });
}, 40000);

afterAll(async () => {
  await browser?.close();
  await server?.close();
}, 40000);

beforeEach(async () => {
  await page?.close();
  page = await browser.newPage({ viewport: { width: 900, height: 600 } });
  page.on("pageerror", (err) => console.log("PAGEERROR: " + String(err)));
  await page.addInitScript(() => {
    // 任何事件订阅都返回一个取消函数；任何窗口调用都是 no-op。
    const cancelable = () => () => undefined;
    window.runtime = new Proxy({}, { get: () => cancelable });
    window.go = {
      bridge: {
        Workspace: { Snapshot: async () => ({ activeId: "tab-a", tabs: [{ id: "tab-a", name: "未命名", dirty: false }] }) },
        Service: {
          Version: async () => "0.1.0-test",
          ReportReady: async () => undefined,
          DocumentSnapshot: async () => JSON.stringify({ rev: 0, doc: null }),
          RecentProjects: async () => [],
          SyncWindowChrome: async () => undefined,
        },
        WindowStore: { Save: async () => undefined },
      },
    };
    Object.defineProperty(navigator, "gpu", { value: undefined });
  });
  await page.route("**/render/**", (route) => route.fulfill({ json: { rev: 0, filterRev: 0, doc: null, assets: [] } }));
  await page.goto(`${server.resolvedUrls.local[0]}shell-test`);
  await page.evaluate(`(async () => {
    const { mount, tick } = await import("/node_modules/.vite-shell-browser/deps/svelte.js");
    // 样式表由 main.ts 引入；这里直接挂 App，所以要自己带上，否则 html/body/#app 没有
    // height:100%，.app 的高度会撑到内容高（第一版就是这么误报的）。
    await import("/src/app.css");
    const { default: App } = await import("/src/App.svelte");
    mount(App, { target: document.getElementById("app") });
    await tick();
  })()`);
  await page.waitForTimeout(500);
  // 并发跑整套时，建 page + 导航 + 挂整壳会超过 hook 的默认超时（约 10s），显式放宽。
}, 40000);

it("fits a small window without overflowing or overlapping", async () => {
  const errors = [];
  page.on("pageerror", (err) => errors.push(String(err)));

  for (const [w, h] of [[900, 600], [640, 480], [480, 360]]) {
    await page.setViewportSize({ width: w, height: h });
    await page.waitForTimeout(200);
    const measured = await page.evaluate(() => {
      const vw = document.documentElement.clientWidth;
      const vh = document.documentElement.clientHeight;
      const outside = [];
      for (const el of document.querySelectorAll("body *")) {
        const style = getComputedStyle(el);
        if (style.display === "none" || style.visibility === "hidden" || style.position === "fixed") continue;
        const r = el.getBoundingClientRect();
        if (r.width === 0 || r.height === 0) continue;
        // 越界的那一侧若有祖先能滚，就**够得着**，不算遮挡 —— 但只看越界的那一侧。
        const needsX = r.right > vw + 1 || r.left < -1;
        const needsY = r.bottom > vh + 1 || r.top < -1;
        let scrollable = false;
        for (let p = el.parentElement; p; p = p.parentElement) {
          const ps = getComputedStyle(p);
          const canX = (ps.overflowX === "auto" || ps.overflowX === "scroll") && p.scrollWidth > p.clientWidth;
          const canY = (ps.overflowY === "auto" || ps.overflowY === "scroll") && p.scrollHeight > p.clientHeight;
          if ((needsX && canX) || (needsY && canY)) {
            scrollable = true;
            break;
          }
        }
        if (scrollable) continue;
        // 允许 1px 的舍入；只报真正跑到视口外、且够不着的元素。
        if (r.right > vw + 1 || r.bottom > vh + 1 || r.left < -1 || r.top < -1) {
          outside.push({
            tag: el.tagName.toLowerCase(),
            cls: String(el.className).slice(0, 40),
            left: Math.round(r.left), top: Math.round(r.top),
            right: Math.round(r.right), bottom: Math.round(r.bottom),
          });
        }
      }
      return {
        vw, vh,
        scrollWidth: document.documentElement.scrollWidth,
        scrollHeight: document.documentElement.scrollHeight,
        outside: outside.slice(0, 5),
        outsideCount: outside.length,
      };
    });
    if (measured.outsideCount > 0 || measured.scrollWidth > measured.vw) {
      console.log(`VIEWPORT ${w}x${h} overflow=${measured.scrollWidth - measured.vw} outside(${measured.outsideCount}): ` + JSON.stringify(measured.outside));
    }
    // 页面本身不该横向溢出。
    expect({ viewport: `${w}x${h}`, overflow: measured.scrollWidth - measured.vw }).toEqual({ viewport: `${w}x${h}`, overflow: 0 });
    // 也不该有元素跑到视口外（截断属于遮挡）。
    expect({ viewport: `${w}x${h}`, outside: measured.outside }).toEqual({ viewport: `${w}x${h}`, outside: [] });
  }
  expect(errors).toEqual([]);
});
