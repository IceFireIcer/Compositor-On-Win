import { afterAll, beforeAll, beforeEach, expect, it } from "vitest";
import { createServer } from "vite";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";

/**
 * 命令搜索面板的浏览器回归（票 54）：真实 DOM 里的键盘导航（↑/↓ 环绕、回车
 * 执行、Esc 关闭并把焦点还给打开前的控件）、重名命令、动态菜单、禁用项与
 * 未打开文档。纯匹配/排序/条目构建在 src/lib/commands/*.test.ts 里断言。
 */

// Browser updates can exceed Vitest's 1s default under concurrent CI load.
const poll = (callback) => expect.poll(callback, { timeout: 10000 });
const require = createRequire(import.meta.url);
// Install separately with npm install --prefix <PLAYWRIGHT_ROOT> playwright.
const { chromium } = require(path.join(process.env.PLAYWRIGHT_ROOT ?? path.join(tmpdir(), "compositor-playwright-smoke"), "node_modules/playwright"));
let server, browser, page;

beforeAll(async () => {
  // Concurrent browser fixtures must not overwrite each other's Svelte cache.
  server = await createServer({ cacheDir: "node_modules/.vite-palette-browser", server: { port: 0, strictPort: false }, optimizeDeps: { noDiscovery: true, include: [] }, plugins: [{ name: "command-palette-browser-fixture", configureServer(vite) {
  vite.middlewares.use("/command-palette-test", (_req, res) => {
    res.setHeader("Content-Type", "text/html");
    res.end('<!doctype html><html><body style="margin:0"><button id="before">before</button><div id="palette"></div></body></html>');
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
  await page.goto(`${server.resolvedUrls.local[0]}command-palette-test`);
  // A string keeps Vitest's SSR transform out of the browser imports.
  await page.evaluate(`(async () => {
    const { mount, tick } = await import("/node_modules/.vite-palette-browser/deps/svelte.js");
    const { default: Palette } = await import("/src/lib/components/CommandPalette.svelte");
    const { menuModel } = await import("/src/lib/commands/menuModel.ts");
    const session = await import("/src/lib/commands/session.ts");
    const { document: docStore, EMPTY_DOCUMENT } = await import("/src/lib/state/document.ts");
    const { workspace } = await import("/src/lib/state/workspace.ts");
    const { currentSelection } = await import("/src/lib/state/selection.ts");
    const { activeTool } = await import("/src/lib/state/tools.ts");
    const { installAppShortcuts } = await import("/src/lib/shortcuts/actions.ts");
    window.session = session;
    window.toolState = () => {
      let value;
      const stop = activeTool.subscribe((next) => { value = next; });
      stop();
      return value;
    };
    window.openState = () => {
      let value;
      const stop = session.commandPaletteOpen.subscribe((next) => { value = next; });
      stop();
      return value;
    };
    window.docStore = docStore;
    window.workspace = workspace;
    window.currentSelection = currentSelection;
    window.EMPTY_DOCUMENT = EMPTY_DOCUMENT;
    window.ran = [];
    window.loadDocument = (withDocument) => {
      if (!withDocument) {
        docStore.set({ ...EMPTY_DOCUMENT });
        workspace.set({ tabs: [], activeId: "" });
        return;
      }
      workspace.set({ tabs: [{ id: "d1", name: "Test", width: 100, height: 100, resolution: 72, dirty: false }], activeId: "d1" });
      docStore.set({ docId: "d1", rev: 1, filterRev: 0, width: 100, height: 100, resolution: 72, activeLayerID: "l1", layers: [
        { id: "l1", name: "层1", isVisible: true, isGroup: false, opacity: 1, blendMode: "Normal", parentID: null,
          maskSourceID: null, imageFile: "a.png", adjustment: null, text: null, transform: { origin: [0, 0], size: [100, 100] } },
      ] });
    };
    menuModel.set([
      { label: "编辑", items: [
        { label: "撤销", hint: "Ctrl+Z", run: () => window.ran.push("undo") },
        { label: "重做", hint: "Ctrl+Shift+Z", disabled: true, run: () => window.ran.push("redo") },
        { label: "搜索命令…", run: () => window.ran.push("search") },
      ] },
      { label: "滤镜", items: [
        { label: "高斯模糊…", run: () => window.ran.push("gaussian") },
        { label: "运动模糊…", run: () => window.ran.push("motion") },
      ] },
      { label: "视图", items: [
        { label: "显示网格", hint: "Ctrl+'", checked: true, run: () => window.ran.push("grid") },
        { label: "对齐", run: () => window.ran.push("snap") },
        // 重名命令：原版 View 菜单里就有两个 Snap，id 靠出现序号区分。
        { label: "对齐", run: () => window.ran.push("snap2") },
      ] },
      { label: "帮助", items: [{ label: "检查更新…", run: () => window.ran.push("update") }] },
    ]);
    window.loadDocument(true);
    currentSelection.set(null);
    // 打开前先把焦点放在别处：关闭时焦点要还回来。
    document.getElementById("before").focus();
    session.openCommandPalette();
    mount(Palette, { target: document.getElementById("palette") });
    // 真正的窗口级分发器（票 47）：Ctrl+F 与画布池的抑制规则要按真实链路验证。
    installAppShortcuts();
    await tick();
  })()`);
}, 30000);

it("lists every menu command by its path, with shortcut and checkmark, and skips the palette's own entry", async () => {
  const titles = await page.locator(".row .title").allTextContents();
  expect(titles).toContain("编辑 › 撤销");
  expect(titles).toContain("滤镜 › 高斯模糊…");
  expect(titles).toContain("视图 › 显示网格");
  expect(titles).not.toContain("编辑 › 搜索命令…");
  // 帮助是本应用的菜单（不是 macOS 的系统 Help），照列。
  expect(titles).toContain("帮助 › 检查更新…");
  // 工具与图层蒙版命令也在（未打开文档时的禁用另测）。
  expect(titles).toContain("工具 › 移动 / 变换");
  expect(titles).toContain("图层 › 添加图层蒙版");
  // 重名命令：两条都列出，各自的 id 靠出现序号区分。
  expect(titles.filter((title) => title === "视图 › 对齐")).toHaveLength(2);
  expect(await page.locator('[data-entry-id="视图 › 对齐 (2)"]').count()).toBe(1);

  const undo = page.locator(".row", { hasText: "编辑 › 撤销" });
  expect(await undo.locator(".shortcut").textContent()).toBe("Ctrl+Z");
  expect(await page.locator(".row", { hasText: "视图 › 显示网格" }).locator(".check").textContent()).toBe("✓");
});

it("filters as you type and says so when nothing matches", async () => {
  await page.locator(".search").fill("高斯");
  await poll(() => page.locator(".row").count()).toBe(1);
  expect(await page.locator(".row .title").textContent()).toBe("滤镜 › 高斯模糊…");

  await page.locator(".search").fill("zzzz");
  await poll(() => page.locator(".row").count()).toBe(0);
  expect(await page.locator(".empty").textContent()).toContain("没有匹配的命令");
});

it("moves the choice with the arrow keys, wrapping at either end", async () => {
  await page.locator(".search").fill("滤镜");
  await poll(() => page.locator(".row").count()).toBe(2);
  expect(await page.locator(".row.chosen .title").textContent()).toBe("滤镜 › 高斯模糊…");

  await page.keyboard.press("ArrowDown");
  await poll(() => page.locator(".row.chosen .title").textContent()).toBe("滤镜 › 运动模糊…");
  await page.keyboard.press("ArrowDown");
  await poll(() => page.locator(".row.chosen .title").textContent()).toBe("滤镜 › 高斯模糊…");
  await page.keyboard.press("ArrowUp");
  await poll(() => page.locator(".row.chosen .title").textContent()).toBe("滤镜 › 运动模糊…");
});

it("runs the chosen command on Enter and closes the palette", async () => {
  await page.locator(".search").fill("高斯");
  await poll(() => page.locator(".row").count()).toBe(1);
  await page.keyboard.press("Enter");
  await poll(() => page.evaluate(() => window.ran)).toEqual(["gaussian"]);
  expect(await page.evaluate(() => window.openState())).toBe(false);
});

it("handles the keys from a focused row too, and Escape still gives the focus back", async () => {
  await page.locator(".search").fill("滤镜");
  await poll(() => page.locator(".row").count()).toBe(2);
  await page.locator(".row").first().focus();
  await page.keyboard.press("ArrowDown");
  await poll(() => page.locator(".row.chosen .title").textContent()).toBe("滤镜 › 运动模糊…");
  await page.keyboard.press("Escape");
  await poll(() => page.evaluate(() => window.openState())).toBe(false);
  expect(await page.evaluate(() => document.activeElement?.id)).toBe("before");
});

it("closes on Escape and gives the focus back to what had it before", async () => {
  await page.locator(".search").focus();
  await page.keyboard.press("Escape");
  await poll(() => page.evaluate(() => document.activeElement?.id)).toBe("before");
});

it("closes on Ctrl+F from a focused row without the window dispatcher reopening it", async () => {
  await page.locator(".search").fill("滤镜");
  await page.locator(".row").first().focus();
  await page.keyboard.press("Control+f");
  await poll(() => page.evaluate(() => window.openState())).toBe(false);
  // 回归症状是"面板没关、查询被清空"：查询词不该被重开的流程清掉。
  const query = await page.evaluate(() => {
    let value;
    const stop = window.session.paletteQuery.subscribe((next) => { value = next; });
    stop();
    return value;
  });
  expect(query).toBe("滤镜");
});

it("suppresses the canvas tool keys while the palette is open", async () => {
  // 焦点落在面板本身（tabindex=-1 的 .palette）：目标既不是输入也不是按钮，
  // 只有注册了画布模态守卫，普通字母才不会穿透去切工具。
  await page.locator(".palette").focus();
  expect(await page.evaluate(() => document.activeElement?.className)).toContain("palette");
  await page.keyboard.press("b");
  expect(await page.evaluate(() => window.toolState())).toBe("move");
  expect(await page.evaluate(() => window.openState())).toBe(true);
});

it("keeps a disabled command listed but unrunnable, and Enter on it does not close", async () => {
  await page.locator(".search").fill("重做");
  await poll(() => page.locator(".row").count()).toBe(1);
  expect(await page.locator(".row").isDisabled()).toBe(true);
  await page.keyboard.press("Enter");
  expect(await page.evaluate(() => window.ran)).toEqual([]);
  expect(await page.evaluate(() => window.openState())).toBe(true);
});

it("the menu bar publishes the same menu the palette searches, with the live Ctrl+F hint", async () => {
  await page.evaluate(`(async () => {
    const { mount, tick } = await import("/node_modules/.vite-palette-browser/deps/svelte.js");
    const { default: MenuBar } = await import("/src/lib/components/MenuBar.svelte");
    const { menuModel } = await import("/src/lib/commands/menuModel.ts");
    window.menuModel = menuModel;
    const host = document.createElement("div");
    document.body.appendChild(host);
    mount(MenuBar, { target: host });
    await tick();
  })()`);
  const published = await page.evaluate(() => {
    let value = [];
    const stop = window.menuModel.subscribe((next) => { value = next; });
    stop();
    const view = value.find((menu) => menu.label === "视图");
    const search = view && view.items.find((item) => item.label === "搜索命令…");
    return { menus: value.map((menu) => menu.label), hint: search ? search.hint : null };
  });
  expect(published.menus).toEqual(["选择", "文件", "编辑", "图像", "图层", "帮助", "视图", "滤镜"]);
  expect(published.hint).toBe("Ctrl+F");
  // 面板搜到的就是这份菜单：菜单栏发布之后，路径与提示都一致。
  await poll(() => page.locator('[data-entry-id="视图 › 搜索命令…"]').count()).toBe(0);
  expect(await page.locator('[data-entry-id="帮助 › 检查更新…"]').count()).toBe(1);
  expect(await page.locator('[data-entry-id="视图 › 适应窗口"] .shortcut').textContent()).toBe("Ctrl+0");
});

it("disables tools and mask commands while no document is open, keeping menu commands", async () => {
  await page.evaluate(() => window.loadDocument(false));
  // 用 data-entry-id 精确匹配：「添加图层蒙版」也是「添加图层蒙版（隐藏全部）」的前缀。
  await poll(() => page.locator('[data-entry-id="工具 › 移动 / 变换"]').isDisabled()).toBe(true);
  expect(await page.locator('[data-entry-id="图层 › 添加图层蒙版"]').isDisabled()).toBe(true);
  expect(await page.locator('[data-entry-id="编辑 › 撤销"]').isDisabled()).toBe(false);
});
