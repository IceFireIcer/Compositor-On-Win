<script lang="ts">
  import { onMount } from "svelte";
  import { TOOLS, activeTool, selectTool } from "./lib/state/tools";
  import { Version } from "../wailsjs/go/bridge/Service";

  let version = $state("…");

  onMount(async () => {
    try {
      version = await Version();
    } catch {
      version = "离线";
    }
  });
</script>

<div class="app">
  <header class="toolbar">
    <div class="brand">Compositor</div>
    <div class="tabs">
      <span class="tab active">未命名</span>
      <button class="tab-add" title="新建画布（票 02）">＋</button>
    </div>
    <div class="spacer"></div>
    <div class="zoom-controls">
      <button title="缩小">−</button>
      <span class="zoom-value">100%</span>
      <button title="放大">＋</button>
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
      <div class="welcome">
        <p class="welcome-title">Compositor for Windows</p>
        <p class="hint">脚手架就绪 — 空画布区域（票 02 接新建画布）</p>
      </div>
    </main>

    <aside class="layers-panel">
      <div class="panel-title">图层</div>
      <div class="panel-empty">没有打开的文档</div>
    </aside>
  </div>

  <footer class="status-bar">
    <span>就绪</span>
    <span class="spacer"></span>
    <span title="internal/bridge.Service.Version 往返">v{version}</span>
  </footer>
</div>

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

  .tabs {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .tab {
    padding: 3px 12px;
    border-radius: 4px;
    background: var(--bg-raised);
  }

  .tab.active {
    background: var(--accent-dim);
  }

  .tab-add {
    width: 24px;
    height: 24px;
    padding: 0;
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
  }

  .tool {
    width: 34px;
    height: 34px;
    padding: 0;
  }

  .canvas-area {
    position: relative;
    background: var(--bg-canvas);
  }

  .welcome {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 4px;
  }

  .welcome-title {
    font-size: 18px;
    font-weight: 600;
  }

  .hint {
    color: var(--text-dim);
  }

  .layers-panel {
    padding: 8px;
    background: var(--bg-panel);
    border-left: 1px solid var(--border);
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
</style>
