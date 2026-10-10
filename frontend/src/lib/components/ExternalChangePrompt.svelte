<script lang="ts">
  import { pendingExternalChange, resolveExternalChange } from "../state/external";

  /**
   * 外部变更询问（票 51 / 故事 56）：文档有未保存修改时，磁盘上的包被代理改过。
   * 原版语义：绝不静默覆盖用户的修改 —— 要么载入磁盘版本（丢掉本地修改），要么保留。
   */
  const pending = $derived($pendingExternalChange);

  function answer(accept: boolean): void {
    void resolveExternalChange(accept);
  }

  // Esc = 保底的"保留我的修改"：询问的目的是别弄丢用户的活，默认动作必须是不丢。
  function onkeydown(event: KeyboardEvent): void {
    if (!pending) return;
    if (event.key === "Escape") answer(false);
  }

  function focus(node: HTMLElement): void {
    node.focus();
  }
</script>

<svelte:window {onkeydown} />

{#if pending}
  <div class="backdrop" role="presentation">
    <div class="dialog" role="alertdialog" aria-modal="true" aria-label="文件在磁盘上被修改了">
      <h2>文件在磁盘上被修改了</h2>
      <p>「{pending.path}」有外部改动，而这个文档还有未保存的修改。</p>
      <p class="dim">
        载入磁盘版本会丢弃你的修改；保留修改则继续用当前画布，下一次外部改动会再问一次。
      </p>
      <div class="actions">
        <button type="button" onclick={() => answer(false)}>保留我的修改</button>
        <button type="button" class="primary" use:focus onclick={() => answer(true)}>载入磁盘版本</button>
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
    z-index: 30;
  }

  .dialog {
    width: 420px;
    max-width: calc(100vw - 40px);
    padding: 18px;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 8px;
  }

  h2 {
    margin: 0 0 10px;
    font-size: 14px;
  }

  p {
    margin: 0 0 10px;
    word-break: break-all;
  }

  .dim {
    color: var(--text-dim);
    font-size: 12px;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  .actions button {
    padding: 6px 14px;
  }

  .actions button.primary {
    background: var(--accent-dim);
    border-color: var(--accent, #3b82f6);
    color: #fff;
  }
</style>
