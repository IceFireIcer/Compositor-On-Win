<script lang="ts">
  import { get } from "svelte/store";
  import {
    EXPORT_MAX_SIDE,
    EXPORT_ZOOM_STEPS,
    MORE_EXPORT_FORMATS,
    PRIMARY_EXPORT_FORMATS,
    beginExportPreview,
    commitExport,
    endExportPreview,
    exportDialog,
    exportFormatDef,
    exportPreview,
    exportScalePercent,
    exportSizeForScale,
    formatBytes,
    linkedExportSize,
    rememberedQuality,
    updateExportPreview,
    type ExportKind,
    type ExportPreviewRequest,
  } from "../state/export";

  /**
   * 导出为…（ticket 59）：一个窗口覆盖 PNG/JPEG/TIFF/单页 PDF。PNG/JPEG 是
   * 首要格式，TIFF/PDF 收在更多格式区；宽高等比联动（原版 setWidth/setHeight），
   * 显示最终像素尺寸与缩放百分比。预览按格式显示真实产物：PNG/TIFF 保留透明
   * （棋盘格衬底）、JPEG 显示压缩痕迹、PDF 显示按背景压平后的页面；可在预览
   * 内缩放（适应 ↔ 100% ↔ 0.25×…8×）。旧预览请求由 export.ts 的序号守卫
   * 丢弃，绝不覆盖新结果。
   */

  let { onClose }: { onClose?: () => void } = $props();

  let format = $state<ExportKind>(get(exportDialog)?.kind ?? "png");
  let width = $state(0);
  let height = $state(0);
  let quality = $state(rememberedQuality());
  // 票 84：16 位每通道。只对 PNG/TIFF 有意义（上游同样只在 PNG 上给这个开关）。
  let sixteenBit = $state(false);
  // 票 87：CMYK 每次默认关（上游同此：多数导出是给屏幕看的）。只对 TIFF/PDF 有意义。
  let cmyk = $state(false);
  let background = $state("#ffffff");
  let zoom = $state<number | null>(null);
  let ready = $state(false);
  let exporting = $state(false);
  let error = $state<string | null>(null);

  const preview = $derived($exportPreview);
  const original = $derived({ width: preview?.width ?? width, height: preview?.height ?? height });
  const scalePercent = $derived(exportScalePercent(original, { width, height }));
  const def = $derived(exportFormatDef(format));
  // JPEG 与 PDF 没有透明通道：预览与导出都要落在选定背景上。
  // 压平（要背景色）的格式：JPEG/PDF 本来就没有透明通道；CMYK 的 TIFF 也没有（油墨没有 alpha）。
  const flattened = $derived(format === "jpeg" || format === "pdf" || (format === "tiff" && cmyk));

  // 打开窗口：压平一次（Go 侧持有光栅），尺寸从压平后的原图起步。
  $effect(() => {
    let cancelled = false;
    void beginExportPreview()
      .then(() => {
        if (cancelled) return;
        const state = get(exportPreview);
        if (state) {
          width = state.width;
          height = state.height;
        }
        ready = true;
      })
      .catch((err: unknown) => {
        error = err instanceof Error ? err.message : String(err);
      });
    return () => {
      cancelled = true;
      endExportPreview();
    };
  });

  // 设置变化 → 重编码预览（150ms 防抖，与滤镜/JPEG 预览一致）。
  let debounce: ReturnType<typeof setTimeout> | null = null;
  $effect(() => {
    const request: ExportPreviewRequest = { format, width, height, quality, background, sixteenBit, cmyk };
    if (!ready || width < 1 || height < 1) return;
    if (debounce) clearTimeout(debounce);
    debounce = setTimeout(() => {
      error = null;
      void updateExportPreview(request).catch((err: unknown) => {
        error = err instanceof Error ? err.message : String(err);
      });
    }, 150);
    return () => {
      if (debounce) clearTimeout(debounce);
    };
  });

  function setWidth(event: Event): void {
    const next = linkedExportSize(original, "width", Number((event.target as HTMLInputElement).value));
    width = next.width;
    height = next.height;
  }

  function setHeight(event: Event): void {
    const next = linkedExportSize(original, "height", Number((event.target as HTMLInputElement).value));
    width = next.width;
    height = next.height;
  }

  function applyScale(event: Event): void {
    const select = event.currentTarget as HTMLSelectElement;
    const percent = Number(select.value);
    select.value = "";
    if (!Number.isFinite(percent) || percent <= 0) return;
    const next = exportSizeForScale(original, percent / 100);
    width = next.width;
    height = next.height;
  }

  function stepZoom(direction: 1 | -1): void {
    const current = zoom ?? 1;
    const ladder = EXPORT_ZOOM_STEPS as readonly number[];
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

  function inches(pixels: number, dpi: number): string {
    const value = pixels / dpi;
    return value >= 0.01 ? value.toFixed(2).replace(/\.?0+$/, "") : value.toFixed(4);
  }

  // PDF 页面：导出像素按文档分辨率换算成点（原版 pageSize）。
  const pageSize = $derived.by(() => {
    const dpi = preview?.resolution && preview.resolution > 0 ? preview.resolution : 72;
    return inches(width, dpi) + " × " + inches(height, dpi) + " in @ " + Math.round(dpi) + " DPI";
  });

  const shownWidth = $derived(
    zoom === null ? "100%" : Math.round((preview?.previewWidth || width) * zoom) + "px",
  );

  async function doExport(): Promise<void> {
    exporting = true;
    error = null;
    try {
      if (await commitExport({ format, width, height, quality, background, sixteenBit, cmyk })) close();
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
  <div class="dialog" role="dialog" aria-modal="true" aria-label="导出为">
    <h2>导出为</h2>
    <div class="preview-area">
      {#if preview?.previewURL}
        <div class="preview-scroll" role="presentation" ondblclick={toggleFit}>
          <img
            src={preview.previewURL}
            alt="导出预览"
            class:checkerboard={def.keepsAlpha}
            style:width={shownWidth}
            style:max-width={zoom === null ? "100%" : "none"}
            style:image-rendering={zoom !== null && zoom >= 2 ? "pixelated" : "auto"}
          />
        </div>
        <div class="preview-meta">
          <span>
            {preview.targetWidth}×{preview.targetHeight}px{#if preview.previewWidth && preview.previewWidth !== preview.targetWidth}
              · 预览 {preview.previewWidth}×{preview.previewHeight}{/if}{#if preview.size}
              · 约 {formatBytes(preview.size)}{/if}
          </span>
          <span class="zoom">
            <button type="button" disabled={zoom === null} onclick={() => (zoom = null)}>适应</button>
            <button type="button" onclick={() => stepZoom(-1)}>−</button>
            {zoom === null ? "适应" : Math.round(zoom * 100) + "%"}
            <button type="button" onclick={() => stepZoom(1)}>＋</button>
          </span>
        </div>
      {:else}
        <div class="placeholder">{ready ? "正在生成预览…" : "正在压平画布…"}</div>
      {/if}
    </div>

    <div class="row">
      <span class="label">格式</span>
      <div class="segmented" role="group" aria-label="格式">
        {#each PRIMARY_EXPORT_FORMATS as kind}
          <button
            type="button"
            aria-pressed={format === kind}
            class:active={format === kind}
            onclick={() => (format = kind)}
          >
            {exportFormatDef(kind).label}
          </button>
        {/each}
      </div>
      <div class="more" role="group" aria-label="更多格式">
        <span class="more-label">更多格式</span>
        {#each MORE_EXPORT_FORMATS as kind}
          <button
            type="button"
            aria-pressed={format === kind}
            class:active={format === kind}
            onclick={() => (format = kind)}
          >
            {exportFormatDef(kind).label}
          </button>
        {/each}
      </div>
    </div>

    <div class="row">
      <span class="label">尺寸</span>
      <input
        class="size"
        type="number"
        min="1"
        max={EXPORT_MAX_SIDE}
        value={width}
        oninput={setWidth}
        aria-label="宽度（像素）"
      />
      <span class="times">×</span>
      <input
        class="size"
        type="number"
        min="1"
        max={EXPORT_MAX_SIDE}
        value={height}
        oninput={setHeight}
        aria-label="高度（像素）"
      />
      <span class="unit">px</span>
      <select class="scale" value="" onchange={applyScale} aria-label="缩放百分比">
        <option value="" disabled>缩放…</option>
        {#each [25, 50, 75, 100, 200] as percent}
          <option value={percent}>{percent}%</option>
        {/each}
      </select>
      <span class="percent">{scalePercent}%</span>
    </div>

    {#if format === "jpeg"}
      <label class="row">
        <span class="label">质量</span>
        <input class="quality" type="range" min="1" max="100" bind:value={quality} />
        <span class="percent">{quality}%</span>
      </label>
    {/if}

    {#if format === "png" || format === "tiff"}
      <label class="row">
        <span class="label">16 位</span>
        <input type="checkbox" bind:checked={sixteenBit} />
        <span class="hint">每通道 16 位，文件约大一倍（需要 16 位图层且画布上只有它）</span>
      </label>
    {/if}

    {#if format === "jpeg" || format === "tiff" || format === "pdf"}
      <label class="row">
        <span class="label">CMYK</span>
        <input type="checkbox" bind:checked={cmyk} />
        <span class="hint">按「视图 › CMYK › 配置」那份 ICC 写出印刷油墨（JPEG 走 Adobe 约定）</span>
      </label>
    {/if}

    {#if flattened}
      <label class="row">
        <span class="label">背景</span>
        <input class="color" type="color" bind:value={background} />
        <span class="hint">透明区域填充色（{def.label} 无透明通道）</span>
      </label>
    {/if}

    {#if format === "pdf"}
      <div class="row">
        <span class="label">页面</span>
        <span class="hint">{pageSize}</span>
      </div>
    {/if}

    {#if error}
      <p class="error">{error}</p>
    {/if}

    <div class="actions">
      <span class="status">{def.keepsAlpha ? "保留透明度 · sRGB" : "sRGB"}</span>
      <button type="button" onclick={close}>取消</button>
      <button
        type="button"
        class="primary"
        disabled={exporting || !ready || !preview?.previewURL}
        onclick={() => void doExport()}
      >
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
    max-height: 86vh;
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
    cursor: zoom-in;
  }

  .preview-scroll img {
    display: block;
    margin: 0 auto;
  }

  /* 原版 JPEGPreview.checkerboard：10pt 方格，透明像素下透出。 */
  .checkerboard {
    background-color: #4d4d4d;
    background-image:
      linear-gradient(45deg, #595959 25%, transparent 25%, transparent 75%, #595959 75%),
      linear-gradient(45deg, #595959 25%, transparent 25%, transparent 75%, #595959 75%);
    background-size: 20px 20px;
    background-position: 0 0, 10px 10px;
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

  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 10px;
    font-size: 13px;
  }

  .label {
    width: 56px;
    color: var(--text-dim);
  }

  .segmented,
  .more {
    display: flex;
    align-items: center;
    gap: 4px;
  }

  .more-label {
    color: var(--text-dim);
    font-size: 12px;
  }

  .segmented button,
  .more button {
    padding: 4px 10px;
  }

  .active {
    background: var(--accent, #3b82f6);
    border-color: var(--accent, #3b82f6);
    color: #fff;
  }

  .size {
    width: 76px;
    text-align: right;
  }

  .times,
  .unit,
  .percent,
  .hint {
    color: var(--text-dim);
  }

  .scale {
    margin-left: auto;
  }

  .quality {
    flex: 1;
  }

  .color {
    width: 42px;
    height: 24px;
    padding: 0;
    border: 1px solid var(--border);
    background: none;
  }

  .error {
    margin: 0 0 10px;
    color: var(--danger);
    font-size: 12px;
  }

  .actions {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 8px;
  }

  .status {
    margin-right: auto;
    color: var(--text-dim);
    font-size: 12px;
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
