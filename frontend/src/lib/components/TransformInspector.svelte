<script lang="ts">
  import {
    applyTransformNumeric,
    cancelTransformSession,
    commitTransformSession,
    flipTransform,
    transformCommitPending,
    transformDraft,
    transformError,
    transformSession,
  } from "../state/transform";
  import { SAMPLINGS, type NumericInput, type Sampling } from "../state/transformSession";
  // 票 54/V06：拖标签 scrub、方向键步进、输入钳制。语义在 numeric.ts（移植自
  // NumericScrub.swift 与 ArrowStepper），这里只做接线。
  import { arrows, direct, scrub, type ValueRange } from "./numeric";

  type NumericKey = "x" | "y" | "w" | "h" | "rotation";

  // 每个字段的范围与每像素增量。Swift 里 px 字段的 sensitivity 是 1（BrushControls.swift:39），
  // 角度同样是 1。拖拽按整数吸附（step 1），输入不吸附。
  const FIELD_LIMITS: Record<NumericKey, ValueRange & { step: number; sensitivity: number }> = {
    x: { min: -30000, max: 30000, step: 1, sensitivity: 1 },
    y: { min: -30000, max: 30000, step: 1, sensitivity: 1 },
    w: { min: 1, max: 30000, step: 1, sensitivity: 1 },
    h: { min: 1, max: 30000, step: 1, sensitivity: 1 },
    rotation: { min: -360, max: 360, step: 1, sensitivity: 1 },
  };

  const draft = $derived($transformDraft);
  const pending = $derived($transformCommitPending);
  const error = $derived($transformError);

  function numericValue(key: NumericKey): number | undefined {
    if (!draft) return undefined;
    switch (key) {
      case "x": return draft.origin[0];
      case "y": return draft.origin[1];
      case "w": return draft.size[0];
      case "h": return draft.size[1];
      case "rotation": return draft.rotation;
    }
  }

  function applyNumericField(key: NumericKey, event: Event): boolean {
    if (!(event.currentTarget instanceof HTMLInputElement)) return false;
    const limits = FIELD_LIMITS[key];
    // 输入走 direct()：解析并钳制，解析不出来就保留旧值（Swift 的 commit-on-submit 语义）。
    const value = direct(event.currentTarget.value, { min: limits.min, max: limits.max });
    if (value === null) return false;
    const input: NumericInput = { [key]: value } as NumericInput;
    return applyTransformNumeric(input);
  }

  // 标签拖拽：按下时记住起点值，每帧算 起点 + 位移 × 增量，吸附并钳制。
  let scrubGesture: { key: NumericKey; startX: number; startValue: number } | null = null;

  function beginScrub(event: PointerEvent, key: NumericKey): void {
    if (pending || event.button !== 0) return;
    const value = numericValue(key);
    if (value === undefined) return;
    scrubGesture = { key, startX: event.clientX, startValue: value };
    (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
    event.preventDefault();
  }

  function moveScrub(event: PointerEvent): void {
    const gesture = scrubGesture;
    if (!gesture || pending) return;
    const limits = FIELD_LIMITS[gesture.key];
    const next = scrub(event.clientX - gesture.startX, gesture.startValue, {
      sensitivity: limits.sensitivity,
      step: limits.step,
      range: { min: limits.min, max: limits.max },
    });
    applyTransformNumeric({ [gesture.key]: next } as NumericInput);
  }

  function endScrub(): void {
    scrubGesture = null;
  }

  function setSampling(event: Event): void {
    const value = (event.currentTarget as HTMLSelectElement).value as Sampling;
    if (SAMPLINGS.includes(value)) applyTransformNumeric({ sampling: value });
  }

  function onKeydown(event: KeyboardEvent, numericKey?: NumericKey): void {
    // 方向键步进（ArrowStepper 只认上下）：↑ 加一步、↓ 减一步，Shift 把步长乘十。
    if ((event.key === "ArrowUp" || event.key === "ArrowDown") && numericKey !== undefined && !pending) {
      const current = numericValue(numericKey);
      if (current !== undefined) {
        const limits = FIELD_LIMITS[numericKey];
        event.preventDefault();
        const next = arrows(event.key === "ArrowUp" ? "up" : "down", current, {
          step: limits.step,
          shift: event.shiftKey,
          range: { min: limits.min, max: limits.max },
        });
        applyTransformNumeric({ [numericKey]: next } as NumericInput);
        return;
      }
    }
    if (event.key === "Escape" && !pending) {
      event.preventDefault();
      cancelTransformSession();
    } else if (
      event.key === "Enter" &&
      !pending &&
      numericKey !== undefined &&
      event.currentTarget instanceof HTMLInputElement
    ) {
      event.preventDefault();
      // `change` fires after Enter in some browsers. Apply the visible field
      // first so the request cannot serialize the previous number.
      if (applyNumericField(numericKey, event)) void apply();
    }
  }

  async function apply(): Promise<void> {
    if (pending) return;
    await commitTransformSession();
  }
</script>

{#if $transformSession && draft}
  <section
    class="inspector"
    aria-label="Transform inspector"
  >
    <div class="header">
      <h2>Transform</h2>
      <span class="hint">Non-destructive</span>
    </div>

    <div class="fields">
      <label for="transform-x" class="scrub" onpointerdown={(e) => beginScrub(e, "x")} onpointermove={moveScrub} onpointerup={endScrub} onpointercancel={endScrub}>X</label>
      <input
        id="transform-x"
        aria-label="X"
        type="number"
        step="any"
        min="-30000"
        max="30000"
        value={numericValue("x")}
        disabled={pending}
        onchange={(event) => applyNumericField("x", event)}
        onkeydown={(event) => onKeydown(event, "x")}
      />

      <label for="transform-y" class="scrub" onpointerdown={(e) => beginScrub(e, "y")} onpointermove={moveScrub} onpointerup={endScrub} onpointercancel={endScrub}>Y</label>
      <input
        id="transform-y"
        aria-label="Y"
        type="number"
        step="any"
        min="-30000"
        max="30000"
        value={numericValue("y")}
        disabled={pending}
        onchange={(event) => applyNumericField("y", event)}
        onkeydown={(event) => onKeydown(event, "y")}
      />

      <label for="transform-width" class="scrub" onpointerdown={(e) => beginScrub(e, "w")} onpointermove={moveScrub} onpointerup={endScrub} onpointercancel={endScrub}>W</label>
      <input
        id="transform-width"
        aria-label="W"
        type="number"
        step="any"
        min="1"
        max="30000"
        value={numericValue("w")}
        disabled={pending}
        onchange={(event) => applyNumericField("w", event)}
        onkeydown={(event) => onKeydown(event, "w")}
      />

      <label for="transform-height" class="scrub" onpointerdown={(e) => beginScrub(e, "h")} onpointermove={moveScrub} onpointerup={endScrub} onpointercancel={endScrub}>H</label>
      <input
        id="transform-height"
        aria-label="H"
        type="number"
        step="any"
        min="1"
        max="30000"
        value={numericValue("h")}
        disabled={pending}
        onchange={(event) => applyNumericField("h", event)}
        onkeydown={(event) => onKeydown(event, "h")}
      />

      <label for="transform-rotation" class="scrub" onpointerdown={(e) => beginScrub(e, "rotation")} onpointermove={moveScrub} onpointerup={endScrub} onpointercancel={endScrub}>Rotation</label>
      <input
        id="transform-rotation"
        aria-label="Rotation"
        type="number"
        step="any"
        min="-360"
        max="360"
        value={numericValue("rotation")}
        disabled={pending}
        onchange={(event) => applyNumericField("rotation", event)}
        onkeydown={(event) => onKeydown(event, "rotation")}
      />

      <label for="transform-sampling">Sampling</label>
      <select
        id="transform-sampling"
        aria-label="Sampling"
        value={draft.sampling}
        disabled={pending}
        onchange={setSampling}
        onkeydown={onKeydown}
      >
        {#each SAMPLINGS as sampling}
          <option value={sampling}>{sampling}</option>
        {/each}
      </select>
    </div>

    <div class="flips" aria-label="Flip controls">
      <button
        type="button"
        aria-label="Horizontal flip"
        aria-pressed={draft.flipX}
        disabled={pending}
        onclick={() => flipTransform("x")}
        onkeydown={onKeydown}
      >
        Flip H
      </button>
      <button
        type="button"
        aria-label="Vertical flip"
        aria-pressed={draft.flipY}
        disabled={pending}
        onclick={() => flipTransform("y")}
        onkeydown={onKeydown}
      >
        Flip V
      </button>
    </div>

    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}

    <div class="actions">
      <button type="button" aria-label="Cancel transform" onclick={cancelTransformSession} onkeydown={onKeydown}>
        Cancel
      </button>
      <button
        type="button"
        class="primary"
        aria-label="Apply transform"
        disabled={pending}
        onclick={() => void apply()}
        onkeydown={onKeydown}
      >
        {pending ? "Applying..." : "Apply"}
      </button>
    </div>
  </section>
{/if}

<style>
  .inspector {
    display: grid;
    gap: 14px;
    box-sizing: border-box;
    width: 100%;
    min-width: 0;
    padding: 14px 12px 16px;
    background: var(--bg-panel);
    color: var(--text);
    border-bottom: 1px solid var(--border);
  }

  .header,
  .actions,
  .flips {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .header h2 {
    margin: 0;
    color: var(--text-strong);
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.02em;
  }

  .hint {
    margin-left: auto;
    color: var(--text-dim);
    font-size: 11px;
  }

  .fields {
    display: grid;
    grid-template-columns: 68px minmax(0, 1fr);
    align-items: center;
    gap: 8px;
  }

  label {
    color: var(--text-dim);
    font-size: 12px;
  }

  /* 可拖调的值标签（票 54/V06）：横向拖动 = 改值，所以光标与禁止选中文字。 */
  label.scrub {
    cursor: ew-resize;
    user-select: none;
    touch-action: none;
  }

  input,
  select,
  button {
    min-height: 30px;
    box-sizing: border-box;
    background: var(--bg-raised);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 4px 7px;
    font: inherit;
  }

  input,
  select {
    width: 100%;
  }

  button {
    cursor: pointer;
  }

  button[aria-pressed="true"] {
    border-color: var(--accent);
    color: var(--accent);
  }

  .flips button {
    flex: 1;
  }

  .actions {
    justify-content: flex-end;
    padding-top: 12px;
    border-top: 1px solid var(--border);
  }

  .actions button {
    min-width: 72px;
  }

  .primary {
    background: var(--accent-action);
    color: #fff;
    border-color: var(--accent-action);
  }

  .primary:hover:not(:disabled) {
    background: var(--accent-hover);
  }

  .error {
    margin: 0;
    color: var(--danger);
    font-size: 12px;
  }

  :is(input, select, button):disabled {
    cursor: default;
    opacity: 0.6;
  }

  :is(input, select, button):focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
</style>
