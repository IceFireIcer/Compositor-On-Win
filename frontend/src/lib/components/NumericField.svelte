<script lang="ts">
  import { arrows, direct, scrub, type ValueRange } from "./numeric";

  // 数值字段（票 53/V06）：三种调法全部来自 numeric.ts 的纯函数，本组件只做事件接线。
  //   · 拖标签横向改值 → scrub()（移植自 NumericScrub.swift）：每拖一像素加 sensitivity，
  //     拖拽按 step 吸附、超出范围钳制（不回绕）；
  //   · 输入框 ↑/↓ 步进、Shift 把步长 ×10 → arrows()（移植自 ArrowStepper）；
  //   · 输入提交 → direct() 解析并钳制，解析不出来就保留旧值（commit-on-submit 语义）。
  // 数值语义不要在组件里重新实现，否则就和单测覆盖的那份实现分叉了。
  let {
    label,
    value,
    min = -Infinity,
    max = Infinity,
    step = 1,
    sensitivity = 1,
    unit = "",
    disabled = false,
    onchange,
  }: {
    label: string;
    value: number;
    min?: number;
    max?: number;
    step?: number;
    sensitivity?: number;
    /** 输入框后面的单位文字（如 "%"），纯展示，不参与取值。 */
    unit?: string;
    disabled?: boolean;
    onchange: (value: number) => void;
  } = $props();

  // 同一工具栏里会挂很多个字段，每个实例要一个稳定且唯一的 id 供 <label for> 关联。
  const uid = $props.id();
  const range = $derived<ValueRange>({ min, max });

  // 标签拖拽：按下时记住起点值与起点横坐标，每帧算 起点 + 位移 × 灵敏度。
  let gesture: { startX: number; startValue: number } | null = null;

  function beginScrub(event: PointerEvent): void {
    if (disabled || event.button !== 0) return;
    gesture = { startX: event.clientX, startValue: value };
    (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
    event.preventDefault();
  }

  function moveScrub(event: PointerEvent): void {
    const current = gesture;
    if (!current || disabled) return;
    const next = scrub(event.clientX - current.startX, current.startValue, {
      sensitivity,
      step,
      range,
    });
    if (next !== value) onchange(next);
  }

  function endScrub(): void {
    gesture = null;
  }

  function onKeydown(event: KeyboardEvent): void {
    if (disabled || (event.key !== "ArrowUp" && event.key !== "ArrowDown")) return;
    // 拦掉原生 number 输入框自己的方向键步进，统一走 arrows()（含 Shift ×10 与范围钳制）。
    event.preventDefault();
    const next = arrows(event.key === "ArrowUp" ? "up" : "down", value, {
      step,
      shift: event.shiftKey,
      range,
    });
    if (next !== value) onchange(next);
  }

  function commitInput(event: Event): void {
    if (disabled) return;
    const input = event.currentTarget as HTMLInputElement;
    const parsed = direct(input.value, range);
    if (parsed === null) {
      // 空、半截或非数字：保留旧值，把输入框显示改回去。
      input.value = String(value);
      return;
    }
    // 钳制后的结果先写回显示，避免父组件回写之前闪一下超范围数字。
    input.value = String(parsed);
    if (parsed !== value) onchange(parsed);
  }
</script>

<span class="numeric-field">
  <label
    for="{uid}-input"
    onpointerdown={beginScrub}
    onpointermove={moveScrub}
    onpointerup={endScrub}
    onpointercancel={endScrub}
  >
    {label}
  </label>
  <input
    id="{uid}-input"
    type="number"
    min={Number.isFinite(min) ? min : undefined}
    max={Number.isFinite(max) ? max : undefined}
    step={step}
    {disabled}
    value={value}
    onchange={commitInput}
    onkeydown={onKeydown}
  />
  {#if unit}<span class="unit">{unit}</span>{/if}
</span>

<style>
  .numeric-field {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    flex: 0 0 auto;
    white-space: nowrap;
  }

  /* 可拖调的值标签：横向拖动 = 改值，所以用左右箭头光标并禁止选中文字。 */
  label {
    color: var(--text-dim);
    cursor: ew-resize;
    user-select: none;
    touch-action: none;
  }

  input {
    box-sizing: border-box;
    width: 64px;
    min-height: 22px;
    background: var(--bg-panel);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 2px 4px;
    font: inherit;
  }

  input:disabled {
    cursor: default;
    opacity: 0.6;
  }

  input:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
</style>
