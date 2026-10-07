<script lang="ts">
  import {
    JPEG_ZOOM_STEPS,
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
  // JPEGPreview.steps: Fit (null) and the 0.25×…8× ladder, plus a free
  // "Fit ↔ 100%" toggle on double-click.
  let zoom = $state<number | null>(null);
  let exporting = $state(false);
  let error = $state<string | null>(null);
  let { onClose }: { onClose?: () => void } = $props();

  const preview = $derived($exportPreview);

  function stepZoom(direction: 1 | -1): void {
    const current = zoom ?? 1;
    const ladder = JPEG_ZOOM_STEPS as readonly number[];
    if (direction === 1) {
      const next = ladder.find((z) => z > current + 1e-9);
      if (next !== undefined) zoom = next;
    } else {
      const next = [...ladder].reverse().find((z) => z < current - 1e-9);
      if (next !== undefined && next >= 0.25) zoom = next;
    }
  }

  function toggleFit(): void {
    zoom = zoom === null ? 1 : null;
  }

  function setBackground(event: Event): void {
    const value = (event.target as HTMLInputElement).value;
    preview && exportPreview.update((state) => (state ? { ...state, background: value } : state));
  }

  $effect(() => {
    void beginExportPreview();
    return () => endExportPreview();
  });

  // Quality/background → re-encode (debounced like the filter previews).
  let debounce: ReturnType<typeof setTimeout> | null = null;
  $effect(() => {
    const bg = preview?.background ?? "#ffffff";
    void quality;
    void bg;
    if (debounce) clearTimeout(debounce);
    debounce = setTimeout(() => {
      void updateExportPreview(quality, bg).catch((err) => {
        error = err instanceof Error ? err.message : String(err);
      });
    }, 150);
    return () => {
      if (debounce) clearTimeout(debounce);
    };
  });

  async function doExport(): Promise<void> {
    exporting = true;
    error = null;
    try {
      if (await commitExportJPEG(quality, preview?.background ?? "#ffffff")) close();
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
        <div
          class="preview-scroll"
          role="presentation"
          ondblclick={toggleFit}
        >
          <img
            src={preview.jpegURL}
            alt="JPEG 导出预览"
            style:width={zoom === null ? "100%" : "auto"}
            style:max-width={zoom === null ? "100%" : "none"}
            style:image-rendering={zoom !== null && zoom >= 2 ? "pixelated" : "auto"}
          />
        </div>
        <div class="preview-meta">
          <span>
            {preview.width}×{preview.height}{#if preview.size}
              · 预览约 {formatBytes(preview.size)}（导出按原始分辨率）{/if}
          </span>
          <span class="zoom">
            <button type="button" disabled={zoom === null} onclick={() => (zoom = null)}>适应</button>
            <button type="button" onclick={() => stepZoom(-1)}>−</button>
            {zoom === null ? "适应" : `${Math.round(zoom * 100)}%`}
            <button type="button" onclick={() => stepZoom(1)}>＋</button>
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
    <label class="background">
      <span>透明区域背景色</span>
      <input type="color" value={preview?.background ?? "#ffffff"} oninput={setBackground} />
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

  .preview-scroll {
    cursor: zoom-in;
  }

  .preview-scroll img {
    display: block;
    margin: 0 auto;
  }

  .background {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 12px;
    font-size: 13px;
  }

  .background input {
    width: 42px;
    height: 24px;
    padding: 0;
    border: 1px solid var(--border);
    background: none;
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
