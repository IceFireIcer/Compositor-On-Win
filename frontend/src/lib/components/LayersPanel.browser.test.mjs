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
const { chromium } = require(path.join(process.env.PLAYWRIGHT_ROOT ?? path.join(tmpdir(), "compositor-playwright-smoke"), "node_modules/playwright"));
let server, browser, page;

beforeAll(async () => {
  // Concurrent browser fixtures must not overwrite each other's Svelte cache.
  server = await createServer({ cacheDir: "node_modules/.vite-layers-browser", server: { port: 0, strictPort: false }, optimizeDeps: { noDiscovery: true, include: [] }, plugins: [{ name: "layer-panel-browser-fixture", configureServer(vite) {
  vite.middlewares.use("/layer-panel-test", (_req, res) => {
    res.setHeader("Content-Type", "text/html");
    res.end('<!doctype html><html><body><div id="panel" style="width:300px"></div></body></html>');
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
  await page.goto(`${server.resolvedUrls.local[0]}layer-panel-test`);
  // A string keeps Vitest's SSR transform out of the browser imports.
  await page.evaluate(`(async () => {
    const { mount, tick } = await import("/node_modules/.vite-layers-browser/deps/svelte.js");
    const { default: Panel } = await import("/src/lib/components/LayersPanel.svelte");
    const { document: store, applyDocumentSnapshot } = await import("/src/lib/state/document.ts");
    const { workspace } = await import("/src/lib/state/workspace.ts");
    const { currentSelection } = await import("/src/lib/state/selection.ts");
    const { getCanvasEditTarget } = await import("/src/lib/state/layerTarget.ts");
    window.editTarget = getCanvasEditTarget;
    window.doc = { documentID: "tab-a", width: 4, height: 4, activeLayerID: "b", layers: [
      { id: "a", name: "Alpha", isVisible: true, imageFile: "a.png", maskFile: "a.mask.png", transform: { origin: [0, 0], size: [4, 4] },
        effects: { shadow: { angle: 90, distance: 2, blur: 4, red: 0, green: 0, blue: 0, opacity: 0.5 } } },
      { id: "b", name: "Beta", isVisible: true, imageFile: "b.png", transform: { origin: [0, 0], size: [4, 4] } },
      { id: "g", name: "Folder", isVisible: true, isGroup: true, transform: { origin: [0, 0], size: [4, 4] } },
    ] };
    window.rev = 1;
    window.requests = [];
    window.snapshot = () => JSON.stringify({ rev: window.rev, doc: window.doc });
    window.apply = () => applyDocumentSnapshot(window.snapshot());
    window.workspace = workspace;
    window.currentSelection = currentSelection;
    window.go = { bridge: {
      Workspace: { Snapshot: async () => ({ activeId: window.doc.documentID, tabs: window.tabs }) },
      Service: {
        DocumentSnapshot: async () => window.snapshot(),
        PlaceLayer: async (tab, id, target, placement) => {
          window.requests.push({ op: "place", tab, id, target, placement });
          const layer = window.doc.layers.find(l => l.id === id);
          const targetLayer = window.doc.layers.find(l => l.id === target);
          layer.parentID = placement === "inside" ? target : targetLayer.parentID;
          window.doc.layers = window.doc.layers.filter(l => l.id !== id);
          const index = window.doc.layers.findIndex(l => l.id === target);
          window.doc.layers.splice(placement === "inside" ? window.doc.layers.length : index + (placement === "before" ? 1 : 0), 0, layer);
          window.rev++;
          return window.snapshot();
        },
        LayerOp: async (op, raw) => {
          const payload = JSON.parse(raw);
          window.requests.push({ op, payload });
          if (op === "setActive") window.doc.activeLayerID = payload.id;
          if (op === "setVisible") window.doc.layers.find(l => l.id === payload.id).isVisible = payload.visible;
          if (op === "rename") window.doc.layers.find(l => l.id === payload.id).name = payload.name;
          if (op === "deleteEffect") delete window.doc.layers.find(l => l.id === payload.id).effects[payload.kind];
          window.rev++;
          return window.snapshot();
        },
        SetLayerMask: async (tab, id, op, selection) => {
          window.requests.push({ op, tab, id, selection });
          if (window.maskReply) return window.maskReply;
          const l = window.doc.layers.find(l => l.id === id);
          if (op.startsWith("create")) l.maskFile = id + ".mask.png";
          if (op === "delete") delete l.maskFile;
          window.rev++;
          return window.snapshot();
        },
      },
    } };
    window.tabs = ["tab-a", "tab-b"].map(id => ({ id, name: id, width: 4, height: 4, resolution: 72, dirty: false }));
    workspace.set({ activeId: "tab-a", tabs: window.tabs });
    currentSelection.set(null);
    store.set({ docId: null, rev: 0, filterRev: 0, width: 0, height: 0, resolution: 72, activeLayerID: null, layers: [] });
    window.apply();
    mount(Panel, { target: window.document.getElementById("panel") });
    await tick();
  })()`);
}, 30000);

it("clears a mask target when an external action selects another layer", async () => {
  await page.locator(".mask-row").click({ position: { x: 35, y: 10 } });
  await poll(() => page.locator(".mask-row.active").count()).toBe(1);
  await page.evaluate(() => { window.doc.activeLayerID = "b"; window.rev++; window.apply(); });
  await poll(() => page.locator(".mask-row.active").count()).toBe(0);
  expect(await page.locator(".panel-actions button").last().textContent()).toMatch(/^\s*删除\s*$/);
});

// C53：图层挂着的特效在面板里要看得见、删得掉 —— 从 PSD 带进来的特效此前完全没有行。
it("lists a layer's effects and deletes one from its row", async () => {
  await poll(() => page.locator('[data-layer-id="a"] .effect-row').count()).toBe(1);
  await poll(() => page.locator('[data-layer-id="a"] .effect-name').textContent()).toBe("投影");
  // 没有特效的图层不该长出空行。
  expect(await page.locator('[data-layer-id="b"] .effect-row').count()).toBe(0);

  await page.locator('[data-layer-id="a"] .effect-delete').click();
  await poll(() => page.evaluate(() => window.requests.filter(r => r.op === "deleteEffect").length)).toBe(1);
  expect(await page.evaluate(() => window.requests.find(r => r.op === "deleteEffect").payload))
    .toEqual({ id: "a", kind: "shadow" });
  await poll(() => page.locator('[data-layer-id="a"] .effect-row').count()).toBe(0);
});

it("selects and edits a disabled mask, preserves its target through control keyboard events", async () => {
  await page.evaluate(() => { window.doc.layers[0].maskEnabled = false; window.rev++; window.apply(); });
  await page.locator(".mask-row").click({ position: { x: 35, y: 10 } });
  await poll(() => page.evaluate(() => window.editTarget("tab-a"))).toEqual({ layerID: "a", targetMask: true });
  const eye = page.locator('[data-layer-id="a"] .eye');
  await eye.focus();
  await eye.press("Enter");
  await poll(() => page.evaluate(() => window.doc.layers[0].isVisible)).toBe(false);
  expect(await page.evaluate(() => window.editTarget("tab-a"))).toEqual({ layerID: "a", targetMask: true });
  const name = page.locator('[data-layer-id="a"] .name');
  await name.focus();
  await name.press("Enter");
  await page.locator(".rename").fill("Renamed");
  await page.locator(".rename").press("Enter");
  await poll(() => page.locator('[data-layer-id="a"] .name').textContent()).toMatch(/Renamed/);
  expect(await page.evaluate(() => window.editTarget("tab-a"))).toEqual({ layerID: "a", targetMask: true });
  expect(await page.evaluate(() => window.requests.filter(r => r.op === "setActive").length)).toBe(1);
  await page.locator('[data-layer-id="a"] .row').click({ position: { x: 290, y: 3 } });
  await poll(() => page.evaluate(() => window.editTarget("tab-a")?.targetMask)).toBe(false);
});

it("creates a mask on an inactive layer, selects it, and deletes only the mask", async () => {
  await page.locator('[data-layer-id="b"] .mask-create').selectOption("createHideAll");
  await poll(() => page.evaluate(() => window.editTarget("tab-a"))).toEqual({ layerID: "b", targetMask: true });
  await page.locator(".panel-actions button").last().click();
  await poll(() => page.evaluate(() => window.editTarget("tab-a")?.targetMask)).toBe(false);
  expect(await page.evaluate(() => window.doc.layers.find(l => l.id === "b").maskFile)).toBeUndefined();
  expect(await page.evaluate(() => window.requests.some(r => r.op === "deleteLayer"))).toBe(false);
});

it("does not enable selection masks for a present but empty selection", async () => {
  await page.evaluate(() => window.currentSelection.set({ width: 4, height: 4, mask: new Uint8Array(16), loops: [], active: false }));
  await poll(() => page.locator('[data-layer-id="b"] option[value="createRevealSelection"]').isDisabled()).toBe(true);
  await page.evaluate(() => window.currentSelection.set({ width: 4, height: 4, mask: new Uint8Array(16).fill(128), loops: [], active: true, feather: 2 }));
  await poll(() => page.locator('[data-layer-id="b"] option[value="createRevealSelection"]').isDisabled()).toBe(false);
  await page.locator('[data-layer-id="b"] .mask-create').selectOption("createRevealSelection");
  await poll(() => page.evaluate(() => window.requests.find(r => r.op === "createRevealSelection")?.selection)).toMatch(/"feather":2/);
});

it("ignores mask replies after switching tabs", async () => {
  await page.evaluate(() => {
    window.go.bridge.Service.SetLayerMask = async () => new Promise(resolve => { window.releaseMask = resolve; });
  });
  await page.locator('[data-layer-id="b"] .mask-create').selectOption("createHideAll");
  await poll(() => page.evaluate(() => typeof window.releaseMask)).toBe("function");
  await page.evaluate(() => {
    const stale = window.snapshot();
    window.workspace.set({ activeId: "tab-b", tabs: window.tabs });
    window.doc.documentID = "tab-b";
    window.doc.layers = [{ id: "new", name: "Other tab", transform: { origin: [0,0], size: [4,4] } }];
    window.doc.activeLayerID = "new";
    window.rev = 1; window.apply();
    window.releaseMask(stale);
  });
  await poll(() => page.locator(".name").textContent()).toMatch(/Other tab/);
  expect(await page.evaluate(() => window.editTarget("tab-b"))).toEqual({ layerID: "new", targetMask: false });
});

it("accepts real drag events for reorder and nesting into the existing folder", async () => {
  await page.evaluate(() => {
    const source = window.document.querySelector('[data-layer-id="b"] .row');
    const target = window.document.querySelector('[data-layer-id="g"]');
    const transfer = new DataTransfer();
    source.dispatchEvent(new DragEvent("dragstart", { bubbles: true, dataTransfer: transfer }));
    window.dragEffect = transfer.effectAllowed;
    window.dragMime = transfer.getData("application/x-compositor-layer");
    const rect = target.getBoundingClientRect();
    target.dispatchEvent(new DragEvent("dragover", { bubbles: true, cancelable: true, dataTransfer: transfer, clientY: rect.top + rect.height / 2 }));
    target.dispatchEvent(new DragEvent("drop", { bubbles: true, cancelable: true, dataTransfer: transfer, clientY: rect.top + rect.height / 2 }));
  });
  await poll(() => page.locator('[data-layer-id="b"]').getAttribute("data-depth")).toBe("1");
  expect(await page.evaluate(() => window.dragMime)).toBe("b");
  expect(await page.evaluate(() => window.requests.find(r => r.op === "place"))).toMatchObject({ id: "b", target: "g", placement: "inside", tab: "tab-a" });
  expect(await page.locator(".name").allTextContents()).toEqual(expect.arrayContaining([expect.stringMatching(/Folder/), expect.stringMatching(/Beta/)]));
  await page.evaluate(() => {
    const source = window.document.querySelector('[data-layer-id="b"] .row');
    const target = window.document.querySelector('[data-layer-id="a"]');
    const transfer = new DataTransfer();
    source.dispatchEvent(new DragEvent("dragstart", { bubbles: true, dataTransfer: transfer }));
    const rect = target.getBoundingClientRect();
    target.dispatchEvent(new DragEvent("drop", { bubbles: true, cancelable: true, dataTransfer: transfer, clientY: rect.bottom - 1 }));
  });
  await poll(() => page.locator('[data-layer-id="b"]').getAttribute("data-depth")).toBe("0");
  expect(await page.evaluate(() => window.requests.filter(r => r.op === "place").at(-1).placement)).toBe("after");
});
