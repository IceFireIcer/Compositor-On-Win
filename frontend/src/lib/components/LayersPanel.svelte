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
  import { depthMenuItemsFor, layerDepth, refreshLayerDepth, runDepthCommand } from "../state/layerDepth";
  import { loadLayerSelection, type SampleMode } from "../state/selectionEdit";
  import type { EffectKind } from "../state/layerOps";

  /**
   * 特效行的标题。面板 UI 是中文，所以这里自带一份；`state/layerOps.ts` 的
   * EFFECT_KIND_TITLES 是**撤销事务名**，不面向用户，保持英文。
   */
  const EFFECT_LABELS: Record<EffectKind, string> = {
    stroke: "描边",
    shadow: "投影",
    colorOverlay: "颜色叠加",
    innerShadow: "内阴影",
    outerGlow: "外发光",
    innerGlow: "内发光",
  };

  /**
   * 这一层挂着的特效，按 domain.Effects 的字段顺序（与 Go 和原版一致），所以行的次序
   * 稳定，不会随对象键序变。
   */
  function effectKindsOf(layer: DocLayer): EffectKind[] {
    const effects = layer.effects;
    if (!effects) return [];
    return (Object.keys(EFFECT_LABELS) as EffectKind[]).filter((kind) => effects[kind] != null);
  }

  /**
   * Ctrl（macOS 的 ⌘）点缩略图把该层的像素 / 蒙版载入为选区；Shift 加选、Alt 减选
   * （原版 NativeLayerList.loadMaskSelection/loadLayerSelection 的 loadMode）。
   * 不带修饰键的普通点击不拦，仍由外层容器处理（选中图层 / 蒙版目标）。
   */
  function loadThumbSelection(event: MouseEvent, kind: "pixels" | "mask"): void {
    if (!event.ctrlKey && !event.metaKey) return;
    event.preventDefault();
    event.stopPropagation();
    const mode: SampleMode = event.altKey ? "subtract" : event.shiftKey ? "add" : "replace";
    void loadLayerSelection(kind, mode).catch((err) => {
      window.alert(`载入选区失败：${err instanceof Error ? err.message : String(err)}`);
    });
  }

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
  // 右键菜单（票 69）：上游 NativeLayerList 的菜单里就有「转换为 16 位 / 8 位」，
  // 光有顶部菜单栏不算接完。位置用视口坐标，越界由 CSS 收回来。
  // tabID 与 layerID 是两回事：位深命令按标签找到会话、再作用在活动图层上。
  // 两个 id 都在这里存下来，免得点菜单时把图层 id 当标签 id 递过去。
  let layerMenu = $state<{ x: number; y: number; layerId: string; tabID: string } | null>(null);
  let menuEl: HTMLDivElement | undefined = $state();

  // 右键那一层先成为活动层，位深命令（Go 侧只认 ActiveLayerID）才会作用到它；
  // 然后按这一层重取标签，菜单里的项才是它自己的。
  // 命中的是行内子元素（比如位深标签）时也要认这一层，所以这里不看
  // isLayerPanelChildEventTarget —— 那是给左键选中用的。
  async function openLayerMenu(event: MouseEvent, layer: DocLayer): Promise<void> {
    event.preventDefault();
    const at = { x: event.clientX, y: event.clientY };
    activateLayer(layer.id);
    await refreshLayerDepth();
    layerMenu = { ...at, layerId: layer.id, tabID: docState.docId ?? "" };
  }

  // 菜单外的 pointerdown 才关菜单。全局无条件关会在按钮的 click 之前就把菜单
  // 拆掉（pointerdown 先于 click），菜单项就永远点不中。
  $effect(() => {
    if (!layerMenu) return;
    const dismiss = (event: Event) => {
      if (menuEl && event.target instanceof Node && menuEl.contains(event.target)) return;
      layerMenu = null;
    };
    const onKey = (event: KeyboardEvent) => { if (event.key === "Escape") layerMenu = null; };
    globalThis.addEventListener("pointerdown", dismiss);
    globalThis.addEventListener("keydown", onKey);
    return () => {
      globalThis.removeEventListener("pointerdown", dismiss);
      globalThis.removeEventListener("keydown", onKey);
    };
  });
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
          oncontextmenu={(e) => openLayerMenu(e, layer)}
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
          onclick={(e) => loadThumbSelection(e, "pixels")}
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
            {#if $layerDepth.labels[layer.id]}
              <span class="depth">{ $layerDepth.labels[layer.id] }</span>
            {/if}
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
              onclick={(e) => loadThumbSelection(e, "mask")}
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
        {#each effectKindsOf(layer) as kind (kind)}
          <div class="effect-row">
            <span class="effect-name">{EFFECT_LABELS[kind]}</span>
            <button
              class="effect-delete"
              type="button"
              title={`删除${EFFECT_LABELS[kind]}`}
              aria-label={`删除 ${layer.name} 的${EFFECT_LABELS[kind]}`}
              onclick={(e) => {
                e.stopPropagation();
                void layerOps.deleteEffect(layer.id, kind);
              }}
            >×</button>
          </div>
        {/each}
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

  {#if layerMenu}
    {@const menuLayer = findLayer(layerMenu.layerId)}
    {#if menuLayer}
      {@const items = depthMenuItemsFor(
        $layerDepth.labels[menuLayer.id] ?? "",
        !menuLayer.isGroup && !menuLayer.adjustment && !menuLayer.text,
      )}
      <div
        class="layer-menu"
        bind:this={menuEl}
        style:left={`${layerMenu.x}px`}
        style:top={`${layerMenu.y}px`}
        role="menu"
      >
        {#each items as item (item.command)}
          <button
            type="button"
            role="menuitem"
            onclick={() => {
              const tabID = layerMenu?.tabID ?? "";
              layerMenu = null;
              if (tabID) void runDepthCommand(tabID, item.command);
            }}
          >
            {item.label}
          </button>
        {/each}
        {#if items.length === 0}
          <button type="button" role="menuitem" disabled>此图层无法转换位深</button>
        {/if}
      </div>
    {/if}
  {/if}
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

  /* 特效行：缩进在图层名下面，与蒙版行同一层视觉层次。 */
  .effect-row {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 1px 4px 1px 26px;
    font-size: 11px;
    color: var(--text-dim);
  }

  .effect-name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .effect-delete {
    border: none;
    background: transparent;
    color: var(--text-dim);
    cursor: pointer;
    font-size: 12px;
    line-height: 1;
    padding: 0 4px;
  }

  .effect-delete:hover {
    color: var(--text-strong);
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

  /* 右键菜单固定在视口坐标上，越界时由 max-* 收回来。 */
  .layer-menu {
    position: fixed;
    z-index: 40;
    display: flex;
    flex-direction: column;
    min-width: 160px;
    padding: 4px;
    background: var(--bg-panel, #2b2b2b);
    border: 1px solid var(--border, #444);
    border-radius: 4px;
    box-shadow: 0 6px 18px rgb(0 0 0 / 45%);
  }

  .layer-menu button {
    padding: 5px 10px;
    color: var(--text);
    text-align: left;
    background: transparent;
    border: none;
    border-radius: 3px;
    font: inherit;
    cursor: pointer;
  }

  .layer-menu button:hover:not(:disabled) {
    background: var(--accent, #3d6ea5);
  }

  .layer-menu button:disabled {
    color: var(--text-dim, #888);
    cursor: default;
  }

  .depth {
    margin-left: 6px;
    color: var(--text-dim, #9aa);
    font-size: 10px;
    letter-spacing: 0.04em;
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
