<script lang="ts">
  import { NewDocument } from "../../../wailsjs/go/bridge/Workspace";
  import { applySnapshot, type Snapshot } from "../state/workspace";

  let {
    open,
    onclose,
  }: {
    open: boolean;
    onclose: () => void;
  } = $props();

  let width = $state(1920);
  let height = $state(1080);
  let resolution = $state(72);
  let error = $state("");
  let submitting = $state(false);

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (submitting) return;
    submitting = true;
    error = "";
    try {
      const snapshot: Snapshot = await NewDocument(width, height, resolution);
      applySnapshot(snapshot);
      onclose();
    } catch (err) {
      error = String(err)
        .replace(/^Error:\s*/, "")
        .replace(/\s*$/, "");
    } finally {
      submitting = false;
    }
  }

  function onkeydown(event: KeyboardEvent) {
    if (open && event.key === "Escape") onclose();
  }
</script>

<svelte:window {onkeydown} />

{#if open}
  <div
    class="backdrop"
    role="presentation"
    onclick={(e) => {
      if (e.target === e.currentTarget) onclose();
    }}
  >
    <div class="sheet" role="dialog" aria-modal="true" aria-label="新建画布">
      <form onsubmit={submit}>
        <h2>新建画布</h2>

        <label>
          宽度（px）
          <input type="number" min="1" bind:value={width} required />
        </label>
        <label>
          高度（px）
          <input type="number" min="1" bind:value={height} required />
        </label>
        <label>
          分辨率（ppi，1–9600）
          <input type="number" min="1" max="9600" bind:value={resolution} required />
        </label>

        {#if error}
          <p class="error" role="alert">{error}</p>
        {/if}

        <div class="actions">
          <button type="button" onclick={onclose}>取消</button>
          <button type="submit" disabled={submitting}>
            {submitting ? "创建中…" : "创建"}
          </button>
        </div>
      </form>
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.45);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 10;
  }

  .sheet {
    width: 320px;
    padding: 20px;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 8px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  h2 {
    margin: 0;
    font-size: 15px;
  }

  label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    color: var(--text-dim);
    font-size: 12px;
  }

  input {
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text);
    padding: 6px 8px;
    font: inherit;
  }

  input:focus {
    outline: 1px solid var(--accent);
  }

  .error {
    margin: 0;
    color: var(--danger);
    font-size: 12px;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 4px;
  }

  .actions button {
    padding: 6px 16px;
  }

  .actions button[type="submit"] {
    background: var(--accent-dim);
    border-color: var(--accent);
  }
</style>
