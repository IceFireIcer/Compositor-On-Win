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
  } from "./lib/state/workspace";
  import TabStrip from "./lib/components/TabStrip.svelte";
  import NewCanvasSheet from "./lib/components/NewCanvasSheet.svelte";
  import CanvasSurface from "./lib/components/CanvasSurface.svelte";

  let version = $state("…");
  let sheetOpen = $state(false);

  const current = $derived(activeTab($workspace));

  // Welcome screen whenever the workspace empties (first launch or last tab closed).
  $effect(() => {
    if (!hasDocument($workspace)) sheetOpen = true;
  });

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
</script>

<div class="app">
  <header class="toolbar">
    <div class="brand">Compositor</div>
    <TabStrip onnew={newCanvas} />
    <div class="spacer"></div>
    <div class="zoom-controls">
      <button title="缩小（票 13 实装）">−</button>
      <span class="zoom-value">100%</span>
      <button title="放大（票 13 实装）">＋</button>
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
    </nav>

    <main class="canvas-area">
      {#if current}
        <CanvasSurface doc={current} />
      {:else}
        <div class="welcome">
          <p class="welcome-title">Compositor for Windows</p>
          <p class="hint">新建画布开始创作，或打开 .comp 项目（票 05）</p>
          <button class="welcome-new" onclick={newCanvas}>新建画布…</button>
        </div>
      {/if}
    </main>

    <aside class="layers-panel">
      <div class="panel-title">图层</div>
      {#if current}
        <div class="panel-empty">「{current.name}」尚无图层内容（票 04 接域模型）</div>
      {:else}
        <div class="panel-empty">没有打开的文档</div>
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

  .zoom-value {
    min-width: 44px;
    text-align: center;
    color: var(--text-dim);
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

  .layers-panel {
    padding: 8px;
    background: var(--bg-panel);
    border-left: 1px solid var(--border);
    overflow-y: auto;
  }

  .panel-title {
    font-weight: 600;
    margin-bottom: 6px;
  }

  .panel-empty {
    color: var(--text-dim);
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
