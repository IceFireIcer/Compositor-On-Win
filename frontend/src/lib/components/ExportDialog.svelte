<script lang="ts">
  import {
    beginExportPreview,
    commitExportJPEG,
    endExportPreview,
    exportPreview,
    formatBytes,
    updateExportPreview,
  } from "../state/export";

  /**
   * Export JPEG…（ticket 42）：一次压平的光栅在质量滑杆之间复用，滑杆
   * 移动只重编码（150ms 防抖）；预览显示真实 JPEG 压缩痕迹，可在预览
   * 内缩放（fit ↔ 100% ↔ 放大档位）。
   */

  let quality = $state(85);
  let zoom = $state(0); // 0 = fit, 1 = 100%, 2 = 200%
  let exporting = $state(false);
  let error = $state<string | null>(null);
  let { onClose }: { onClose?: () => void } = $props();

  const preview = $derived($exportPreview);

  $effect(() => {
    void beginExportPreview();
    return () => endExportPreview();
  });

  // Quality slider → re-encode (debounced like the filter previews).
  let debounce: ReturnType<typeof setTimeout> | null = null;
  $effect(() => {
    void quality;
    if (debounce) clearTimeout(debounce);
    debounce = setTimeout(() => {
      void updateExportPreview(quality).catch((err) => {
        error = err instanceof Error ? err.message : String(err);
      });
    }, 150);
    return () => {
      if (debounce) clearTimeout(debounce);
    };
  });

  const zoomLabels = ["适应", "100%", "200%"];

  async function doExport(): Promise<void> {
    exporting = true;
    error = null;
    try {
      if (await commitExportJPEG(quality)) close();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      exporting = false;
    }
  }

  function close(): void {
    onClose?.();
  }
</script>

<div
  class="backdrop"
  role="presentation"
  onclick={(e) => {
    if (e.target === e.currentTarget) close();
  }}
>
  <div class="dialog" role="dialog" aria-modal="true" aria-label="导出 JPEG">
    <h2>导出 JPEG</h2>
    <div class="preview-area">
      {#if preview?.jpegURL}
        <div class="preview-scroll">
          <img
            src={preview.jpegURL}
            alt="JPEG 导出预览"
            style:width={zoom === 0 ? "100%" : zoom === 1 ? "auto" : "200%"}
            style:image-rendering={zoom >= 2 ? "pixelated" : "auto"}
          />
        </div>
        <div class="preview-meta">
          <span>
            {preview.width}×{preview.height}{#if preview.size}
              · 预览约 {formatBytes(preview.size)}（导出按原始分辨率）{/if}
          </span>
          <span class="zoom">
            <button type="button" onclick={() => (zoom = Math.max(0, zoom - 1))}>−</button>
            {zoomLabels[zoom]}
            <button type="button" onclick={() => (zoom = Math.min(2, zoom + 1))}>＋</button>
          </span>
        </div>
      {:else}
        <div class="placeholder">正在生成预览…</div>
      {/if}
    </div>
    <label class="quality">
      <span>质量：{quality}%</span>
      <input type="range" min="1" max="100" bind:value={quality} />
    </label>
    {#if error}
      <p class="error">{error}</p>
    {/if}
    <div class="actions">
      <button type="button" onclick={close}>取消</button>
      <button type="button" class="primary" disabled={exporting || !preview} onclick={() => void doExport()}>
        {exporting ? "正在导出…" : "导出…"}
      </button>
    </div>
  </div>
</div>

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
    width: 560px;
    max-height: 80vh;
    display: flex;
    flex-direction: column;
    padding: 16px;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 8px;
  }

  h2 {
    margin: 0 0 10px;
    font-size: 14px;
  }

  .preview-area {
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-canvas, #202020);
    margin-bottom: 12px;
  }

  .preview-scroll {
    max-height: 300px;
    overflow: auto;
    display: flex;
  }

  .preview-scroll img {
    display: block;
    margin: 0 auto;
  }

  .placeholder {
    padding: 40px;
    text-align: center;
    color: var(--text-dim);
  }

  .preview-meta {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 6px 10px;
    border-top: 1px solid var(--border);
    color: var(--text-dim);
    font-size: 12px;
  }

  .zoom {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .zoom button {
    width: 22px;
    height: 22px;
    padding: 0;
    line-height: 1;
  }

  .quality {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 12px;
    font-size: 13px;
  }

  .error {
    margin: 0 0 10px;
    color: var(--danger);
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

  .primary {
    background: var(--accent, #3b82f6);
    border-color: var(--accent, #3b82f6);
    color: #fff;
  }
</style>
