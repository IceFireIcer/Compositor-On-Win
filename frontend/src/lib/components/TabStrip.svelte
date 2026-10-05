<script lang="ts">
  import { SelectTab, CloseTab } from "../../../wailsjs/go/bridge/Workspace";
  import { applySnapshot, workspace, type DocTab } from "../state/workspace";
  import { pendingClose, requestClose, resolveClose } from "../state/confirm";
  import ConfirmDialog from "./ConfirmDialog.svelte";

  let { onnew }: { onnew: () => void } = $props();

  function choose(id: string): void {
    void SelectTab(id).then(applySnapshot);
  }

  async function doClose(doc: DocTab): Promise<void> {
    applySnapshot(await CloseTab(doc.id));
  }

  // window.confirm is unsupported in Wails/WebView2 (review I1): dirty tabs
  // park in the confirm state machine and render the in-app dialog instead.
  function close(doc: DocTab): void {
    requestClose(doc, doClose);
  }
</script>

<div class="tabs" role="tablist" aria-label="打开的文档">
  {#each $workspace.tabs as doc (doc.id)}
    <div
      class="tab"
      class:active={doc.id === $workspace.activeId}
      role="tab"
      aria-selected={doc.id === $workspace.activeId}
      tabindex="0"
      onclick={() => choose(doc.id)}
      onkeydown={(e) => e.key === "Enter" && choose(doc.id)}
    >
      <span class="tab-name">{doc.name}</span>
      {#if doc.dirty}
        <span class="dot" title="未保存"></span>
      {/if}
      <button
        class="tab-close"
        title="关闭标签"
        onclick={(e) => {
          e.stopPropagation();
          close(doc);
        }}
      >
        ×
      </button>
    </div>
  {/each}
  <button class="tab-add" title="新建画布（Ctrl+N，票 47 接快捷键）" onclick={onnew}>＋</button>
</div>

<ConfirmDialog
  open={$pendingClose !== null}
  title="关闭标签"
  message={$pendingClose ? `“${$pendingClose.name}”有未保存的更改，仍要关闭吗？` : ""}
  confirmLabel="关闭"
  danger
  onresult={(ok) => resolveClose(ok, doClose)}
/>

<style>
  .tabs {
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
    overflow-x: auto;
  }

  .tab {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 3px 8px 3px 12px;
    border-radius: 4px;
    background: var(--bg-raised);
    cursor: default;
    white-space: nowrap;
  }

  .tab.active {
    background: var(--accent-dim);
  }

  .tab-name {
    max-width: 140px;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--text);
  }

  .tab-close {
    width: 18px;
    height: 18px;
    padding: 0;
    font-size: 11px;
    line-height: 1;
    border: none;
    background: transparent;
  }

  .tab-close:hover {
    background: rgba(255, 255, 255, 0.12);
  }

  .tab-add {
    width: 24px;
    height: 24px;
    padding: 0;
  }
</style>
