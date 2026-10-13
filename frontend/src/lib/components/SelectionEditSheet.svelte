<script lang="ts">
  import { onMount } from "svelte";
  import { registerCanvasModalGuard } from "../shortcuts/dispatch";
  import {
    applySelectionAmount, applySelectionTransform, cancelSelectionEdit, commitColorRange,
    previewColorRange, sampleColorRange, selectionEdit, type SampleMode,
  } from "../state/selectionEdit";

  const titles = { expand: "扩展选区", contract: "收缩选区", feather: "羽化选区", transform: "变换选区", colorRange: "色彩范围" };
  const edit = $derived($selectionEdit);
  let dialog: HTMLDialogElement;
  let amount = $state(5);
  let dx = $state(0), dy = $state(0), scaleX = $state(100), scaleY = $state(100), angle = $state(0);
  let fuzziness = $state(40), invert = $state(false);
  let mode = $state<SampleMode>("replace");
  let busy = $state(false), error = $state("");
  let previewCanvas = $state<HTMLCanvasElement>();

  onMount(() => {
    dialog.showModal();
    const removeGuard = registerCanvasModalGuard(() => true);
    return removeGuard;
  });

  $effect(() => {
    const canvas = previewCanvas, selection = edit?.preview;
    if (!canvas || !selection) return;
    const scale = Math.min(292 / selection.width, 200 / selection.height);
    const w = Math.max(1, Math.round(selection.width * scale)), h = Math.max(1, Math.round(selection.height * scale));
    canvas.width = w;
    canvas.height = h;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const image = ctx.createImageData(w, h);
    for (let y = 0; y < h; y++) for (let x = 0; x < w; x++) {
      const value = selection.mask[Math.min(selection.height - 1, Math.floor(y / scale)) * selection.width + Math.min(selection.width - 1, Math.floor(x / scale))];
      const i = (y * w + x) * 4;
      image.data[i] = image.data[i + 1] = image.data[i + 2] = value;
      image.data[i + 3] = 255;
    }
    ctx.putImageData(image, 0, 0);
  });

  function close(): void { if (!busy) cancelSelectionEdit(); }
  async function work(fn: () => void | Promise<unknown>): Promise<void> {
    if (busy) return;
    busy = true;
    error = "";
    try { await fn(); }
    catch (err) { error = err instanceof Error ? err.message : String(err); }
    finally { busy = false; }
  }

  function submit(event: SubmitEvent): void {
    event.preventDefault();
    void work(async () => {
      if (edit?.kind === "colorRange") {
        if (await previewColorRange(fuzziness, invert)) commitColorRange();
      } else if (edit?.kind === "transform") applySelectionTransform(dx, dy, scaleX, scaleY, angle);
      else applySelectionAmount(amount);
    });
  }

  function sample(event: MouseEvent): void {
    if (!edit || busy) return;
    const image = event.currentTarget as HTMLButtonElement;
    const rect = image.getBoundingClientRect();
    const x = Math.min(edit.width - 1, Math.max(0, Math.floor((event.clientX - rect.left) / rect.width * edit.width)));
    const y = Math.min(edit.height - 1, Math.max(0, Math.floor((event.clientY - rect.top) / rect.height * edit.height)));
    void work(() => sampleColorRange(x, y, event.altKey ? "subtract" : event.shiftKey ? "add" : mode));
  }

  let sampleX = $state(0), sampleY = $state(0);
</script>

<dialog bind:this={dialog} oncancel={(event) => { event.preventDefault(); close(); }} aria-labelledby="selection-edit-title">
  {#if edit}
    <form onsubmit={submit}>
      <h2 id="selection-edit-title">{titles[edit.kind]}</h2>
      {#if edit.kind === "colorRange"}
        <p class="hint">点击图像取样。Shift 添加颜色，Alt 排除颜色。白色区域将被选中。</p>
        <div class="previews">
          <button type="button" class="sample-image" aria-label="点击图像取样颜色" onclick={sample} disabled={busy}>
            <img src="/render/{encodeURIComponent(edit.tabId)}.png?v={edit.rev}" alt="当前画布的颜色取样图" draggable="false" />
          </button>
          <canvas bind:this={previewCanvas} aria-label="选区黑白预览"></canvas>
        </div>
        <label>取样方式
          <select bind:value={mode} disabled={busy}>
            <option value="replace">替换颜色</option><option value="add">添加颜色</option><option value="subtract">排除颜色</option>
          </select>
        </label>
        <div class="row">
          <label>取样 X<input type="number" min="0" max={edit.width - 1} step="1" bind:value={sampleX} disabled={busy} /></label>
          <label>取样 Y<input type="number" min="0" max={edit.height - 1} step="1" bind:value={sampleY} disabled={busy} /></label>
          <button type="button" disabled={busy} onclick={() => void work(() => sampleColorRange(sampleX, sampleY, mode))}>取样</button>
        </div>
        <div class="samples" aria-live="polite">
          <span>包含：{edit.include.length} 色</span><span>排除：{edit.exclude.length} 色</span>
          {#each edit.include as rgb, i (`include-${i}`)}<span class="swatch" title="包含 RGB {rgb.join(', ')}" style:background={`rgb(${rgb.join(',')})`}></span>{/each}
          {#each edit.exclude as rgb, i (`exclude-${i}`)}<span class="swatch excluded" title="排除 RGB {rgb.join(', ')}" style:background={`rgb(${rgb.join(',')})`}></span>{/each}
        </div>
        <label>颜色容差<input type="number" min="0" max="200" step="1" bind:value={fuzziness} disabled={busy} required onchange={() => void work(() => previewColorRange(fuzziness, invert))} /></label>
        <label class="check"><input type="checkbox" bind:checked={invert} disabled={busy} onchange={() => void work(() => previewColorRange(fuzziness, invert))} />反向选区</label>
      {:else if edit.kind === "transform"}
        <div class="row">
          <label>水平位移（px）<input type="number" bind:value={dx} required /></label>
          <label>垂直位移（px）<input type="number" bind:value={dy} required /></label>
        </div>
        <div class="row">
          <label>水平缩放（%）<input type="number" min="0.01" step="any" bind:value={scaleX} required /></label>
          <label>垂直缩放（%）<input type="number" min="0.01" step="any" bind:value={scaleY} required /></label>
        </div>
        <label>旋转角度（°）<input type="number" step="any" bind:value={angle} required /></label>
        <p class="hint">绕选区中心缩放和旋转，仅改变选区边界。</p>
      {:else}
        <label>半径（px）<input type="number" min="1" max={edit.kind === "feather" ? 250 : 500} step="1" bind:value={amount} required /></label>
      {/if}
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <div class="actions">
        <span role="status">{busy ? "处理中…" : ""}</span>
        <button type="button" onclick={close} disabled={busy}>取消</button>
        <button class="primary" type="submit" disabled={busy || (edit.kind === "colorRange" && edit.include.length === 0)}>确定</button>
      </div>
    </form>
  {/if}
</dialog>

<style>
  dialog { width: min(650px, calc(100vw - 48px)); max-height: calc(100vh - 48px); overflow: auto; margin: auto; padding: 24px; background: var(--bg-panel); color: var(--text); border: 1px solid var(--border); border-radius: 8px; }
  dialog::backdrop { background: rgba(0, 0, 0, 0.6); }
  form { display: grid; gap: 16px; }
  h2 { margin: 0; font-size: 18px; }
  label { display: flex; gap: 8px; align-items: center; }
  input[type="number"] { width: 96px; }
  .row { display: flex; gap: 16px; align-items: center; flex-wrap: wrap; }
  .hint { margin: 0; color: var(--text-dim); }
  .previews { display: flex; gap: 12px; align-items: center; justify-content: center; min-height: 100px; }
  .sample-image { padding: 0; border: 0; cursor: crosshair; line-height: 0; }
  .sample-image img, canvas { max-width: 292px; max-height: 200px; display: block; object-fit: contain; }
  .samples { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
  .swatch { width: 18px; height: 18px; border: 1px solid var(--border); }
  .excluded { border-style: dashed; }
  .actions { display: flex; gap: 8px; align-items: center; justify-content: flex-end; }
  .actions span { margin-right: auto; }
  .error { color: var(--error, #ff9494); margin: 0; }
  :focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; }
</style>
