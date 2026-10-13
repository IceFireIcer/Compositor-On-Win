<script lang="ts">
  import { get } from "svelte/store";
  import {
    BLEND_MODES,
    applyDocumentSnapshot,
    document as documentState,
    layerOps,
    type DocLayer,
  } from "../state/document";
  import { beginFilter, filterSession } from "../state/filters";
  import { LAYER_DRAG_MIME } from "../state/dropimport";
  import { currentSelection, selectionPayload } from "../state/selection";
  import {
    isLayerPanelChildEventTarget,
    layerDropIntent,
    layerPanelRows,
    placeLayer,
    type LayerDropIntent,
  } from "../state/layerPanel";
  import { workspace } from "../state/workspace";
  import { canvasEditTarget, clearMaskTarget, selectMaskTarget } from "../state/layerTarget";
  import { requestLayerMask, type LayerMaskOperation } from "../state/layerMask";

  /**
   * The layer panel (ticket 20 shell + bridge wiring). Rows render top→bottom
   * (Photoshop convention) out of the bottom→top domain array; every edit is
   * a Service.LayerOp round-trip whose fresh snapshot updates the store.
   */

  const docState = $derived($documentState);
  const rows = $derived(layerPanelRows(docState.layers));

  // Note: svelte-check (svelte 5.19) rejects the generic rune form here —
  // `$state<string | null>(null)` parses as a legacy store subscription and
  // errors. Annotate the variable and pass the initial value instead.
  let editingId: string | null = $state(null);
  let editName = $state("");
  const maskTargetId = $derived($canvasEditTarget?.targetMask ? $canvasEditTarget.layerID : null);
  let targetRequest = 0;
  let dragSourceId: string | null = $state(null);
  let dropIntent: LayerDropIntent | null = $state(null);
  const hasSelection = $derived(!!$currentSelection?.mask.some((value) => value > 0));

  function findLayer(id: string): DocLayer | undefined {
    return docState.layers.find((l) => l.id === id);
  }

  const selectedMaskLayer = $derived(maskTargetId ? findLayer(maskTargetId) : undefined);

  function startRename(layer: DocLayer): void {
    editingId = layer.id;
    editName = layer.name;
  }

  function commitRename(): void {
    const id = editingId;
    const name = editName.trim();
    editingId = null;
    if (!id) return;
    const layer = findLayer(id);
    if (name && layer && name !== layer.name) void layerOps.rename(id, name);
  }

  function cancelRename(): void {
    editingId = null;
  }

  // Action: focus and pre-select the whole name when the inline editor mounts.
  function focusSelect(node: HTMLInputElement): void {
    node.focus();
    node.select();
  }

  function activateLayer(id: string): void {
    targetRequest++;
    clearMaskTarget(get(workspace).activeId);
    void layerOps.setActive(id);
  }

  function beginLayerDrag(event: DragEvent, id: string): void {
    dragSourceId = id;
    event.dataTransfer?.setData(LAYER_DRAG_MIME, id);
    if (event.dataTransfer) event.dataTransfer.effectAllowed = "copyMove";
  }

  function updateLayerDrop(event: DragEvent, target: DocLayer): void {
    const source = dragSourceId ?? event.dataTransfer?.getData(LAYER_DRAG_MIME) ?? "";
    if (!source || !event.currentTarget || !(event.currentTarget instanceof HTMLElement)) return;
    dropIntent = layerDropIntent(
      docState.layers,
      source,
      target.id,
      event.clientY - event.currentTarget.getBoundingClientRect().top,
      event.currentTarget.clientHeight,
    );
    if (event.dataTransfer) event.dataTransfer.dropEffect = dropIntent ? "move" : "none";
  }

  function finishLayerDrop(event: DragEvent, target: DocLayer): void {
    event.preventDefault();
    const source = dragSourceId ?? event.dataTransfer?.getData(LAYER_DRAG_MIME) ?? "";
    const intent = layerDropIntent(
      docState.layers,
      source,
      target.id,
      event.currentTarget instanceof HTMLElement ? event.clientY - event.currentTarget.getBoundingClientRect().top : 0,
      event.currentTarget instanceof HTMLElement ? event.currentTarget.clientHeight : 0,
    );
    dragSourceId = null;
    dropIntent = null;
    if (!intent) return;
    void placeLayer(get(workspace).activeId, intent).catch((err) => console.warn("移动图层失败", err));
  }

  async function setMask(layerId: string, operation: LayerMaskOperation): Promise<void> {
    const tabId = get(workspace).activeId;
    if (!tabId || docState.docId !== tabId) return;
    const request = ++targetRequest;
    const activeBefore = docState.activeLayerID;

    let selectionJSON = "";
    if (operation === "createRevealSelection" || operation === "createHideSelection") {
      const selection = get(currentSelection);
      if (!selection?.mask.some((value) => value > 0)) return;
      selectionJSON = selectionPayload(
        selection.mask,
        selection.width,
        selection.height,
        selection.feather,
      );
    }

    // 跨标签守卫与失败告警在 requestLayerMask 里（与"添加图层蒙版"命令共用）。
    const raw = await requestLayerMask(tabId, layerId, operation, selectionJSON);
    if (raw === null) return;
    const unchangedSelection = get(documentState).activeLayerID === activeBefore;
    applyDocumentSnapshot(raw, tabId);
    if (request !== targetRequest || !unchangedSelection) return;
    if (operation === "delete") clearMaskTarget(tabId);
    else await selectMask(layerId);
  }

  function chooseMaskAction(
    layerId: string,
    operation: string,
    menu: HTMLSelectElement,
  ): void {
    menu.value = "";
    if (!operation) return;
    void setMask(layerId, operation as LayerMaskOperation);
  }

  async function selectMask(layerId: string): Promise<void> {
    const tabId = get(workspace).activeId;
    if (!tabId || docState.docId !== tabId) return;
    const request = ++targetRequest;
    try {
      if (get(documentState).activeLayerID !== layerId) await layerOps.setActive(layerId);
      if (request === targetRequest) selectMaskTarget(tabId, layerId);
    } catch (err) {
      console.warn("选择蒙版失败", err);
    }
  }
</script>

<div class="panel-title">图层</div>

{#if !docState.docId}
  <div class="panel-empty">没有打开的文档</div>
{:else}
  <div class="layer-list" role="listbox" aria-label="图层列表">
    {#each rows as { layer, depth } (layer.id)}
      <div
        class="layer-entry"
        role="presentation"
        data-layer-id={layer.id}
        data-depth={depth}
        data-drop-placement={dropIntent?.targetID === layer.id ? dropIntent.placement : undefined}
        style:margin-left={`${depth * 16}px`}
        class:drop-target={dropIntent?.targetID === layer.id}
        ondragover={(e) => {
          if (e.dataTransfer?.types.includes(LAYER_DRAG_MIME)) {
            e.preventDefault();
            updateLayerDrop(e, layer);
          }
        }}
        ondragleave={() => {
          if (dropIntent?.targetID === layer.id) dropIntent = null;
        }}
        ondrop={(e) => finishLayerDrop(e, layer)}
      >
        <div
          class="row"
          class:active={layer.id === docState.activeLayerID && maskTargetId !== layer.id}
          role="option"
          aria-selected={layer.id === docState.activeLayerID && maskTargetId !== layer.id}
          tabindex="0"
          draggable="true"
          onclick={(e) => { if (!isLayerPanelChildEventTarget(e)) activateLayer(layer.id); }}
          onkeydown={(e) => {
            if (!isLayerPanelChildEventTarget(e) && (e.key === "Enter" || e.key === " ")) {
              e.preventDefault();
              activateLayer(layer.id);
            }
          }}
          ondragstart={(e) => {
            // 拖到别的标签上 = 把这一层（连同子树）复制进那个项目（票 48）。
            // 注意：这里**不能** preventDefault —— 按 HTML 规范那会取消整个拖拽
            // 操作，跨标签复制就永远起不来；面板内部的拖拽重排（票 20）落到
            // 面板自己的 drop 处理器上，与这条自定义 MIME 互不干扰。
            beginLayerDrag(e, layer.id);
          }}
          ondragend={() => {
            dragSourceId = null;
            dropIntent = null;
          }}
        >
        <button
          class="eye"
          title={layer.isVisible ? "隐藏图层" : "显示图层"}
          aria-label={layer.isVisible ? `隐藏 ${layer.name}` : `显示 ${layer.name}`}
          onclick={(e) => {
            e.stopPropagation();
            void layerOps.setVisible(layer.id, !layer.isVisible);
          }}
        >
          {#if layer.isVisible}
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
              <path
                d="M8 3C4.5 3 1.9 5.6 1 8c.9 2.4 3.5 5 7 5s6.1-2.6 7-5c-.9-2.4-3.5-5-7-5Zm0 8.2A3.2 3.2 0 1 1 8 4.8a3.2 3.2 0 0 1 0 6.4ZM8 6.4A1.6 1.6 0 1 0 8 9.6 1.6 1.6 0 0 0 8 6.4Z"
                fill="currentColor"
              />
            </svg>
          {:else}
            <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
              <path
                d="M8 3C4.5 3 1.9 5.6 1 8c.5 1.3 1.5 2.7 2.9 3.7L2.6 13l1 1L13.4 4l-1-1-1.9 1.9C9.7 3.7 8.9 3 8 3Zm0 1.8c.5 0 1 .1 1.5.3l-1 1A3.2 3.2 0 0 0 4.8 10l-1.1 1.1C2.7 10.4 1.9 9.2 1.6 8 2.5 5.9 4.9 4.8 8 4.8Zm3.7 2.4 1-1c.7.6 1.2 1.2 1.7 1.8-.9 2.1-3.3 5-7 5-.4 0-.8 0-1.2-.1l1.2-1.2A3.2 3.2 0 0 0 11.7 7.2Z"
                fill="currentColor"
              />
            </svg>
          {/if}
        </button>

        <img
          class="layer-thumb"
          src={`/pixel/${docState.docId}/${layer.id}/image.png?v=${docState.rev}`}
          alt=""
          draggable="false"
          aria-hidden="true"
        />

        {#if editingId === layer.id}
          <input
            class="rename"
            bind:value={editName}
            use:focusSelect
            aria-label="图层名称"
            onkeydown={(e) => {
              e.stopPropagation();
              if (e.key === "Enter" || e.key === "Escape") e.preventDefault();
              if (e.key === "Enter") commitRename();
              else if (e.key === "Escape") cancelRename();
            }}
            onblur={commitRename}
          />
        {:else}
          <span
            class="name"
            class:group={layer.isGroup}
            role="button"
            tabindex="0"
            aria-label={`重命名 ${layer.name}`}
            ondblclick={() => startRename(layer)}
            onkeydown={(e) => {
              if (e.key === "Enter") { e.preventDefault(); e.stopPropagation(); startRename(layer); }
            }}
            title="双击重命名"
          >
            {layer.name}
          </span>
        {/if}

        <select
          class="blend"
          aria-label="混合模式"
          title="混合模式"
          value={layer.blendMode}
            onchange={(e) => {
              e.stopPropagation();
              void layerOps.setBlendMode(layer.id, e.currentTarget.value);
            }}
            onclick={(e) => e.stopPropagation()}
            onkeydown={(e) => e.stopPropagation()}
        >
          {#each BLEND_MODES as mode (mode)}
            <option value={mode}>{mode}</option>
          {/each}
        </select>

        <label class="opacity" title="不透明度">
          <input
            type="range"
            min="0"
            max="100"
            value={Math.round(layer.opacity * 100)}
            aria-label="不透明度"
            onchange={(e) => {
              e.stopPropagation();
              void layerOps.setOpacity(layer.id, Number(e.currentTarget.value) / 100);
            }}
          />
          <span class="opacity-value">{Math.round(layer.opacity * 100)}%</span>
        </label>
        {#if !layer.maskFile}
          <select
            class="mask-menu mask-create"
            aria-label={`为 ${layer.name} 添加蒙版`}
            title="添加蒙版"
            onchange={(e) => {
              e.stopPropagation();
              chooseMaskAction(layer.id, e.currentTarget.value, e.currentTarget);
            }}
            onclick={(e) => e.stopPropagation()}
            onkeydown={(e) => e.stopPropagation()}
          >
            <option value="">添加蒙版…</option>
            <option value="createRevealAll">显示全部</option>
            <option value="createHideAll">隐藏全部</option>
            <option value="createRevealSelection" disabled={!hasSelection}>显示选区</option>
            <option value="createHideSelection" disabled={!hasSelection}>隐藏选区</option>
          </select>
        {/if}
        {#if layer.adjustment && layer.adjustment.kind !== "Invert"}
          <button
            class="edit-adjustment"
            title="编辑调整（Edit Adjustment）"
            disabled={$filterSession !== null}
            onclick={(e) => {
              e.stopPropagation();
              void beginFilter(
                `adjust:${String(layer.adjustment?.kind ?? "")}`,
                `编辑调整 · ${String(layer.adjustment?.kind ?? "")}`,
                false,
                layer.id,
                layer.adjustment ?? undefined,
              );
            }}
          >
            编辑
          </button>
        {/if}
        </div>

        {#if layer.maskFile}
          <div
            class="mask-row"
            class:active={maskTargetId === layer.id}
            role="option"
            aria-selected={maskTargetId === layer.id}
            tabindex="0"
            onclick={(e) => {
              e.stopPropagation();
              selectMask(layer.id);
            }}
            onkeydown={(e) => {
              e.stopPropagation();
              if (!isLayerPanelChildEventTarget(e) && (e.key === "Enter" || e.key === " ")) {
                e.preventDefault();
                void selectMask(layer.id);
              }
            }}
          >
            <img
              class="mask-thumb"
              src={`/pixel/${docState.docId}/${layer.id}/mask.png?v=${docState.rev}`}
              alt=""
              draggable="false"
              aria-hidden="true"
            />
            <span class="mask-name">蒙版</span>
            <select
              class="mask-menu"
              aria-label={`${layer.name} 蒙版操作`}
              title="蒙版操作"
              onchange={(e) => {
                e.stopPropagation();
                chooseMaskAction(layer.id, e.currentTarget.value, e.currentTarget);
              }}
              onclick={(e) => e.stopPropagation()}
              onkeydown={(e) => e.stopPropagation()}
            >
              <option value="">操作…</option>
              <option value={layer.maskEnabled === false ? "enable" : "disable"}>
                {layer.maskEnabled === false ? "启用" : "停用"}
              </option>
              {#if !layer.isGroup && !layer.adjustment}
                <option value={layer.maskLinked === false ? "link" : "unlink"}>
                  {layer.maskLinked === false ? "链接" : "取消链接"}
                </option>
              {/if}
              <option value="invert">反相</option>
              <option value="delete">删除蒙版</option>
            </select>
          </div>
        {/if}
      </div>
    {:else}
      <div class="panel-empty">暂无图层</div>
    {/each}
  </div>

  <div class="panel-actions">
    <button class="add" title="新建图层" onclick={() => void layerOps.addLayer()}>＋</button>
    <button
      title={selectedMaskLayer?.maskFile ? "删除所选蒙版" : "删除所选图层"}
      disabled={!docState.activeLayerID && !selectedMaskLayer?.maskFile}
      onclick={() => {
        const target = selectedMaskLayer;
        if (target?.maskFile) void setMask(target.id, "delete");
        else if (docState.activeLayerID) void layerOps.deleteLayer(docState.activeLayerID);
      }}
    >
      {selectedMaskLayer?.maskFile ? "删除蒙版" : "删除"}
    </button>
  </div>
{/if}

<style>
  .panel-title {
    font-weight: 600;
    color: var(--text-strong);
    margin-bottom: 10px;
    font-size: 12px;
    letter-spacing: 0.02em;
  }

  .panel-empty {
    color: var(--text-dim);
  }

  .layer-list {
    display: flex;
    flex-direction: column;
    gap: 5px;
  }

  .layer-entry {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .layer-entry[data-drop-placement="before"] { border-top: 2px solid var(--accent); }
  .layer-entry[data-drop-placement="after"] { border-bottom: 2px solid var(--accent); }
  .layer-entry[data-drop-placement="inside"] { outline: 2px solid var(--accent); }

  .row {
    display: grid;
    grid-template-columns: 22px minmax(0, 1fr);
    grid-template-areas:
      "eye name"
      "blend blend"
      "opacity opacity";
    gap: 2px 4px;
    align-items: center;
    padding: 4px;
    border-radius: 4px;
    background: var(--bg-raised);
    cursor: default;
  }

  .row {
    grid-template-columns: 22px 28px minmax(0, 1fr);
    grid-template-areas:
      "eye thumb name"
      "blend blend blend"
      "opacity opacity opacity";
  }

  .layer-thumb {
    grid-area: thumb;
    width: 26px;
    height: 22px;
    object-fit: cover;
    border: 1px solid var(--border);
    border-radius: 2px;
    background: var(--bg-app);
  }

  .row.active {
    background: var(--accent-surface);
    box-shadow: inset 2px 0 var(--accent);
  }

  .row:hover:not(.active) {
    background: #373d46;
  }

  .row:focus-visible,
  .mask-row:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  .eye {
    grid-area: eye;
    width: 20px;
    height: 20px;
    padding: 0;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: none;
  }

  .name {
    grid-area: name;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .name.group {
    font-weight: 600;
  }

  .rename {
    grid-area: name;
    min-width: 0;
    background: var(--bg-app);
    border: 1px solid var(--accent);
    border-radius: 3px;
    color: var(--text);
    font: inherit;
    padding: 1px 4px;
  }

  .blend {
    grid-area: blend;
    min-width: 0;
    background: var(--bg-app);
    border: 1px solid var(--border);
    border-radius: 3px;
    color: var(--text);
    font: inherit;
    font-size: 11px;
    padding: 1px 2px;
  }

  .opacity {
    grid-area: opacity;
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .opacity input {
    flex: 1;
    min-width: 0;
    accent-color: var(--accent);
  }

  .opacity-value {
    width: 34px;
    text-align: right;
    color: var(--text-dim);
    font-size: 11px;
  }

  .mask-menu {
    min-width: 0;
    background: var(--bg-app);
    border: 1px solid var(--border);
    border-radius: 3px;
    color: var(--text);
    font: inherit;
    font-size: 11px;
    padding: 1px 2px;
  }

  .mask-create {
    grid-column: 1 / -1;
  }

  .mask-row {
    display: grid;
    grid-template-columns: 22px minmax(0, 1fr) minmax(88px, auto);
    gap: 4px;
    align-items: center;
    min-height: 28px;
    margin-left: 26px;
    padding: 3px 4px;
    border-radius: 4px;
    background: var(--bg-raised);
    color: var(--text-dim);
    cursor: default;
  }

  .mask-row.active {
    background: var(--accent-dim);
    color: var(--text);
  }

  .mask-thumb {
    width: 22px;
    height: 18px;
    border: 1px solid var(--border);
    border-radius: 2px;
    background: repeating-conic-gradient(#f2f2f2 0 25%, #707070 0 50%) 50% / 8px 8px;
  }

  .mask-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .panel-actions {
    display: flex;
    gap: 6px;
    margin-top: 8px;
  }

  .panel-actions .add {
    width: 24px;
    padding: 0;
  }
</style>
