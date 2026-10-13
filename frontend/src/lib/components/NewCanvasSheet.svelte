<script lang="ts">
  import { newDocument } from "../state/workspace";
  import {
    NEW_CANVAS_MAX_SIDE,
    backgroundById,
    nextResolution,
    pixelPreview,
    unitById,
    type NewCanvasBackgroundId,
    type NewCanvasUnitId,
  } from "../state/newCanvas";

  let {
    open,
    onclose,
  }: {
    open: boolean;
    onclose: () => void;
  } = $props();

  // 表单状态全在组件内部（票 57）：宽/高按当前单位存字符串——打印单位允许小数
  // 与本地化逗号；单位、DPI、底色三颗 pill 点击循环。默认仍是像素、72 DPI、
  // 透明、1920 × 1080（原版 NewCanvasSheet 的初始值）。
  let width = $state("1920");
  let height = $state("1080");
  let unitId = $state<NewCanvasUnitId>("px");
  let resolution = $state(72);
  let backgroundId = $state<NewCanvasBackgroundId>("transparent");
  let error = $state("");
  let submitting = $state(false);

  const unit = $derived(unitById(unitId));
  const background = $derived(backgroundById(backgroundId));
  const pixelWidth = $derived(unit.pixels(width, resolution));
  const pixelHeight = $derived(unit.pixels(height, resolution));
  const valid = $derived(pixelWidth !== null && pixelHeight !== null);
  // 打印单位才报最终像素尺寸（原版 resolutionHelp 只在非像素单位给出尺寸）。
  const preview = $derived(
    unitId !== "px" && pixelWidth !== null && pixelHeight !== null
      ? pixelPreview(pixelWidth, pixelHeight, resolution)
      : "",
  );

  /** 换单位写的是同一张画布：先按新单位把像素尺寸写回输入框（原版 switchUnit）。 */
  function switchUnit(next: NewCanvasUnitId) {
    if (pixelWidth !== null && pixelHeight !== null) {
      const target = unitById(next);
      width = target.text(pixelWidth, resolution);
      height = target.text(pixelHeight, resolution);
    }
    unitId = next;
  }

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (submitting) return;
    const w = pixelWidth;
    const h = pixelHeight;
    if (w === null || h === null) return; // 非法输入：创建按钮已禁用，这里再兜一层
    submitting = true;
    error = "";
    try {
      // workspace.newDocument applies the tab snapshot and loads the new
      // tab's document state (layer panel + render).
      await newDocument(w, h, resolution, backgroundId);
      onclose();
    } catch (err) {
      error = String(err)
        .replace(/^Error:\s*/, "")
        .replace(/\s*$/, "");
    } finally {
      submitting = false;
    }
  }

  function onkeydown(event: KeyboardEvent) {
    if (open && event.key === "Escape") onclose();
  }
</script>

<svelte:window {onkeydown} />

{#if open}
  <div
    class="backdrop"
    role="presentation"
    onclick={(e) => {
      if (e.target === e.currentTarget) onclose();
    }}
  >
    <div class="sheet" role="dialog" aria-modal="true" aria-label="新建画布">
      <form onsubmit={submit}>
        <h2>新建画布</h2>

        <div class="dimensions">
          <label>
            宽度（{unit.id}）
            <input type="text" inputmode="decimal" bind:value={width} aria-label="宽度" />
          </label>
          <span class="times">×</span>
          <label>
            高度（{unit.id}）
            <input type="text" inputmode="decimal" bind:value={height} aria-label="高度" />
          </label>
        </div>

        <!-- 三颗设置 pill，点一下换下一个值（原版 CyclePill）。 -->
        <div class="pills">
          <button
            type="button"
            class="pill"
            title="透明、白底或黑底；点击切换"
            onclick={() => (backgroundId = background.next)}
          >
            {background.title}
          </button>
          <span class="dot">·</span>
          <button
            type="button"
            class="pill"
            title="像素、英寸、厘米或毫米；点击切换"
            onclick={() => switchUnit(unit.next)}
          >
            {unit.name}
          </button>
          <span class="dot">·</span>
          <button
            type="button"
            class="pill"
            title="屏幕用 72，印刷用 300；点击切换"
            onclick={() => (resolution = nextResolution(resolution))}
          >
            {resolution} DPI
          </button>
        </div>

        {#if preview}
          <p class="preview">{preview}</p>
        {/if}

        {#if !valid}
          <p class="hint" role="alert">
            {unitId === "px"
              ? `请输入 1 到 ${NEW_CANVAS_MAX_SIDE} 的整数像素。`
              : `请输入不超过 ${NEW_CANVAS_MAX_SIDE} 像素的尺寸。`}
          </p>
        {/if}

        {#if error}
          <p class="error" role="alert">{error}</p>
        {/if}

        <div class="actions">
          <button type="button" onclick={onclose}>取消</button>
          <button type="submit" disabled={submitting || !valid}>
            {submitting ? "创建中…" : "创建"}
          </button>
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
    z-index: 10;
  }

  .sheet {
    width: 320px;
    padding: 20px;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 8px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  h2 {
    margin: 0;
    font-size: 15px;
  }

  .dimensions {
    display: flex;
    align-items: flex-end;
    gap: 8px;
  }

  .dimensions label {
    flex: 1;
  }

  .times {
    color: var(--text-dim);
    padding-bottom: 8px;
  }

  label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    color: var(--text-dim);
    font-size: 12px;
  }

  input {
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text);
    padding: 6px 8px;
    font: inherit;
    width: 100%;
    box-sizing: border-box;
  }

  input:focus {
    outline: 1px solid var(--accent);
  }

  .pills {
    display: flex;
    align-items: center;
    gap: 4px;
    font-size: 12px;
    color: var(--text-dim);
  }

  .pill {
    padding: 2px 8px;
    border: 1px solid transparent;
    border-radius: 999px;
    background: none;
    color: var(--text-dim);
  }

  .pill:hover {
    background: var(--bg-raised);
    border-color: var(--border);
  }

  .dot {
    color: var(--text-dim);
    opacity: 0.6;
  }

  .preview {
    margin: 0;
    color: var(--text-dim);
    font-size: 12px;
  }

  .hint {
    margin: 0;
    color: var(--warning);
    font-size: 12px;
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
    padding: 6px 16px;
  }

  .actions button[type="submit"] {
    background: var(--accent-dim);
    border-color: var(--accent);
  }
</style>
