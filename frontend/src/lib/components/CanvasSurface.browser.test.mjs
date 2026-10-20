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

// 票 30/S05：选区拖到视口边缘时自动滚动。
//
// `edgeAutoScroll` 是现成的纯函数（`selection.test.ts` 有单测），但此前**零消费者** —— 拖选区到边缘
// 什么都不会发生。这条钉住接线：贴边拖 → 视口平移；在中间拖 → 不平移（后半句才是判别性的，
// 一个「总是平移」的实现过不了它）。
it("auto-scrolls the viewport when a marquee drag reaches the edge", async () => {
  const page = await browser.newPage({ viewport: { width: 900, height: 700 } });
  try {
    await page.addInitScript(() => Object.defineProperty(navigator, "gpu", { value: undefined }));
    await page.goto(`${server.resolvedUrls.local[0]}canvas-preview-test`);
    const body = { contentType: "image/png", body: Buffer.from([0]) };
    await page.route("**/render/**", (route) => route.fulfill(body));
    await page.route("**/pixel/**", (route) => route.fulfill(body));
    await page.evaluate(`(async () => {
      const { mount, tick } = await import("/node_modules/.vite-canvas-browser/deps/svelte.js");
      const { default: Surface } = await import("/src/lib/components/CanvasSurface.svelte");
      const { applyDocumentSnapshot } = await import("/src/lib/state/document.ts");
      const { workspace } = await import("/src/lib/state/workspace.ts");
      const { activeTool, toolModes } = await import("/src/lib/state/tools.ts");
      const { viewport } = await import("/src/lib/state/viewport.ts");
      viewport.subscribe((value) => { window.__viewport = value; });
      const doc = { documentID: "tab-a", width: 400, height: 400, resolution: 72, activeLayerID: "layer", layers: [{
        id: "layer", name: "Layer", isVisible: true, opacity: 1, blendMode: "Normal", imageFile: "layer.png",
        transform: { origin: [0, 0], size: [400, 400], sampling: "Nearest" } }] };
      const tab = { id: "tab-a", name: "Marquee", width: 400, height: 400, resolution: 72, dirty: false };
      workspace.set({ activeId: "tab-a", tabs: [tab] });
      applyDocumentSnapshot(JSON.stringify({ rev: 1, filterRev: 0, doc }));
      activeTool.set("marquee");
      toolModes.update((value) => ({ ...value, marquee: "rect" }));
      mount(Surface, { target: document.getElementById("canvas"), props: { doc: tab } });
      await tick();
    })()`);
    // 余量是按**舞台**（.canvas-stage）的边缘算的，不是按文档区域。
    const stage = page.locator(".canvas-stage");
    await poll(() => stage.isVisible()).toBe(true);
    const box = await stage.boundingBox();
    const midY = box.y + box.height / 2;

    // 中间拖：不该平移。
    const before = await page.evaluate(() => ({ panX: window.__viewport.panX, panY: window.__viewport.panY }));
    await page.mouse.move(box.x + box.width / 2, midY);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width / 2 + 40, midY + 40, { steps: 4 });
    await page.waitForTimeout(250);
    const midDrag = await page.evaluate(() => ({ panX: window.__viewport.panX, panY: window.__viewport.panY }));
    expect(midDrag).toEqual(before);

    // 贴右边缘拖：视口应当往左平移（panX 增大）。
    await page.mouse.move(box.x + box.width - 8, midY, { steps: 4 });
    await page.waitForTimeout(350);
    const edgeDrag = await page.evaluate(() => window.__viewport.panX);
    await page.mouse.up();
    expect(edgeDrag).toBeGreaterThan(midDrag.panX + 0.5);
    // 松手后停止：再等一会，panX 不再变化。
    await page.waitForTimeout(250);
    const after = await page.evaluate(() => window.__viewport.panX);
    await page.waitForTimeout(250);
    expect(await page.evaluate(() => window.__viewport.panX)).toBe(after);
  } finally {
    await page.close();
  }
}, 30000);
// 票 53/S02 的**显示端**：像素边描迹出的闭环必须真的变成画布上的蚂蚁线。
//
// 内核有黄金基准、outlineLoops 有单测、「点击 → 选区装进 store」也有用例，
// 但「store 里的 loops → 蚂蚁线 SVG」这一段此前没人钉住：删掉覆盖层、闭环不
// 闭合（去掉 Z）、或退回「每个闭环一个 <path>」，现有测试都不会报错。这条用
// 左半边带一个洞的 8×8 蒙版，断言描迹结果原样出现在 SVG 上 —— 两条路径（白色
// 实线打底 + 黑色虚线在上面走），两个闭环是同一条复合路径里的两个子路径。
it("draws the traced pixel edges as animated marching ants", async () => {
  const page = await browser.newPage();
  try {
    await page.addInitScript(() => Object.defineProperty(navigator, "gpu", { value: undefined }));
    await page.goto(`${server.resolvedUrls.local[0]}canvas-preview-test`);
    const mask = Buffer.alloc(8 * 8);
    for (let y = 0; y < 8; y++) for (let x = 0; x < 4; x++) mask[y * 8 + x] = 255;
    mask[2 * 8 + 2] = 0;
    const maskB64 = mask.toString("base64");
    const body = { contentType: "image/png", body: Buffer.from([0]) };
    await page.route("**/render/**", (route) => route.fulfill(body));
    await page.route("**/pixel/**", (route) => route.fulfill(body));
    await page.evaluate(`(async () => {
      const { mount, tick } = await import("/node_modules/.vite-canvas-browser/deps/svelte.js");
      const { default: Surface } = await import("/src/lib/components/CanvasSurface.svelte");
      const { applyDocumentSnapshot } = await import("/src/lib/state/document.ts");
      const { workspace } = await import("/src/lib/state/workspace.ts");
      const { activeTool, toolModes } = await import("/src/lib/state/tools.ts");
      const doc = { documentID: "tab-a", width: 8, height: 8, resolution: 72, activeLayerID: "layer", layers: [{
        id: "layer", name: "Layer", isVisible: true, opacity: 1, blendMode: "Normal", imageFile: "layer.png",
        transform: { origin: [0, 0], size: [8, 8], sampling: "Nearest" } }] };
      const tab = { id: "tab-a", name: "Ants", width: 8, height: 8, resolution: 72, dirty: false };
      window.go = { bridge: { Service: {
        SelectWandAt: async (tabID) => JSON.stringify({ tabId: tabID, selection: { w: 8, h: 8, mask: "${maskB64}" } }),
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
    // 白线 + 黑虚线：两个节点；两个闭环合成一条复合路径（而不是一个闭环一个节点）。
    await poll(() => page.locator(".ants path").count()).toBe(2);
    const paths = await page.evaluate(`(() => Array.from(document.querySelectorAll(".ants path")).map((path) => {
      const style = getComputedStyle(path);
      const dash = style.strokeDasharray === "none" ? [] : style.strokeDasharray.split(/[ ,]+/).filter(Boolean).map(parseFloat);
      return { d: path.getAttribute("d"), stroke: style.stroke, width: parseFloat(style.strokeWidth), dash };
    }))()`);
    // 外框顺时针、洞逆时针，边走边闭合：与 Go wand_trace / outlineLoops 的约定一致。
    const outline = "M 0 0 L 4 0 L 4 8 L 0 8 Z M 2 2 L 2 3 L 3 3 L 3 2 Z";
    expect(paths.map((path) => path.d)).toEqual([outline, outline]);
    expect(paths).toEqual([
      { d: outline, stroke: "rgb(255, 255, 255)", width: 1, dash: [] },
      { d: outline, stroke: "rgb(0, 0, 0)", width: 1, dash: [4, 4] },
    ]);
    // 蚂蚁线得真的「走」：原版每 0.12s 把相位推 1px（0.96s 一个周期）。
    const dashOffset = () => page.evaluate(`(() => getComputedStyle(document.querySelector(".ants .ants-dashes")).strokeDashoffset)()`);
    const first = await dashOffset();
    await poll(async () => (await dashOffset()) !== first).toBe(true);
  } finally {
    await page.close();
  }
}, 30000);

// ---------------------------------------------------------------------------
// 票 53/S05：选区的鼠标拖动搬运（浮动选区）
//
// 「拖到视口边缘自动滚动」与「方向键搬运选区像素」都已接线，但「在选区内按下并
// 拖动」此前没有入口：画布会把这次按下当成新建选区。这四条钉住接线，语义跟原版
// 一致（EditorCanvas.swift:2418-2423 / SelectionEdits.swift:168-212）：
// 普通拖动只移选区轮廓（无 RPC、无撤销步骤），⌘/Ctrl 拖动才搬像素（与方向键同一条
// Go 通道），Esc / 右键取消不提交，且「在选区外按下仍然新建选区」不变。

const SELECTION_MASK_B64 = (() => {
  const mask = Buffer.alloc(8 * 8);
  for (let y = 1; y <= 3; y++) for (let x = 1; x <= 3; x++) mask[y * 8 + x] = 255;
  return mask.toString("base64");
})();
// Go 回复里的权威选区：左半边跟着位移 +2px（x 3..5）。
const MOVED_SELECTION_MASK_B64 = (() => {
  const mask = Buffer.alloc(8 * 8);
  for (let y = 1; y <= 3; y++) for (let x = 3; x <= 5; x++) mask[y * 8 + x] = 255;
  return mask.toString("base64");
})();
// 初始选区（x/y 各 1..3）的蚂蚁线闭环；向右拖 2 个文档像素后的跟手位置。
const ANTS_AT_ORIGIN = "M 1 1 L 4 1 L 4 4 L 1 4 Z";
const ANTS_MOVED_2PX = "M 3 1 L 6 1 L 6 4 L 3 4 Z";

// 挂一块 8×8 的画布，marquee 工具，预置初始选区，并把 MoveSelectedPixels 换成
// 记录调用的假实现。返回文档像素 → 页面坐标的换算函数。
async function openFloatingSelectionPage(page) {
  await page.addInitScript(() => Object.defineProperty(navigator, "gpu", { value: undefined }));
  await page.goto(`${server.resolvedUrls.local[0]}canvas-preview-test`);
  const body = { contentType: "image/png", body: Buffer.from([0]) };
  await page.route("**/render/**", (route) => route.fulfill(body));
  await page.route("**/pixel/**", (route) => route.fulfill(body));
  await page.evaluate(`(async () => {
    const { mount, tick } = await import("/node_modules/.vite-canvas-browser/deps/svelte.js");
    const { default: Surface } = await import("/src/lib/components/CanvasSurface.svelte");
    const { applyDocumentSnapshot } = await import("/src/lib/state/document.ts");
    const { workspace } = await import("/src/lib/state/workspace.ts");
    const { activeTool, toolModes } = await import("/src/lib/state/tools.ts");
    const { currentSelection, select } = await import("/src/lib/state/selection.ts");
    currentSelection.subscribe((value) => { window.__selection = value; });
    const doc = { documentID: "tab-a", width: 8, height: 8, resolution: 72, activeLayerID: "layer", layers: [{
      id: "layer", name: "Layer", isVisible: true, opacity: 1, blendMode: "Normal", imageFile: "layer.png",
      transform: { origin: [0, 0], size: [8, 8], sampling: "Nearest" } }] };
    const tab = { id: "tab-a", name: "Floating", width: 8, height: 8, resolution: 72, dirty: false };
    window.__moveCalls = [];
    window.go = { bridge: { Service: {
      MoveSelectedPixels: async (selectionJSON, dx, dy) => {
        window.__moveCalls.push({ selectionJSON, dx, dy });
        // applyDocumentSnapshot 只接受前进的 rev；每次回复递增一档。
        return JSON.stringify({ rev: 1 + window.__moveCalls.length, filterRev: 0, doc,
          selection: { x: 0, y: 0, w: 8, h: 8, mask: "${MOVED_SELECTION_MASK_B64}" } });
      },
    } } };
    workspace.set({ activeId: "tab-a", tabs: [tab] });
    applyDocumentSnapshot(JSON.stringify({ rev: 1, filterRev: 0, doc }));
    activeTool.set("marquee");
    toolModes.update((value) => ({ ...value, marquee: "rect" }));
    const mask = Uint8Array.from(atob("${SELECTION_MASK_B64}"), (c) => c.charCodeAt(0));
    // 带羽化：普通拖动移轮廓时羽化必须跟着走（原版 moveSelection 保留 feather）。
    currentSelection.set({ ...select("replace", null, { width: 8, height: 8, mask, loops: [] }), feather: 3 });
    mount(Surface, { target: document.getElementById("canvas"), props: { doc: tab } });
    await tick();
  })()`);
  await poll(() => page.locator(".doc").isVisible()).toBe(true);
  // 文档像素 → 页面坐标：.doc 的左上角 + (x + 0.5) 个文档像素的实际屏幕尺寸。
  const docBox = await page.locator(".doc").boundingBox();
  const pixel = (x, y) => ({
    x: docBox.x + ((x + 0.5) * docBox.width) / 8,
    y: docBox.y + ((y + 0.5) * docBox.height) / 8,
  });
  return { docBox, pixel };
}

// 在 (fromX, fromY) 按下、拖到 (+dx, +dy)，返回松手前状态；不负责 mouse.up。
async function dragFrom(page, pixel, fromX, fromY, dx, dy) {
  const start = pixel(fromX, fromY);
  const end = pixel(fromX + dx, fromY + dy);
  await page.mouse.move(start.x, start.y);
  await page.mouse.down();
  await page.mouse.move(end.x, end.y, { steps: 4 });
  return { start, end };
}

it("moves only the selection outline on a plain drag inside the selection", async () => {
  const page = await browser.newPage();
  try {
    const { pixel } = await openFloatingSelectionPage(page);
    const ants = page.locator(".ants .ants-line");
    await poll(() => ants.count()).toBe(1);
    await poll(() => ants.getAttribute("d")).toBe(ANTS_AT_ORIGIN);

    // 悬停在选区内 → move 光标；选区外仍是选框光标（原版 lassoCursor 的 replace 分支）。
    const stageCursor = () => page.evaluate(() => getComputedStyle(document.querySelector(".canvas-stage")).cursor);
    await page.mouse.move(pixel(2, 2).x, pixel(2, 2).y);
    await poll(stageCursor).toBe("move");
    await page.mouse.move(pixel(6, 6).x, pixel(6, 6).y);
    await page.waitForTimeout(100);
    expect(await stageCursor()).not.toBe("move");

    await dragFrom(page, pixel, 2, 2, 2, 0);
    // 拖动中轮廓按整像素跟手。
    await poll(() => ants.getAttribute("d")).toBe(ANTS_MOVED_2PX);
    expect(await page.evaluate(() => getComputedStyle(document.querySelector(".canvas-stage")).cursor)).toBe("move");

    await page.mouse.up();
    // 普通拖动只移选区轮廓：选区本身（mask + loops + feather）整体 +2px，像素不动，
    // 也没有任何 RPC / 撤销步骤（原版 beginSelectionMove/moveSelection 的语义）。
    await poll(() => page.evaluate(`(() => {
      const sel = window.__selection;
      return !!sel && sel.mask[2 * 8 + 4] === 255 && sel.mask[2 * 8 + 2] === 0
        && sel.feather === 3 && sel.loops[0][0].x === 3;
    })()`)).toBe(true);
    expect(await page.evaluate(() => window.__moveCalls.length)).toBe(0);

    // 反例（原来的行为不能被改掉）：在选区**外**按下拖动 = 新建选区，也不搬像素。
    await page.mouse.move(pixel(6, 6).x, pixel(6, 6).y);
    await page.mouse.down();
    await page.mouse.move(pixel(7, 7).x, pixel(7, 7).y, { steps: 4 });
    await page.mouse.up();
    await poll(() => page.evaluate(`(() => {
      const sel = window.__selection;
      return !!sel && sel.mask[7 * 8 + 7] === 255 && sel.mask[4 * 8 + 4] === 0;
    })()`)).toBe(true);
    expect(await page.evaluate(() => window.__moveCalls.length)).toBe(0);
  } finally {
    await page.close();
  }
}, 30000);

it("moves the selected pixels on a Ctrl/Cmd drag inside the selection", async () => {
  const page = await browser.newPage();
  try {
    const { pixel } = await openFloatingSelectionPage(page);
    const ants = page.locator(".ants .ants-line");
    await poll(() => ants.count()).toBe(1);
    // 浏览器里的 ⌘/主修饰键：仓库按 chords.ts 的 Cmd→Ctrl 映射，两者都认；Windows 上按 Ctrl。
    await page.keyboard.down("Control");
    await dragFrom(page, pixel, 2, 2, 2, 0);
    // 拖动中轮廓同样跟手。
    await poll(() => ants.getAttribute("d")).toBe(ANTS_MOVED_2PX);
    await page.mouse.up();
    await page.keyboard.up("Control");
    await poll(() => page.evaluate(() => window.__moveCalls.length)).toBe(1);
    const [call] = await page.evaluate(() => window.__moveCalls);
    // 与方向键搬运同一个 Go 方法：整像素位移。
    expect(call.dx).toBe(2);
    expect(call.dy).toBe(0);
    // 负载是**原始**选区（像素此刻还在的位置），不是跟手后的位置；羽化随行李走。
    const payload = JSON.parse(call.selectionJSON);
    expect([payload.w, payload.h]).toEqual([8, 8]);
    expect(payload.feather).toBe(3);
    const sent = Buffer.from(payload.mask, "base64");
    expect(sent[2 * 8 + 2]).toBe(255);
    expect(sent[2 * 8 + 4]).toBe(0);
    // 回复里的权威选区装回前端：Go 把选区跟着像素一起移了。
    await poll(() => page.evaluate(`(() => {
      const sel = window.__selection;
      return !!sel && sel.mask[3 * 8 + 3] === 255 && sel.mask[1 * 8 + 1] === 0;
    })()`)).toBe(true);
  } finally {
    await page.close();
  }
}, 30000);

it("cancels a Ctrl/Cmd pixel drag on Escape without moving pixels", async () => {
  const page = await browser.newPage();
  try {
    const { pixel } = await openFloatingSelectionPage(page);
    const ants = page.locator(".ants .ants-line");
    await poll(() => ants.count()).toBe(1);
    // 用 ⌘/Ctrl 拖动：这条路径松手会发 RPC，所以「取消后 0 次调用」才是判别性的。
    await page.keyboard.down("Control");
    await dragFrom(page, pixel, 2, 2, 2, 0);
    await poll(() => ants.getAttribute("d")).toBe(ANTS_MOVED_2PX);
    await page.keyboard.press("Escape");
    await page.keyboard.up("Control");
    // 轮廓回到原处，像素一个都没动。
    await poll(() => ants.getAttribute("d")).toBe(ANTS_AT_ORIGIN);
    await page.mouse.up();
    await page.waitForTimeout(200);
    expect(await page.evaluate(() => window.__moveCalls.length)).toBe(0);
  } finally {
    await page.close();
  }
}, 30000);

it("cancels a Ctrl/Cmd pixel drag on right-click without moving pixels", async () => {
  const page = await browser.newPage();
  try {
    const { pixel } = await openFloatingSelectionPage(page);
    const ants = page.locator(".ants .ants-line");
    await poll(() => ants.count()).toBe(1);
    await page.keyboard.down("Control");
    const { end } = await dragFrom(page, pixel, 2, 2, 2, 0);
    await poll(() => ants.getAttribute("d")).toBe(ANTS_MOVED_2PX);
    // 右键按下（左键还按着）→ 取消；再动一下会走 pointermove 那条判定。
    await page.mouse.down({ button: "right" });
    await page.mouse.move(end.x + 8, end.y, { steps: 2 });
    await page.mouse.up({ button: "right" });
    await poll(() => ants.getAttribute("d")).toBe(ANTS_AT_ORIGIN);
    await page.mouse.up();
    await page.keyboard.up("Control");
    await page.waitForTimeout(200);
    expect(await page.evaluate(() => window.__moveCalls.length)).toBe(0);
  } finally {
    await page.close();
  }
}, 30000);
