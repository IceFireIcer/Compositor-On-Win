<script lang="ts">
  import { onMount } from "svelte";
  import { Version } from "../wailsjs/go/bridge/Service";
  import { Snapshot as GetSnapshot } from "../wailsjs/go/bridge/Workspace";
  import { Save as SaveWindowState } from "../wailsjs/go/bridge/WindowStore";
  import { TOOLS, activeTool, selectTool } from "./lib/state/tools";
  import {
    workspace,
    applySnapshot,
    activeTab,
    hasDocument,
    type PendingConversion,
  } from "./lib/state/workspace";
  import PsdConversionSheet from "./lib/components/PsdConversionSheet.svelte";
  import { clearDocument, loadDocument } from "./lib/state/document";
  import { keyboardZoom, viewport, zoomToValue } from "./lib/state/viewport";
  import TabStrip from "./lib/components/TabStrip.svelte";
  import NewCanvasSheet from "./lib/components/NewCanvasSheet.svelte";
  import CanvasSurface from "./lib/components/CanvasSurface.svelte";
  import LayersPanel from "./lib/components/LayersPanel.svelte";
  import MenuBar from "./lib/components/MenuBar.svelte";
  import FilterDialog from "./lib/components/FilterDialog.svelte";
  import CameraRawPanel from "./lib/components/CameraRawPanel.svelte";
  import { watchFilterPreviews, filterSession } from "./lib/state/filters";
  import { healSettings, updateHealSettings } from "./lib/state/tools";
  import { exportDialog, exportNotice } from "./lib/state/export";
  import ExportDialog from "./lib/components/ExportDialog.svelte";
  import GeometrySheet from "./lib/components/GeometrySheet.svelte";
  import TextOptionsBar from "./lib/components/TextOptionsBar.svelte";
  import { geometrySheet } from "./lib/state/geometry";
  import RawDevelopSheet from "./lib/components/RawDevelopSheet.svelte";
  import { rawDevelop } from "./lib/state/rawdevelop";
  import KeyboardShortcutsSheet from "./lib/components/KeyboardShortcutsSheet.svelte";
  import {
    installAppShortcuts,
    newCanvasSheet,
    openProjectSafe,
    pendingConversions,
    saveProjectSafe,
  } from "./lib/shortcuts/actions";
  import { shortcutsSheet } from "./lib/shortcuts/settings";
  import {
    openShellPaths,
    refreshRecentProjects,
    syncWindowChrome,
    watchShellPaths,
  } from "./lib/state/shell";
  import { OnFileDrop } from "../wailsjs/runtime/runtime";
  import { importDroppedBlobs, importDroppedPaths } from "./lib/state/workspace";
  import {
    DROP_SETTLE_MS,
    LAYER_DRAG_MIME,
    browserImageFiles,
    browserRasterizer,
    prepareDroppedBlobs,
    readDropPayload,
    shouldImportDomDrop,
  } from "./lib/state/dropimport";

  let version = $state("…");

  // --- 拖放导入（票 48）-------------------------------------------------------
  // Wails 的窗口级回调拿到的是**磁盘路径**（资源管理器、截图工具、其它应用
  // 拖出的临时文件都算），覆盖 TIFF/PSD/HEIC/RAW 这些浏览器解不了的格式；
  // DOM 的 drop 事件拿到的是**字节**（截图位图、网页图），由浏览器栅格化后
  // 走上传面。两者可能同时收到同一次拖放，DOM 侧用去重窗口避开。
  let wailsPathsAt: number | null = null;

  async function reportImportFailures(failures: string[]): Promise<void> {
    if (failures.length === 0) return;
    window.alert(`\u5bfc\u5165\u672a\u80fd\u5b8c\u6210\n\n${failures.join("\n\n")}`);
  }

  // --- Shell 集成（票 49）----------------------------------------------------
  // 外壳递来的文件（双击 .comp、命令行、第二次启动的单实例转发）由 Go 排队，
  // 前端就绪后调 OpenShellPaths 统一打开：.comp 进标签，.psd 顶起转换报告。
  async function handleShellPaths(): Promise<void> {
    const result = await openShellPaths();
    if (result.pending) pendingConversions.set(result.conversions);
    if (result.failed.length > 0) {
      window.alert(
        `打开失败\n\n${result.failed.map((f) => `${f.path}: ${f.error}`).join("\n\n")}`,
      );
    }
    if (result.opened.length > 0) await refreshRecentProjects();
  }

  // 窗口标题与任务栏脏标记跟着活动标签走（原版：标题跟文档名，isDocumentEdited
  // 画未保存标记）。只在名字/脏标记真的变了时才过桥。
  let lastChromeKey = "";
  $effect(() => {
    const tab = current;
    const key = `${tab?.name ?? ""}|${tab?.dirty ? 1 : 0}`;
    if (key === lastChromeKey) return;
    lastChromeKey = key;
    syncWindowChrome(tab?.name ?? "", tab?.dirty ?? false);
  });

  async function onDroppedPaths(paths: string[]): Promise<void> {
    if (paths.length === 0) return;
    wailsPathsAt = Date.now();
    try {
      const result = await importDroppedPaths(paths);
      await reportImportFailures(result.failures);
    } catch (err) {
      console.warn("拖放导入失败", err);
      await reportImportFailures([err instanceof Error ? err.message : String(err)]);
    }
  }

  function onDragOver(event: DragEvent): void {
    // HTML5 规定：只有 dragover 里 preventDefault 过，drop 才会触发。所以文件
    // （Files）与网页图（text/uri-list、text/html）两种载荷都要放开默认行为 ——
    // 网页图那次拖拽的 types 里没有 Files，漏掉它这条来源就永远收不到 drop。
    const types = event.dataTransfer?.types;
    if (!types) return;
    if (types.includes("Files") || types.includes("text/uri-list") || types.includes("text/html")) {
      event.preventDefault();
    }
  }

  async function onDrop(event: DragEvent): Promise<void> {
    const dt = event.dataTransfer;
    if (!dt) return;
    if (dt.types.includes(LAYER_DRAG_MIME)) return; // 图层跨标签拖拽
    if (!shouldImportDomDrop(wailsPathsAt, Date.now())) return; // Wails 已按路径导入
    event.preventDefault();
    // 等 Wails 的窗口级回调落地再判一次：两条路谁先到不确定，路径先到时
    // DOM 这边必须让位，否则同一次拖放会导入两遍。
    await new Promise((resolve) => setTimeout(resolve, DROP_SETTLE_MS));
    if (!shouldImportDomDrop(wailsPathsAt, Date.now())) return;
    const payload = readDropPayload(dt);
    const files = browserImageFiles(payload.files);
    const failures: string[] = [];
    if (files.length > 0) {
      const { ready, failed } = await prepareDroppedBlobs(files, browserRasterizer);
      failures.push(...failed.map((name) => `${name}: \u6d4f\u89c8\u5668\u65e0\u6cd5\u89e3\u7801\u8fd9\u5f20\u56fe\u50cf`));
      if (ready.length > 0) {
        const result = await importDroppedBlobs(ready);
        failures.push(...result.failures);
      }
    }
    // 网页图（text/uri-list）：取回字节后走同一条上传路。跨域取不到就报出来。
    for (const uri of payload.uris) {
      try {
        const response = await fetch(uri);
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        const blob = await response.blob();
        const png = await browserRasterizer(blob);
        if (!png) throw new Error("无法解码");
        const name = decodeURIComponent(uri.split("/").pop() ?? "image") || "image";
        const result = await importDroppedBlobs([{ name, blob: png }]);
        failures.push(...result.failures);
      } catch (err) {
        failures.push(`${uri}: ${err instanceof Error ? err.message : String(err)}`);
      }
    }
    await reportImportFailures(failures);
  }

  const current = $derived(activeTab($workspace));
  // The zoom readout subscribes to the viewport store (device px per doc px).
  const zoomPct = $derived(Math.round($viewport.zoom * 100));

  // Welcome screen whenever the workspace empties (first launch or last tab closed).
  $effect(() => {
    if (!hasDocument($workspace)) newCanvasSheet.set(true);
  });

  // Keep the document store (layers + render rev) in sync with the active
  // tab: load on tab switch / new document / open project, clear when the
  // last tab closes. Redundant loads dedupe on the store's rev cache.
  $effect(() => {
    const id = current?.id;
    if (id) void loadDocument(id);
    else clearDocument();
  });

  // Camera Raw panel docking: width is resizable and remembered across
  // restarts (ticket 34's 尺寸调整/关闭记忆).
  const PANEL_KEY = "cameraRawPanelWidth";
  let panelWidth = $state(loadPanelWidth());
  let panelDrag = $state(false);

  function loadPanelWidth(): number {
    const raw = Number(localStorage.getItem(PANEL_KEY));
    return raw >= 320 && raw <= 640 ? raw : 440;
  }

  function startPanelDrag(e: MouseEvent): void {
    e.preventDefault();
    panelDrag = true;
  }

  function movePanelDrag(e: MouseEvent): void {
    if (!panelDrag) return;
    panelWidth = Math.max(320, Math.min(640, window.innerWidth - e.clientX));
  }

  function endPanelDrag(): void {
    if (!panelDrag) return;
    panelDrag = false;
    localStorage.setItem(PANEL_KEY, String(panelWidth));
  }

  const cameraRawOpen = $derived($filterSession?.kind === "cameraRaw");

  // Filter preview pushes (filterPreview:{tabID}) keep document.filterRev —
  // and therefore the canvas URL — fresh while a dialog is open.
  $effect(() => {
    const stop = watchFilterPreviews();
    return stop;
  });

  // The three-domain shortcut dispatcher (ticket 47): 菜单 / 画布 / 文字 分流，
  // 输入框里不抢画布快捷键（BlendShortcutTests 语义）。动作表在
  // lib/shortcuts/actions.ts —— App 这里只负责装上/拆掉。
  onMount(() => {
    const teardownShortcuts = installAppShortcuts();
    // 窗口级文件拖放：Wails 给的是路径（无路径的内存图像走 DOM 的 drop）。
    OnFileDrop((_x, _y, paths) => {
      void onDroppedPaths(paths);
    }, false);
    // 第二次启动把参数交给既有实例后，Go 会推这个事件（票 49 的单实例协议）。
    const stopShellPaths = watchShellPaths(() => {
      void handleShellPaths();
    });
    // 启动参数里带的文件（双击关联打开）。
    void handleShellPaths();
    void refreshRecentProjects();

    void (async () => {
      try {
        version = await Version();
      } catch {
        version = "离线";
      }
      applySnapshot(await GetSnapshot());
    })();

    // Window size persistence: debounced save on resize; restore happens
    // Go-side at startup (main.go OnStartup). outerWidth/outerHeight match
    // the unit of runtime.WindowSetSize — saving the viewport would shrink
    // the window by the titlebar on every relaunch (review I2).
    let timer: number | undefined;
    const onResize = () => {
      clearTimeout(timer);
      timer = window.setTimeout(() => {
        void SaveWindowState(window.outerWidth, window.outerHeight);
      }, 400);
    };
    window.addEventListener("resize", onResize);
    return () => {
      teardownShortcuts();
      stopShellPaths();
      clearTimeout(timer);
      window.removeEventListener("resize", onResize);
    };
  });

  function newCanvas(): void {
    newCanvasSheet.set(true);
  }

  // PSD 转换报告（票 39）：报告完再应用。
  const psdReport = $derived<PendingConversion[] | null>($pendingConversions);
</script>

<svelte:window onmousemove={movePanelDrag} onmouseup={endPanelDrag} />

<!-- 拖放处理器挂在应用根上；这里不需要 ARIA 角色（role="application" 会关掉读屏的
     虚拟光标导航），所以显式忽略静态元素交互的告警。 -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="app" ondragover={onDragOver} ondrop={onDrop}>
  <header class="toolbar">
    <div class="brand">Compositor</div>
    <MenuBar />
    <TabStrip onnew={newCanvas} />
    <div class="spacer"></div>
    <div class="file-actions">
      <button title="打开 .comp 项目" onclick={openProjectSafe}>打开</button>
      <button title="保存项目" onclick={saveProjectSafe}>保存</button>
    </div>
    <div class="zoom-controls">
      <button
        title="缩小"
        disabled={!current}
        onclick={() => current && keyboardZoom(-1, current.width, current.height)}
      >
        −
      </button>
      <button
        class="zoom-value"
        title="实际像素：点击回到 100%"
        disabled={!current}
        onclick={() => current && zoomToValue(1, current.width, current.height)}
      >
        {zoomPct}%
      </button>
      <button
        title="放大"
        disabled={!current}
        onclick={() => current && keyboardZoom(1, current.width, current.height)}
      >
        ＋
      </button>
    </div>
  </header>

  {#if $activeTool === "type"}
    <TextOptionsBar />
  {/if}

  <div class="body">
    <nav class="tool-rail" aria-label="工具">
      {#each TOOLS as tool (tool.id)}
        <button
          class="tool"
          class:active={$activeTool === tool.id}
          title="{tool.label} ({tool.shortcut})"
          onclick={() => selectTool(tool.id)}
        >
          {tool.shortcut}
        </button>
      {/each}
      {#if $activeTool === "spotHealing"}
        <div class="heal-options">
          <select
            aria-label="修复模式"
            title="修复模式"
            value={$healSettings.mode}
            onchange={(e) => updateHealSettings({ mode: +e.currentTarget.value })}
          >
            <option value={0}>内容感知</option>
            <option value={1}>创建纹理</option>
            <option value={2}>临近匹配</option>
          </select>
          <label title="画笔直径">
            <input
              type="range"
              min="4"
              max="200"
              value={$healSettings.diameter}
              onchange={(e) => updateHealSettings({ diameter: +e.currentTarget.value })}
            />
          </label>
          <label title="不透明度">
            <input
              type="range"
              min="1"
              max="100"
              value={Math.round($healSettings.opacity * 100)}
              onchange={(e) => updateHealSettings({ opacity: +e.currentTarget.value / 100 })}
            />
          </label>
        </div>
      {/if}
    </nav>

    <main class="canvas-area">
      {#if current}
        <CanvasSurface doc={current} />
      {:else}
        <div class="welcome">
          <p class="welcome-title">Compositor for Windows</p>
          <p class="hint">新建画布开始创作，或打开 .comp 项目</p>
          <button class="welcome-new" onclick={newCanvas}>新建画布…</button>
        </div>
      {/if}
    </main>

    <aside
      class="right-panel"
      class:docking={panelDrag}
      style:width={cameraRawOpen ? `${panelWidth}px` : "260px"}
    >
      {#if cameraRawOpen}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="panel-resize" onmousedown={startPanelDrag}></div>
        <CameraRawPanel />
      {:else}
        <LayersPanel />
      {/if}
    </aside>
  </div>

  <footer class="status-bar">
    <span>{current ? "就绪" : "欢迎"}</span>
    {#if current}
      <span class="sep">·</span>
      <span>{current.name} — {current.width} × {current.height} @ {current.resolution}ppi</span>
      {#if current.dirty}
        <span class="sep">·</span>
        <span class="unsaved">未保存</span>
      {/if}
    {/if}
    <span class="spacer"></span>
    <span title="internal/bridge.Service.Version 往返">v{version}</span>
  </footer>
</div>

<NewCanvasSheet
  open={$newCanvasSheet}
  onclose={() => {
    newCanvasSheet.set(false);
  }}
/>

{#if $shortcutsSheet}
  <KeyboardShortcutsSheet />
{/if}

{#if $filterSession && $filterSession.kind !== "cameraRaw"}
  <FilterDialog />
{/if}

{#if $geometrySheet}
  <GeometrySheet />
{/if}

{#if psdReport}
  <PsdConversionSheet conversions={psdReport} onclose={() => pendingConversions.set(null)} />
{/if}

{#if $rawDevelop}
  <RawDevelopSheet />
{/if}

{#if $exportDialog}
  <ExportDialog
    onClose={() => {
      exportDialog.set(null);
    }}
  />
{/if}

{#if $exportNotice}
  <div class="export-toast" role="status">{$exportNotice}</div>
{/if}

<style>
  .app {
    display: grid;
    grid-template-rows: auto 1fr auto;
    height: 100%;
  }

  .toolbar {
    display: flex;
    align-items: center;
    gap: 12px;
    height: 40px;
    padding: 0 12px;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
  }

  .brand {
    font-weight: 600;
  }

  .export-toast {
    position: fixed;
    left: 50%;
    bottom: 44px;
    transform: translateX(-50%);
    padding: 8px 16px;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.35);
    font-size: 13px;
    z-index: 30;
    max-width: 70vw;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .zoom-controls {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .zoom-controls button {
    width: 24px;
    height: 24px;
    padding: 0;
  }

  .zoom-controls button:disabled {
    opacity: 0.45;
    cursor: default;
  }

  /* The readout is a button (click = 100%) styled as plain text. */
  .zoom-controls .zoom-value {
    width: auto;
    min-width: 48px;
    padding: 0 4px;
    background: transparent;
    border: none;
    color: var(--text-dim);
    text-align: center;
  }

  .zoom-value:hover {
    background: transparent;
    color: var(--text);
  }

  .file-actions {
    display: flex;
    gap: 6px;
  }

  .file-actions button {
    height: 24px;
    padding: 0 10px;
  }

  .body {
    display: grid;
    grid-template-columns: auto 1fr 260px;
    min-height: 0;
  }

  .tool-rail {
    width: 44px;
    padding: 6px 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 2px;
    background: var(--bg-panel);
    border-right: 1px solid var(--border);
    overflow-y: auto;
  }

  .tool {
    width: 34px;
    height: 34px;
    padding: 0;
  }

  .heal-options {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 8px 4px;
    border-top: 1px solid var(--border);
    width: 100%;
  }

  .heal-options select {
    width: 100%;
    font-size: 11px;
  }

  .heal-options input[type="range"] {
    width: 100%;
  }

  .canvas-area {
    position: relative;
    background: var(--bg-canvas);
    min-width: 0;
  }

  .welcome {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
  }

  .welcome-title {
    font-size: 18px;
    font-weight: 600;
  }

  .hint {
    color: var(--text-dim);
  }

  .welcome-new {
    padding: 6px 18px;
    margin-top: 8px;
  }

  .right-panel {
    position: relative;
    background: var(--bg-panel);
    border-left: 1px solid var(--border);
    overflow: hidden;
    display: flex;
    min-width: 0;
  }

  .right-panel.docking {
    user-select: none;
  }

  .panel-resize {
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    width: 5px;
    cursor: ew-resize;
    z-index: 10;
  }

  .right-panel > :global(*) {
    flex: 1;
    min-width: 0;
  }

  .status-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 24px;
    padding: 0 10px;
    background: var(--bg-panel);
    border-top: 1px solid var(--border);
    color: var(--text-dim);
    font-size: 11px;
  }

  .unsaved {
    color: var(--text);
  }

  .sep {
    opacity: 0.5;
  }
</style>
