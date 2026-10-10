<script lang="ts">
  import { updateSheet, updateState, downloadUpdate, applyUpdate, checkForUpdates } from "../state/update";

  /**
   * "检查更新"表单（票 50 / C09）：显示当前版本、可用版本与发行说明，并把
   * 下载（含进度）与安装两步交给 Go。状态驱动的文案，不做网络请求的 try/catch。
   */

  const state = $derived($updateState);

  function close(): void {
    updateSheet.set(false);
  }

  function percent(): string {
    const p = state.progress;
    return p === undefined ? "" : `${Math.round(p * 100)}%`;
  }

  function sizeText(bytes?: number): string {
    if (!bytes || bytes <= 0) return "";
    const mb = bytes / (1024 * 1024);
    return mb >= 1 ? `${mb.toFixed(1)} MB` : `${Math.round(bytes / 1024)} KB`;
  }

  function onkeydown(event: KeyboardEvent): void {
    if (!$updateSheet) return;
    if (event.key === "Escape" && state.status !== "downloading" && state.status !== "applying") {
      close();
    }
  }
</script>

<svelte:window {onkeydown} />

{#if $updateSheet}
  <div
    class="backdrop"
    role="presentation"
    onclick={(e) => {
      if (e.target === e.currentTarget && state.status !== "downloading" && state.status !== "applying") close();
    }}
  >
    <div class="dialog" role="dialog" aria-modal="true" aria-label="检查更新">
      <h2>检查更新</h2>

      {#if state.status === "checking"}
        <p>正在检查更新…</p>
      {:else if state.status === "up-to-date"}
        <p>已是最新版本（{state.currentVersion}）。</p>
      {:else if state.status === "unconfigured"}
        <p>这个构建没有配置更新源。</p>
        {#if state.error}<p class="dim">{state.error}</p>{/if}
      {:else if state.status === "available"}
        <p>
          发现新版本 <strong>{state.version}</strong>（当前 {state.currentVersion}）
          {#if state.size}<span class="dim">· 安装包 {sizeText(state.size)}</span>{/if}
        </p>
        {#if state.notes}
          <pre class="notes">{state.notes}</pre>
        {/if}
      {:else if state.status === "downloading"}
        <p>正在下载并校验安装包…</p>
        <div
          class="bar"
          role="progressbar"
          aria-label="更新下载进度"
          aria-valuemin="0"
          aria-valuemax="100"
          aria-valuenow={state.progress === undefined ? undefined : Math.round(state.progress * 100)}
        >
          <div class="fill" style:width="{state.progress === undefined ? 100 : state.progress * 100}%"></div>
        </div>
        <p class="dim">{percent()}</p>
      {:else if state.status === "ready"}
        <p>安装包已下载并通过签名校验，可以安装了。</p>
        <p class="dim">安装时应用会退出，安装完成后请重新打开。</p>
      {:else if state.status === "applying"}
        <p>正在启动安装程序…</p>
      {:else if state.status === "error"}
        <p>检查更新失败。</p>
        {#if state.error}<pre class="notes">{state.error}</pre>{/if}
      {:else}
        <p>还没有检查过更新。</p>
      {/if}

      <div class="actions">
        {#if state.status === "available"}
          <button type="button" onclick={close}>稍后</button>
          <button type="button" class="primary" onclick={() => void downloadUpdate()}>下载并安装</button>
        {:else if state.status === "ready"}
          <button type="button" onclick={close}>稍后</button>
          <button type="button" class="primary" onclick={() => void applyUpdate()}>立即安装并重启</button>
        {:else if state.status === "error" || state.status === "up-to-date" || state.status === "unconfigured"}
          <button type="button" onclick={() => void checkForUpdates()}>重试</button>
          <button type="button" class="primary" onclick={close}>关闭</button>
        {:else}
          <button type="button" onclick={close} disabled={state.status === "downloading" || state.status === "applying"}>
            关闭
          </button>
        {/if}
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
  }

  .dim {
    color: var(--text-dim);
    font-size: 12px;
  }

  .notes {
    margin: 0 0 12px;
    padding: 8px;
    max-height: 160px;
    overflow: auto;
    background: var(--bg-canvas);
    border: 1px solid var(--border);
    border-radius: 4px;
    font-size: 12px;
    white-space: pre-wrap;
  }

  .bar {
    height: 6px;
    margin: 6px 0;
    background: var(--bg-canvas);
    border-radius: 3px;
    overflow: hidden;
  }

  .fill {
    height: 100%;
    background: var(--accent, #3b82f6);
    transition: width 120ms linear;
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
