<script lang="ts">
  import { cancelPendingOpen, confirmPendingOpen, type PendingConversion } from "../state/workspace";

  /**
   * Photoshop 转换报告（票 39）：逐项列出保留/降级，确认后才应用——
   * 原版 PSDConversionRequest 的先报告后插入语义。空报告不出表单。
   */
  let { conversions, onclose }: { conversions: PendingConversion[]; onclose: () => void } = $props();

  let busy = $state(false);

  async function confirm(): Promise<void> {
    busy = true;
    try {
      await confirmPendingOpen();
      onclose();
    } catch (err) {
      console.warn("确认导入失败", err);
      busy = false;
    }
  }

  async function cancel(): Promise<void> {
    await cancelPendingOpen();
    onclose();
  }
</script>

<div class="backdrop" role="presentation">
  <div class="sheet" role="dialog" aria-modal="true" aria-label="Photoshop 导入说明">
    <h2>Photoshop 导入说明</h2>
    <p class="hint">以下图层以像素导入或有所降级；确认后应用。</p>
    <ul class="notes">
      {#each conversions as note, i (i)}
        <li><strong>{note.layerName}</strong>：{note.message}</li>
      {/each}
    </ul>
    <div class="actions">
      <button type="button" onclick={() => void cancel()} disabled={busy}>取消</button>
      <button type="button" class="primary" onclick={() => void confirm()} disabled={busy}>
        {busy ? "正在导入…" : "确认导入"}
      </button>
    </div>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 25;
  }

  .sheet {
    width: 520px;
    max-height: 70vh;
    display: flex;
    flex-direction: column;
    padding: 16px;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 8px;
  }

  h2 {
    margin: 0 0 8px;
    font-size: 14px;
  }

  .hint {
    margin: 0 0 10px;
    color: var(--text-dim);
    font-size: 12px;
  }

  .notes {
    margin: 0 0 14px;
    padding-left: 18px;
    overflow-y: auto;
    font-size: 13px;
    line-height: 1.5;
  }

  .actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
  }

  .actions button {
    padding: 6px 14px;
  }

  .primary {
    background: var(--accent, #3b82f6);
    border-color: var(--accent, #3b82f6);
    color: #fff;
  }
</style>
