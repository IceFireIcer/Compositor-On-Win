import { afterAll, beforeAll, expect, it } from "vitest";
import { createServer } from "vite";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";

// Browser updates can exceed Vitest's 1s default under concurrent CI load.
const poll = (callback) => expect.poll(callback, { timeout: 10000 });
const require = createRequire(import.meta.url);
const { chromium } = require(path.join(process.env.PLAYWRIGHT_ROOT ?? path.join(tmpdir(), "compositor-playwright-smoke"), "node_modules/playwright"));
let server, browser;

beforeAll(async () => {
  // Concurrent browser fixtures must not overwrite each other's Svelte cache.
  server = await createServer({ cacheDir: "node_modules/.vite-canvas-browser", server: { port: 0, strictPort: false }, optimizeDeps: { noDiscovery: true, include: [] }, plugins: [{ name: "canvas-preview-browser-fixture", configureServer(vite) {
    vite.middlewares.use("/canvas-preview-test", (_req, res) => {
      res.setHeader("Content-Type", "text/html");
      res.end('<!doctype html><html><body style="margin:0"><div id="canvas" style="width:400px;height:400px"></div></body></html>');
    });
  } }] });
  await server.listen();
  browser = await chromium.launch({ executablePath: process.env.EDGE_PATH ?? "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", headless: true, args: ["--enable-unsafe-webgpu"] });
}, 30000);

afterAll(async () => {
  await browser?.close();
  await server?.close();
}, 30000);

it.each(["cpu", "gpu"])("shows an erased full-document preview without the committed %s image beneath it", async (track) => {
  const page = await browser.newPage();
  try {
    if (track === "cpu") {
      await page.addInitScript(() => Object.defineProperty(navigator, "gpu", { value: undefined }));
    }
    await page.goto(`${server.resolvedUrls.local[0]}canvas-preview-test`);
    const pngs = await page.evaluate(async () => {
      const canvas = document.createElement("canvas");
      canvas.width = canvas.height = 8;
      const context = canvas.getContext("2d");
      const transparent = canvas.toDataURL().split(",")[1];
      context.fillStyle = "red";
      context.fillRect(0, 0, 8, 8);
      return { transparent, red: canvas.toDataURL().split(",")[1] };
    });
    // The backend boundary supplies a complete transparent erase preview,
    // while the committed document stays opaque red until pointer-up.
    await page.route("**/render/**", async (route) => {
      const url = new URL(route.request().url());
      const rev = Number(url.searchParams.get("v")) || 1;
      if (url.pathname.endsWith(".json")) {
        await route.fulfill({ json: {
          rev, filterRev: 0, filterPreview: false,
          doc: { width: 8, height: 8, layers: [{ id: "layer", isVisible: true, opacity: 1, blendMode: "Normal", imageFile: "layer.png", transform: { origin: [0, 0], size: [8, 8], rotation: 0, flipX: false, flipY: false } }] },
          assets: [{ layerId: "layer", kind: "image", url: `/pixel/tab-a/layer/image.rgba?v=${rev}`, format: "rgba8-premultiplied-srgb", width: 8, height: 8, hash: String(rev) }],
        } });
      } else {
        await route.fulfill({ contentType: "image/png", body: Buffer.from(rev === 1 ? pngs.red : pngs.transparent, "base64") });
      }
    });
    await page.route("**/pixel/**", async (route) => {
      const url = new URL(route.request().url());
      if (url.pathname.startsWith("/pixel/stroke/")) {
        await route.fulfill({ contentType: "image/png", body: Buffer.from(pngs.transparent, "base64") });
      } else {
        const rev = Number(url.searchParams.get("v")) || 1;
        const pixels = Buffer.alloc(8 * 8 * 4);
        if (rev === 1) for (let i = 0; i < pixels.length; i += 4) { pixels[i] = 255; pixels[i + 3] = 255; }
        await route.fulfill({ body: pixels, headers: {
          "Content-Type": "application/octet-stream", "X-Pixel-Width": "8", "X-Pixel-Height": "8", "X-Pixel-Format": "rgba8-premultiplied-srgb", "X-Pixel-Revision": String(rev),
        } });
      }
    });
    await page.evaluate(`(async () => {
      const { mount, tick } = await import("/node_modules/.vite-canvas-browser/deps/svelte.js");
      const { default: Surface } = await import("/src/lib/components/CanvasSurface.svelte");
      const { applyDocumentSnapshot } = await import("/src/lib/state/document.ts");
      const { workspace } = await import("/src/lib/state/workspace.ts");
      const { activeTool, toolModes } = await import("/src/lib/state/tools.ts");
      const doc = { documentID: "tab-a", width: 8, height: 8, resolution: 72, activeLayerID: "layer", layers: [{ id: "layer", name: "Pixels", isVisible: true, opacity: 1, blendMode: "Normal", imageFile: "layer.png", transform: { origin: [0, 0], size: [8, 8] } }] };
      const tab = { id: "tab-a", name: "Preview", width: 8, height: 8, resolution: 72, dirty: false };
      const snapshot = (rev) => JSON.stringify({ rev, filterRev: 0, doc });
      window.go = { bridge: { Service: {
        BeginPaintStroke: async () => "erase-token",
        PaintStrokePoint: async () => undefined,
        EndPaintStroke: async () => snapshot(2),
      } } };
      workspace.set({ activeId: "tab-a", tabs: [tab] });
      applyDocumentSnapshot(snapshot(1));
      activeTool.set("brush");
      toolModes.update(value => ({ ...value, brush: "erase" }));
      mount(Surface, { target: document.getElementById("canvas"), props: { doc: tab } });
      await tick();
    })()`);
    const committed = page.locator(track === "gpu" ? ".doc canvas.render" : '.doc img.render[src^="/render/"]');
    await poll(() => committed.isVisible()).toBe(true);
    await page.locator(".doc").hover();
    await page.mouse.down();
    const preview = page.locator('.doc img.render[src^="blob:"]');
    await poll(() => preview.evaluateAll(images => images.some(image => image.complete && image.naturalWidth === 8))).toBe(true);
    // A full alpha composite replaces the previous frame. Otherwise the
    // erased area reveals old red pixels and translucent pixels double up.
    await poll(() => page.locator(".doc .render:visible").count()).toBe(1);
    expect(await page.locator(".doc canvas.render").isVisible()).toBe(false);
    expect(await page.locator('.doc img.render[src^="/render/"]').count()).toBe(0);
    await page.mouse.up();
    await poll(() => preview.count()).toBe(0);
    await poll(() => page.locator(".doc .render:visible").count()).toBe(1);
  } finally {
    await page.close();
  }
}, 30000);

// 票 15/37 的**接线**：魔棒工具下点画布，必须真的走到 SelectWandAt，并把回来的蒙版装进选区。
//
// 内核与黄金基准另有 Go 侧测试，但「画布有没有这条分支」只有挂真实组件才测得到 ——
// 对等矩阵 T04 曾长期写着「画布无魔棒分支」，实际早已接线；这条钉住它，防再退回去。
it("runs the wand kernel from a canvas click and installs the selection", async () => {
  const page = await browser.newPage();
  try {
    await page.addInitScript(() => Object.defineProperty(navigator, "gpu", { value: undefined }));
    await page.goto(`${server.resolvedUrls.local[0]}canvas-preview-test`);
    const mask = Buffer.alloc(8 * 8);
    for (let y = 0; y < 8; y++) for (let x = 0; x < 4; x++) mask[y * 8 + x] = 255;
    const maskB64 = mask.toString("base64");
    const body = { contentType: "image/png", body: Buffer.from([0]) };
    await page.route("**/render/**", (route) => route.fulfill(body));
    await page.route("**/pixel/**", (route) => route.fulfill(body));
    // page.evaluate 传**字符串**：传函数会被 Vite 的 SSR 转换破坏（动态 import 会变成
    // __vite_ssr_dynamic_import__），同文件上面那条测试也是这么写的。
    await page.addInitScript((m) => { window.__maskB64 = m; }, maskB64);
    await page.evaluate(`(async () => {
      const { mount, tick } = await import("/node_modules/.vite-canvas-browser/deps/svelte.js");
      const { default: Surface } = await import("/src/lib/components/CanvasSurface.svelte");
      const { applyDocumentSnapshot } = await import("/src/lib/state/document.ts");
      const { workspace } = await import("/src/lib/state/workspace.ts");
      const { activeTool, toolModes } = await import("/src/lib/state/tools.ts");
      const doc = { documentID: "tab-a", width: 8, height: 8, resolution: 72, activeLayerID: "layer", layers: [{
        id: "layer", name: "Layer", isVisible: true, opacity: 1, blendMode: "Normal", imageFile: "layer.png",
        transform: { origin: [0, 0], size: [8, 8], sampling: "Nearest" } }] };
      const tab = { id: "tab-a", name: "Wand", width: 8, height: 8, resolution: 72, dirty: false };
      window.__wandCalls = [];
      const { currentSelection } = await import("/src/lib/state/selection.ts");
      currentSelection.subscribe((value) => { window.__selection = value; });
      window.go = { bridge: { Service: {
        SelectWandAt: async (tabID, x, y, tolerance, radius, contiguous, allLayers) => {
          window.__wandCalls.push({ tabID, x, y, tolerance, radius, contiguous, allLayers });
          return JSON.stringify({ tabId: tabID, selection: { w: 8, h: 8, mask: "${maskB64}" } });
        },
      } } };
      workspace.set({ activeId: "tab-a", tabs: [tab] });
      applyDocumentSnapshot(JSON.stringify({ rev: 1, filterRev: 0, doc }));
      activeTool.set("wand");
      toolModes.update((value) => ({ ...value, wand: "wand" }));
      mount(Surface, { target: document.getElementById("canvas"), props: { doc: tab } });
      await tick();
    })()`);
    await poll(() => page.locator(".doc").isVisible()).toBe(true);
    await page.locator(".doc").click({ position: { x: 2, y: 2 } });
    await poll(async () => (await page.evaluate(() => window.__wandCalls.length)) === 1).toBe(true);
    const calls = await page.evaluate(() => window.__wandCalls);
    expect(calls[0].tabID).toBe("tab-a");
    // 选区是异步装进 store 的（后端 reply → selectionFromMask → set），所以轮询等它落地。
    await poll(() => page.evaluate(`(() => !!window.__selection)()`)).toBe(true);
    const selected = await page.evaluate(`(() => {
      const sel = window.__selection;
      if (!sel) return null;
      return { width: sel.width, height: sel.height, leftTop: sel.mask[0], rightTop: sel.mask[4], loopCount: sel.loops.length };
    })()`);
    // 蒙版原样装进选区（左半边选中、右半边没有），并且描迹出了闭环。
    expect(selected).toEqual({ width: 8, height: 8, leftTop: 255, rightTop: 0, loopCount: 1 });
  } finally {
    await page.close();
  }
}, 30000);
