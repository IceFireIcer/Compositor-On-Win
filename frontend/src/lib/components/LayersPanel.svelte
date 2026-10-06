<script lang="ts">
  import {
    BLEND_MODES,
    displayRows,
    document as documentState,
    layerOps,
    type DocLayer,
  } from "../state/document";
  import { beginFilter, filterSession } from "../state/filters";

  /**
   * The layer panel (ticket 20 shell + bridge wiring). Rows render top→bottom
   * (Photoshop convention) out of the bottom→top domain array; every edit is
   * a Service.LayerOp round-trip whose fresh snapshot updates the store.
   */

  const docState = $derived($documentState);
  const rows = $derived(displayRows(docState.layers));

  // Note: svelte-check (svelte 5.19) rejects the generic rune form here —
  // `$state<string | null>(null)` parses as a legacy store subscription and
  // errors. Annotate the variable and pass the initial value instead.
  let editingId: string | null = $state(null);
  let editName = $state("");

  function findLayer(id: string): DocLayer | undefined {
    return docState.layers.find((l) => l.id === id);
  }

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
</script>

<div class="panel-title">图层</div>

{#if !docState.docId}
  <div class="panel-empty">没有打开的文档</div>
{:else}
  <div class="layer-list" role="listbox" aria-label="图层列表">
    {#each rows as layer (layer.id)}
      <div
        class="row"
        class:active={layer.id === docState.activeLayerID}
        role="option"
        aria-selected={layer.id === docState.activeLayerID}
        tabindex="0"
        onclick={() => void layerOps.setActive(layer.id)}
        onkeydown={(e) => (e.key === "Enter" || e.key === " ") && void layerOps.setActive(layer.id)}
      >
        <button
          class="eye"
          title={layer.isVisible ? "隐藏图层" : "显示图层"}
          aria-label={layer.isVisible ? `隐藏 ${layer.name}` : `显示 ${layer.name}`}
          onclick={() => void layerOps.setVisible(layer.id, !layer.isVisible)}
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

        {#if editingId === layer.id}
          <input
            class="rename"
            bind:value={editName}
            use:focusSelect
            aria-label="图层名称"
            onkeydown={(e) => {
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
            onkeydown={(e) => e.key === "Enter" && startRename(layer)}
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
          onchange={(e) => void layerOps.setBlendMode(layer.id, e.currentTarget.value)}
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
            onchange={(e) =>
              void layerOps.setOpacity(layer.id, Number(e.currentTarget.value) / 100)}
          />
          <span class="opacity-value">{Math.round(layer.opacity * 100)}%</span>
        </label>
        {#if layer.adjustment}
          <button
            class="edit-adjustment"
            title="编辑调整（Edit Adjustment）"
            disabled={$filterSession !== null}
            onclick={() =>
              void beginFilter(
                `adjust:${String(layer.adjustment?.kind ?? "")}`,
                `编辑调整 · ${String(layer.adjustment?.kind ?? "")}`,
                false,
                layer.id,
                layer.adjustment ?? undefined,
              )}
          >
            编辑
          </button>
        {/if}
      </div>
    {:else}
      <div class="panel-empty">暂无图层</div>
    {/each}
  </div>

  <div class="panel-actions">
    <button class="add" title="新建图层" onclick={() => void layerOps.addLayer()}>＋</button>
    <button
      title="删除所选图层"
      disabled={!docState.activeLayerID}
      onclick={() => docState.activeLayerID && void layerOps.deleteLayer(docState.activeLayerID)}
    >
      删除
    </button>
  </div>
{/if}

<style>
  .panel-title {
    font-weight: 600;
    margin-bottom: 6px;
  }

  .panel-empty {
    color: var(--text-dim);
  }

  .layer-list {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }

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

  .row.active {
    background: var(--accent-dim);
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
