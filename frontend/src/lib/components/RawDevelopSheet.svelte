<script lang="ts">
  import {
    closeRawDevelop,
    finishRawDevelop,
    isAsShot,
    rawDevelop,
    resetRawDevelopSettings,
    updateRawDevelopPreview,
    type RawDevelopSettings,
  } from "../state/rawdevelop";

  /**
   * RAW develop sheet（票 41 / 64，RawDevelopSheet 语义）：曝光 −3…3 EV、
   * 色温 2000…12000 K、色调 ±150、增强 0…1、降噪 0…1（2 位小数）、镜头校正
   * 与高光恢复开关——范围与原版一致。预览是像素面上暂存的 JPEG（≤800px，
   * 原版 limit: 800），滑杆停下后重显影半尺寸帧；Reset 回 asShot*（已经是
   * asShot 时禁用）；导入按全尺寸重开并落层。supports* 为 false 的控件置灰，
   * 中文文案沿用仓库风格。
   */

  let settings = $state<RawDevelopSettings | null>(null);
  const sheet = $derived($rawDevelop);

  $effect(() => {
    if (sheet && !settings) settings = { ...sheet.asShot };
  });

  let debounce: ReturnType<typeof setTimeout> | null = null;
  $effect(() => {
    if (!settings || !sheet) return;
    void settings.exposure;
    void settings.temperature;
    void settings.tint;
    void settings.boost;
    void settings.noiseReduction;
    void settings.lensCorrection;
    void settings.highlightRecovery;
    if (debounce) clearTimeout(debounce);
    debounce = setTimeout(() => {
      void updateRawDevelopPreview(settings!);
    }, 250);
    return () => {
      if (debounce) clearTimeout(debounce);
    };
  });

  function reset(): void {
    if (settings) {
      settings = resetRawDevelopSettings(settings);
    }
  }

  function kelvinLabel(k: number): string {
    return k >= 1000 ? `${Math.round(k)}K` : `${Math.round(k)}`;
  }
</script>

{#if sheet && settings}
  <div
    class="backdrop"
    role="presentation"
    onclick={(e) => {
      if (e.target === e.currentTarget) closeRawDevelop();
    }}
  >
    <div class="sheet" role="dialog" aria-modal="true" aria-label="显影 RAW">
      <h2>显影「{sheet.name}」 <span class="dims">{sheet.fullWidth}×{sheet.fullHeight}</span></h2>
      <div class="preview">
        {#if sheet.jpegURL}
          <img src={sheet.jpegURL} alt="RAW 显影预览" />
        {:else}
          <div class="placeholder">正在解码 RAW…（大文件需要几秒）</div>
        {/if}
        {#if sheet.busy}
          <div class="spinner" aria-hidden="true"></div>
        {/if}
      </div>
      <div class="controls">
        <label>
          <span class="name">曝光</span>
          <input type="range" min="-3" max="3" step="0.01" bind:value={settings.exposure} />
          <span class="value">{settings.exposure >= 0 ? "+" : ""}{settings.exposure.toFixed(2)} 档</span>
        </label>
        <label>
          <span class="name">色温</span>
          <input type="range" min="2000" max="12000" step="10" bind:value={settings.temperature} />
          <span class="value">{kelvinLabel(settings.temperature)}</span>
        </label>
        <label>
          <span class="name">色调</span>
          <input type="range" min="-150" max="150" step="1" bind:value={settings.tint} />
          <span class="value">{settings.tint >= 0 ? "+" : ""}{Math.round(settings.tint)}</span>
        </label>
        <label>
          <span class="name">增强</span>
          <input type="range" min="0" max="1" step="0.01" bind:value={settings.boost} />
          <span class="value">{Math.round(settings.boost * 100)}%</span>
        </label>
        <label
          class:disabled={!settings.supportsNoiseReduction}
          title={settings.supportsNoiseReduction
            ? "平滑颗粒，代价是细微细节"
            : "此相机不支持该功能"}
        >
          <span class="name">降噪</span>
          <input
            type="range"
            min="0"
            max="1"
            step="0.01"
            disabled={!settings.supportsNoiseReduction}
            bind:value={settings.noiseReduction}
          />
          <span class="value">{settings.noiseReduction.toFixed(2)}</span>
        </label>
        <div class="toggles">
          <label
            class:disabled={!settings.supportsLensCorrection}
            title={settings.supportsLensCorrection
              ? "消除镜头的畸变与暗角"
              : "此文件没有镜头配置文件"}
          >
            <input
              type="checkbox"
              disabled={!settings.supportsLensCorrection}
              bind:checked={settings.lensCorrection}
            />
            <span>镜头校正</span>
          </label>
          <label
            class:disabled={!settings.supportsHighlightRecovery}
            title={settings.supportsHighlightRecovery
              ? "找回亮部被相机部分削掉细节的区域"
              : "此相机不支持该功能"}
          >
            <input
              type="checkbox"
              disabled={!settings.supportsHighlightRecovery}
              bind:checked={settings.highlightRecovery}
            />
            <span>高光恢复</span>
          </label>
        </div>
      </div>
      {#if sheet.error}
        <p class="error">{sheet.error}</p>
      {/if}
      <div class="actions">
        <button type="button" onclick={reset} disabled={sheet.busy || isAsShot(settings)}>重置</button>
        <span class="flex"></span>
        <button type="button" onclick={() => closeRawDevelop()}>取消</button>
        <button type="button" class="primary" disabled={sheet.busy} onclick={() => void finishRawDevelop(settings!)}>
          导入
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.55);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 20;
  }

  .sheet {
    width: 640px;
    max-height: 84vh;
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

  .dims {
    color: var(--text-dim);
    font-weight: 400;
    font-size: 12px;
  }

  .preview {
    position: relative;
    border: 1px solid var(--border);
    border-radius: 6px;
    background: var(--bg-canvas, #202020);
    margin-bottom: 12px;
    min-height: 200px;
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: hidden;
  }

  .preview img {
    max-width: 100%;
    max-height: 320px;
    display: block;
  }

  .placeholder {
    padding: 60px 20px;
    color: var(--text-dim);
    font-size: 13px;
  }

  .spinner {
    position: absolute;
    top: 10px;
    right: 10px;
    width: 16px;
    height: 16px;
    border: 2px solid var(--border);
    border-top-color: var(--accent, #3b82f6);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }

  .controls {
    display: flex;
    flex-direction: column;
    gap: 10px;
    margin-bottom: 12px;
    font-size: 13px;
  }

  .controls label {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  /* 原版的标签宽 110（1.4.10 从 90 加宽，容下 Noise Reduction）。 */
  .controls .name {
    width: 110px;
    flex: none;
  }

  .controls input[type="range"] {
    flex: 1;
  }

  .controls .value {
    width: 80px;
    flex: none;
    text-align: right;
    color: var(--text-dim);
    font-variant-numeric: tabular-nums;
  }

  .toggles {
    display: flex;
    gap: 20px;
    padding-left: 120px;
  }

  .toggles label {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .controls label.disabled {
    color: var(--text-dim);
    cursor: not-allowed;
  }

  .error {
    margin: 0 0 10px;
    color: var(--danger);
    font-size: 12px;
  }

  .actions {
    display: flex;
    gap: 8px;
    align-items: center;
  }

  .flex {
    flex: 1;
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
