<script lang="ts">
  import { SelectTab } from "../../../wailsjs/go/bridge/Workspace";
  import { applySnapshot, closeTab, importDroppedPathsAsNewProject, transferLayer, workspace, type DocTab } from "../state/workspace";
  import { pendingClose, requestClose, resolveClose } from "../state/confirm";
  import { DROP_SETTLE_MS, LAYER_DRAG_MIME, shouldImportDomDropNow } from "../state/dropimport";
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

  // 票 74：图片落到标签栏**任意位置**都新开项目（画布按第一张图的尺寸）。图层
  // 拖拽（LAYER_DRAG_MIME）仍由每个标签自己接，所以这里先把它排除 —— 否则跨标签
  // 复制会被当成新建项目。
  let barDragOver = $state(false);

  /** 拖进来的文件在 WebView2 上带磁盘路径；没有路径的（网页图）留给 Wails 那条路。 */
  function barDropPaths(event: DragEvent): string[] {
    const paths: string[] = [];
    for (const file of Array.from(event.dataTransfer?.files ?? [])) {
      const path = "path" in file ? String((file as File & { path?: string }).path ?? "") : "";
      if (path) paths.push(path);
    }
    return paths;
  }

  function onBarDragOver(event: DragEvent): void {
    const types = event.dataTransfer?.types;
    if (!types || types.includes(LAYER_DRAG_MIME) || !types.includes("Files")) return;
    event.preventDefault();
    event.dataTransfer!.dropEffect = "copy";
    barDragOver = true;
  }

  async function onBarDrop(event: DragEvent): Promise<void> {
    barDragOver = false;
    if (event.dataTransfer?.types.includes(LAYER_DRAG_MIME)) return; // 每个标签自己处理
    const paths = barDropPaths(event);
    if (paths.length === 0) return;
    event.preventDefault();
    // 与画布那条路共用去重：桌面端同一次拖放 Wails 的窗口级回调也会到，先让位给
    // 它（它带落点坐标，能认出标签栏）。两条路谁先到不确定，所以等一拍再判。
    if (!shouldImportDomDropNow(Date.now())) return;
    await new Promise((resolve) => setTimeout(resolve, DROP_SETTLE_MS));
    if (!shouldImportDomDropNow(Date.now())) return;
    await importDroppedPathsAsNewProject(paths).catch((err) => console.warn("拖到标签栏失败", err));
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

<div
  class="tab-bar"
  class:bar-dragover={barDragOver}
  role="group"
  aria-label="文档标签栏"
  ondragover={onBarDragOver}
  ondragleave={() => (barDragOver = false)}
  ondrop={onBarDrop}
>
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

  /* 票 74：整条标签栏是「新开项目」的拖放目标。外壳不带 role，拖放处理挂它上面，
     这样 role="tablist" 的内层不会被 a11y 规则要求可聚焦。 */
  .tab-bar.bar-dragover {
    outline: 1px dashed var(--accent, #3d6ea5);
    outline-offset: -2px;
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
