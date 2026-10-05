<script lang="ts">
  let {
    open,
    title,
    message,
    confirmLabel = "确定",
    cancelLabel = "取消",
    danger = false,
    onresult,
  }: {
    open: boolean;
    title: string;
    message: string;
    confirmLabel?: string;
    cancelLabel?: string;
    danger?: boolean;
    onresult: (ok: boolean) => void;
  } = $props();

  function finish(ok: boolean): void {
    if (open) onresult(ok);
  }

  function onkeydown(event: KeyboardEvent): void {
    if (!open) return;
    if (event.key === "Escape") finish(false);
    else if (event.key === "Enter") finish(true);
  }

  function focus(node: HTMLElement): void {
    node.focus();
  }
</script>

<svelte:window {onkeydown} />

{#if open}
  <div
    class="backdrop"
    role="presentation"
    onclick={(e) => {
      if (e.target === e.currentTarget) finish(false);
    }}
  >
    <div class="dialog" role="alertdialog" aria-modal="true" aria-label={title}>
      <h2>{title}</h2>
      <p>{message}</p>
      <div class="actions">
        <button type="button" onclick={() => finish(false)}>{cancelLabel}</button>
        <button type="button" class:danger use:focus onclick={() => finish(true)}>
          {confirmLabel}
        </button>
      </div>
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
    z-index: 20;
  }

  .dialog {
    width: 320px;
    padding: 18px;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 8px;
  }

  h2 {
    margin: 0 0 8px;
    font-size: 14px;
  }

  p {
    margin: 0 0 14px;
    color: var(--text-dim);
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  .actions button {
    padding: 6px 14px;
  }

  button.danger {
    background: var(--danger);
    border-color: var(--danger);
    color: #fff;
  }

  button.danger:hover {
    background: #c73e37;
  }
</style>
