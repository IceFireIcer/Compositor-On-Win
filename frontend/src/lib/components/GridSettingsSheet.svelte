<script lang="ts">
  import { onMount } from "svelte";
  import { get } from "svelte/store";
  import { registerCanvasModalGuard } from "../shortcuts/dispatch";
  import { normalizeGrid } from "../state/snap";
  import { gridSettingsOpen, layoutGrid, showGrid } from "../state/view";

  let dialog: HTMLDialogElement;
  let spacing = $state(64);
  let subdivisions = $state(8);
  let originalGrid = get(layoutGrid);
  let originalVisibility = get(showGrid);
  let committed = false;

  onMount(() => {
    originalGrid = get(layoutGrid);
    originalVisibility = get(showGrid);
    spacing = originalGrid.spacing;
    subdivisions = originalGrid.subdivisions;
    showGrid.set(true);
    dialog.showModal();
    const removeGuard = registerCanvasModalGuard(() => true);
    return () => {
      removeGuard();
      if (!committed) {
        layoutGrid.set(originalGrid);
        showGrid.set(originalVisibility);
      }
    };
  });

  function preview(): void {
    if (!Number.isFinite(spacing) || !Number.isFinite(subdivisions)) return;
    const next = normalizeGrid(spacing, subdivisions);
    layoutGrid.set(next);
  }

  function apply(event: SubmitEvent): void {
    event.preventDefault();
    if (!Number.isFinite(spacing) || !Number.isFinite(subdivisions)) return;
    const next = normalizeGrid(spacing, subdivisions);
    layoutGrid.set(next);
    committed = true;
    gridSettingsOpen.set(false);
  }

  function cancel(): void {
    gridSettingsOpen.set(false);
  }
</script>

<dialog bind:this={dialog} oncancel={(event) => { event.preventDefault(); cancel(); }} aria-labelledby="grid-settings-title">
  <form onsubmit={apply}>
    <h2 id="grid-settings-title">网格设置</h2>
    <label>间距（px）
      <input type="number" min="2" max="4096" step="1" required bind:value={spacing} oninput={preview} />
    </label>
    <label>细分
      <input type="number" min="1" max={Math.min(64, Number.isFinite(spacing) ? spacing : 64)} step="1" required bind:value={subdivisions} oninput={preview} />
    </label>
    <div class="actions">
      <button type="button" onclick={cancel}>取消</button>
      <button class="primary" type="submit">确定</button>
    </div>
  </form>
</dialog>

<style>
  dialog { width: min(320px, calc(100vw - 32px)); max-height: calc(100vh - 32px); overflow: auto; margin: auto; padding: 20px; background: var(--bg-panel); color: var(--text); border: 1px solid var(--border); border-radius: 8px; }
  dialog::backdrop { background: rgb(0 0 0 / 60%); }
  form { display: grid; gap: 16px; }
  h2 { margin: 0; font-size: 18px; }
  label { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
  input { width: 90px; }
  .actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 8px; }
  :focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
</style>
