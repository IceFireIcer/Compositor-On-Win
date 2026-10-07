<script lang="ts">
  import {
    activeSize,
    canvasSize,
    geometrySheet,
    imageSize,
    trim,
  } from "../state/geometry";

  /**
   * 文档几何表单（票 43）：画布大小（九宫格锚点 + 可选扩展色）、图像大小
   * （单位/重采样质量）、修剪（依据 + 四边开关 + 容差）。计算在 Go 侧，
   * 表单只收参数。
   */

  let kind = $derived($geometrySheet);
  const initial = activeSize();

  // Canvas size state.
  let cw = $state(initial.width);
  let ch = $state(initial.height);
  let anchor = $state(4);
  let fillEnabled = $state(false);
  let fill = $state("#ffffff");

  // Image size state.
  let iw = $state(initial.width);
  let ih = $state(initial.height);
  let resolution = $state(initial.resolution);
  let sampling = $state<"Nearest" | "Smooth" | "High quality">("High quality");

  // Trim state.
  let basedOn = $state<"transparent" | "topLeft" | "bottomRight">("transparent");
  let top = $state(true);
  let bottom = $state(true);
  let left = $state(true);
  let right = $state(true);
  let tolerance = $state(0);

  let busy = $state(false);
  let error = $state("");

  function close(): void {
    geometrySheet.set(null);
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (busy) return;
    busy = true;
    error = "";
    try {
      if (kind === "canvasSize") {
        await canvasSize({ width: cw, height: ch, anchor, fill: fillEnabled ? fill : "" });
      } else if (kind === "imageSize") {
        await imageSize({ width: iw, height: ih, resolution, sampling });
      } else {
        await trim({ basedOn, top, bottom, left, right, tolerance });
      }
      close();
    } catch (err) {
      error = String(err).replace(/^Error:\s*/, "").trim();
    } finally {
      busy = false;
    }
  }

  function onkeydown(event: KeyboardEvent): void {
    if (kind && event.key === "Escape") close();
  }

  // Aspect lock for image size (keeps the ratio when either side changes).
  let locked = $state(true);
  function setWidth(v: number): void {
    iw = v;
    if (locked && initial.width > 0) ih = Math.max(1, Math.round((v * initial.height) / initial.width));
  }
  function setHeight(v: number): void {
    ih = v;
    if (locked && initial.height > 0) iw = Math.max(1, Math.round((v * initial.width) / initial.height));
  }

  const anchorLabels = [
    "左上", "上", "右上",
    "左", "居中", "右",
    "左下", "下", "右下",
  ];
</script>

<svelte:window {onkeydown} />

{#if kind}
  <div
    class="backdrop"
    role="presentation"
    onclick={(e) => {
      if (e.target === e.currentTarget) close();
    }}
  >
    <div class="sheet" role="dialog" aria-modal="true">
      <form onsubmit={submit}>
        {#if kind === "canvasSize"}
          <h2>画布大小</h2>
          <div class="row">
            <label>宽度（px）<input type="number" min="1" max="30000" bind:value={cw} required /></label>
            <label>高度（px）<input type="number" min="1" max="30000" bind:value={ch} required /></label>
          </div>
          <fieldset class="anchors">
            <legend>锚点</legend>
            {#each anchorLabels as label, i (label)}
              <button
                type="button"
                class:active={anchor === i}
                title={label}
                aria-label={label}
                onclick={() => (anchor = i)}></button>
            {/each}
          </fieldset>
          <div class="row">
            <label class="check">
              <input type="checkbox" bind:checked={fillEnabled} />
              扩展画布时填充
            </label>
            <input class="color" type="color" bind:value={fill} disabled={!fillEnabled} aria-label="扩展色" />
          </div>
        {:else if kind === "imageSize"}
          <h2>图像大小</h2>
          <div class="row">
            <label>宽度（px）<input type="number" min="1" max="30000" value={iw} oninput={(e) => setWidth(Number((e.target as HTMLInputElement).value))} required /></label>
            <label>高度（px）<input type="number" min="1" max="30000" value={ih} oninput={(e) => setHeight(Number((e.target as HTMLInputElement).value))} required /></label>
          </div>
          <div class="row">
            <label class="check"><input type="checkbox" bind:checked={locked} /> 保持长宽比</label>
            <label>分辨率（ppi）<input type="number" min="1" max="9600" bind:value={resolution} required /></label>
          </div>
          <label>
            重采样
            <select bind:value={sampling}>
              <option value="High quality">高质量（2×2 超采样）</option>
              <option value="Smooth">平滑（双线性）</option>
              <option value="Nearest">最近邻（硬边缘）</option>
            </select>
          </label>
        {:else}
          <h2>修剪</h2>
          <label>
            依据
            <select bind:value={basedOn}>
              <option value="transparent">透明像素</option>
              <option value="topLeft">左上角像素颜色</option>
              <option value="bottomRight">右下角像素颜色</option>
            </select>
          </label>
          <fieldset class="edges">
            <legend>修剪边</legend>
            <label class="check"><input type="checkbox" bind:checked={top} /> 上</label>
            <label class="check"><input type="checkbox" bind:checked={bottom} /> 下</label>
            <label class="check"><input type="checkbox" bind:checked={left} /> 左</label>
            <label class="check"><input type="checkbox" bind:checked={right} /> 右</label>
          </fieldset>
          <label>
            容差（0–255）
            <input type="number" min="0" max="255" bind:value={tolerance} />
          </label>
        {/if}

        {#if error}
          <p class="error" role="alert">{error}</p>
        {/if}

        <div class="actions">
          <button type="button" onclick={close}>取消</button>
          <button type="submit" disabled={busy}>{busy ? "处理中…" : "确定"}</button>
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
    z-index: 20;
  }

  .sheet {
    width: 400px;
    padding: 18px;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 8px;
  }

  h2 {
    margin: 0 0 12px;
    font-size: 14px;
  }

  form {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .row {
    display: flex;
    gap: 12px;
    align-items: flex-end;
  }

  label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex: 1;
    font-size: 13px;
  }

  label.check {
    flex-direction: row;
    align-items: center;
    gap: 6px;
  }

  input[type="number"],
  select {
    padding: 4px 6px;
  }

  .color {
    width: 42px;
    height: 26px;
    padding: 0;
    border: 1px solid var(--border);
    background: none;
  }

  .anchors {
    display: grid;
    grid-template-columns: repeat(3, 32px);
    gap: 4px;
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 8px;
    margin: 0;
  }

  .anchors legend,
  .edges legend {
    font-size: 12px;
    color: var(--text-dim);
  }

  .anchors button {
    width: 32px;
    height: 24px;
    padding: 0;
    border: 1px solid var(--border);
    border-radius: 3px;
    background: var(--bg-panel);
  }

  .anchors button.active {
    background: var(--accent, #3b82f6);
    border-color: var(--accent, #3b82f6);
  }

  .edges {
    display: flex;
    gap: 12px;
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 8px;
    margin: 0;
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
    padding: 6px 14px;
  }
</style>
