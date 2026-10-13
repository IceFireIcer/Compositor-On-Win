<script lang="ts">
  import { SelectTab } from "../../../wailsjs/go/bridge/Workspace";
  import { applySnapshot, closeTab, transferLayer, workspace, type DocTab } from "../state/workspace";
  import { pendingClose, requestClose, resolveClose } from "../state/confirm";
  import { LAYER_DRAG_MIME } from "../state/dropimport";
  import ConfirmDialog from "./ConfirmDialog.svelte";

  let { onnew }: { onnew: () => void } = $props();

  // 图层行拖到某个标签上 = 把该图层（连同子树）复制进那个项目（票 48；
  // 原版是拖到标签上，ProjectWorkspace.copyLayer(_:into:at:)）。
  let dragOver = $state<string | null>(null);

  function onTabDragOver(event: DragEvent, id: string): void {
    if (!event.dataTransfer?.types.includes(LAYER_DRAG_MIME)) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
    dragOver = id;
  }

  function onTabDrop(event: DragEvent, id: string): void {
    const layerID = event.dataTransfer?.getData(LAYER_DRAG_MIME) ?? "";
    dragOver = null;
    if (layerID === "") return;
    event.preventDefault();
    if (id === $workspace.activeId) return; // 同一项目里拖 = 图层面板自己的排序
    void transferLayer(layerID, id).catch((err) => console.warn("图层跨标签拖拽失败", err));
  }

  function choose(id: string): void {
    void SelectTab(id).then(applySnapshot);
  }

  async function doClose(doc: DocTab, discard = false): Promise<void> {
    // workspace.closeTab clears the document state when the last tab goes.
    await closeTab(doc.id, discard);
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
      class:dragover={dragOver === doc.id}
      onclick={() => choose(doc.id)}
      onkeydown={(e) => e.key === "Enter" && choose(doc.id)}
      ondragover={(e) => onTabDragOver(e, doc.id)}
      ondragleave={() => (dragOver = null)}
      ondrop={(e) => onTabDrop(e, doc.id)}
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
    gap: 3px;
    min-width: 0;
    overflow-x: auto;
    padding: 3px 0;
  }

  .tab {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 28px;
    padding: 2px 6px 2px 11px;
    border: 1px solid transparent;
    border-radius: 6px;
    background: transparent;
    color: var(--text-dim);
    cursor: default;
    white-space: nowrap;
  }

  .tab.active {
    border-color: var(--border);
    background: var(--bg-raised);
    color: var(--text-strong);
  }

  .tab:hover:not(.active) {
    background: var(--bg-raised);
    color: var(--text);
  }

  .tab:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  /* 图层拖到标签上时的落点提示。 */
  .tab.dragover {
    outline: 1px dashed var(--accent);
    outline-offset: -2px;
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
    background: var(--warning);
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
    width: 28px;
    height: 28px;
    padding: 0;
    border-color: transparent;
    background: transparent;
    color: var(--text-dim);
  }
</style>
