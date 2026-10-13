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
