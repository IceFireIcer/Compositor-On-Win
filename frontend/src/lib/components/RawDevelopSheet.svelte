<script lang="ts">
  import {
    closeRawDevelop,
    finishRawDevelop,
    rawDevelop,
    updateRawDevelopPreview,
    type RawDevelopSettings,
  } from "../state/rawdevelop";

  /**
   * RAW develop sheet (ticket 41 / RawDevelopSheet semantics): 曝光（档）、
   * 色温（K）、色调（绿–品红）、增强（0–1）。滑杆停下后重显影半尺寸帧；
   * Reset 回 asShot；导入按全尺寸重开并落层。
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
    if (debounce) clearTimeout(debounce);
    debounce = setTimeout(() => {
      void updateRawDevelopPreview(settings!);
    }, 250);
    return () => {
      if (debounce) clearTimeout(debounce);
    };
  });

  function reset(): void {
    if (sheet) {
      settings = { ...sheet.asShot };
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
          <span>曝光 {settings.exposure >= 0 ? "+" : ""}{settings.exposure.toFixed(2)} 档</span>
          <input type="range" min="-5" max="5" step="0.05" bind:value={settings.exposure} />
        </label>
        <label>
          <span>色温 {kelvinLabel(settings.temperature)}</span>
          <input type="range" min="2000" max="50000" step="50" bind:value={settings.temperature} />
        </label>
        <label>
          <span>色调 {settings.tint >= 0 ? "+" : ""}{Math.round(settings.tint)}</span>
          <input type="range" min="-100" max="100" step="1" bind:value={settings.tint} />
        </label>
        <label>
          <span>增强 {Math.round(settings.boost * 100)}%</span>
          <input type="range" min="0" max="1" step="0.01" bind:value={settings.boost} />
        </label>
      </div>
      {#if sheet.error}
        <p class="error">{sheet.error}</p>
      {/if}
      <div class="actions">
        <button type="button" onclick={reset} disabled={sheet.busy}>Reset</button>
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
