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
    openProject,
    saveProject,
  } from "./lib/state/workspace";
  import { clearDocument, loadDocument } from "./lib/state/document";
  import { keyboardZoom, viewport, zoomToValue } from "./lib/state/viewport";
  import TabStrip from "./lib/components/TabStrip.svelte";
  import NewCanvasSheet from "./lib/components/NewCanvasSheet.svelte";
  import CanvasSurface from "./lib/components/CanvasSurface.svelte";
  import LayersPanel from "./lib/components/LayersPanel.svelte";
  import MenuBar from "./lib/components/MenuBar.svelte";
  import FilterDialog from "./lib/components/FilterDialog.svelte";
  import CameraRawPanel from "./lib/components/CameraRawPanel.svelte";
  import { watchFilterPreviews, beginFilter, filterSession } from "./lib/state/filters";
  import { healSettings, updateHealSettings } from "./lib/state/tools";
  import {
    copyMerged,
    exportDialog,
    exportNotice,
    exportPNG,
    openExportJPEG,
  } from "./lib/state/export";
  import ExportDialog from "./lib/components/ExportDialog.svelte";
  import RawDevelopSheet from "./lib/components/RawDevelopSheet.svelte";
  import { rawDevelop } from "./lib/state/rawdevelop";

  let version = $state("…");
  let sheetOpen = $state(false);

  const current = $derived(activeTab($workspace));
  // The zoom readout subscribes to the viewport store (device px per doc px).
  const zoomPct = $derived(Math.round($viewport.zoom * 100));

  // Welcome screen whenever the workspace empties (first launch or last tab closed).
  $effect(() => {
    if (!hasDocument($workspace)) sheetOpen = true;
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

  // PSD/PSB 转换报告：信息性提示，不阻断打开。
  $effect(() => {
    const wails = window as unknown as {
      runtime?: { EventsOn: (name: string, fn: (...args: unknown[]) => void) => () => void };
    };
    const runtimeApi = wails.runtime;
    if (!runtimeApi) return;
    const off = runtimeApi.EventsOn("psdConversions", (payload: unknown) => {
      try {
        const notes = JSON.parse(String(payload)) as { layerName: string; message: string }[];
        for (const note of notes) {
          console.info(`PSD 导入 · ${note.layerName}: ${note.message}`);
        }
        if (notes.length > 0) {
          console.info(`PSD 导入：共 ${notes.length} 条转换说明。`);
        }
      } catch {
        // non-fatal
      }
    });
    return off;
  });

  // Image-menu shortcuts: ⌘M 曲线 / ⌘L 色阶 / ⌘U 色相饱和度 / ⌘I 反相.
  function onKeydown(event: KeyboardEvent): void {
    if (!(event.metaKey || event.ctrlKey) || $filterSession) return;
    // File-export shortcuts (ticket 42): ⇧⌘E 导出 PNG / ⇧⌥⌘S 导出 JPEG /
    // ⇧⌘C 拷贝合并 — the original's C06/C07/C12 bindings.
    if (event.shiftKey) {
      const file = event.key.toLowerCase();
      if (file === "e" && !event.altKey) {
        event.preventDefault();
        void exportPNG().catch((err) => console.warn("导出 PNG 失败", err));
        return;
      }
      if (file === "s" && event.altKey) {
        event.preventDefault();
        void openExportJPEG();
        return;
      }
      if (file === "c" && !event.altKey) {
        event.preventDefault();
        void copyMerged().catch((err) => console.warn("拷贝合并失败", err));
        return;
      }
    }
    const key = event.key.toLowerCase();
    if (key !== "m" && key !== "l" && key !== "u" && key !== "i") return;
    const doc = current;
    if (!doc) return;
    event.preventDefault();
    const map: Record<string, { kind: string; label: string }> = {
      m: { kind: "adjust:Curves", label: "曲线" },
      l: { kind: "adjust:Levels", label: "色阶" },
      u: { kind: "adjust:Hue/Saturation", label: "色相/饱和度" },
    };
    if (key === "i") {
      void (async () => {
        const { ApplyFilter } = await import("../wailsjs/go/bridge/Service");
        const { applyDocumentSnapshot } = await import("./lib/state/document");
        if (!doc.id) return;
        try {
          applyDocumentSnapshot(
            await ApplyFilter("invert", "", "{}", 0, ""),
          );
        } catch (err) {
          console.warn("反相失败", err);
        }
      })();
      return;
    }
    const target = map[key];
    if (target) void beginFilter(target.kind, target.label, true);
  }

  onMount(() => {
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
      clearTimeout(timer);
      window.removeEventListener("resize", onResize);
    };
  });

  function newCanvas(): void {
    sheetOpen = true;
  }

  // Bridge errors (plain `vite dev` has no window.go) stay non-fatal.
  function openProjectSafe(): void {
    void openProject().catch((err) => console.warn("打开项目失败", err));
  }

  function saveProjectSafe(): void {
    void saveProject().catch((err) => console.warn("保存项目失败", err));
  }
</script>

<svelte:window onkeydown={onKeydown} onmousemove={movePanelDrag} onmouseup={endPanelDrag} />

<div class="app">
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
  open={sheetOpen}
  onclose={() => {
    sheetOpen = false;
  }}
/>

{#if $filterSession && $filterSession.kind !== "cameraRaw"}
  <FilterDialog />
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
