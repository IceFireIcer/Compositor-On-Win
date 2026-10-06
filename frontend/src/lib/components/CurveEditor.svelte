<script lang="ts">
  import { onMount } from "svelte";

  /**
   * The point-curve editor (CameraRawColor.swift's point curves): an SVG
   * plane with draggable anchors, double-click to add, double-click an
   * anchor to remove. Endpoints pin to x=0 and x=255. Shared by the
   * adjustment Curves dialog and the Camera Raw panel.
   */

  let {
    points,
    onchange,
    label = "曲线编辑器",
  }: {
    points: { x: number; y: number }[];
    onchange: (next: { x: number; y: number }[]) => void;
    label?: string;
  } = $props();

  let dragIndex = $state(-1);

  function svgPoint(event: MouseEvent & { currentTarget: SVGSVGElement }): {
    x: number;
    y: number;
  } {
    const rect = event.currentTarget.getBoundingClientRect();
    const x = Math.round(((event.clientX - rect.left) / rect.width) * 255);
    const y = Math.round((1 - (event.clientY - rect.top) / rect.height) * 255);
    return { x: Math.max(0, Math.min(255, x)), y: Math.max(0, Math.min(255, y)) };
  }

  function down(event: MouseEvent & { currentTarget: EventTarget & SVGCircleElement }, index: number): void {
    event.preventDefault();
    dragIndex = index;
  }

  function move(event: MouseEvent & { currentTarget: SVGSVGElement }): void {
    if (dragIndex < 0) return;
    const next = [...points];
    const p = svgPoint(event);
    const first = dragIndex === 0;
    const last = dragIndex === next.length - 1;
    next[dragIndex] = { x: first ? 0 : last ? 255 : p.x, y: p.y };
    onchange(next);
  }

  function up(): void {
    dragIndex = -1;
  }

  function add(event: MouseEvent & { currentTarget: SVGSVGElement }): void {
    if (points.length >= 16) return;
    const p = svgPoint(event);
    onchange([...points, p].sort((a, b) => a.x - b.x));
  }

  function remove(index: number): void {
    if (points.length <= 2 || index === 0 || index === points.length - 1) return;
    onchange(points.filter((_, i) => i !== index));
  }

  let svgEl = $state<SVGSVGElement | null>(null);
  onMount(() => {
    const el = svgEl;
    if (!el) return;
    const relax = (e: Event): void => {
      e.preventDefault();
    };
    el.addEventListener("mousedown", relax);
    return () => el.removeEventListener("mousedown", relax);
  });
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<svg
  class="curve"
  viewBox="-8 -8 271 271"
  role="application"
  aria-label={label}
  bind:this={svgEl}
  onmousemove={move}
  onmouseup={up}
  onmouseleave={up}
  ondblclick={add}
>
  <line x1="0" y1="255" x2="255" y2="0" class="diag" />
  {#each points as p, i (i)}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <circle
      cx={p.x}
      cy={255 - p.y}
      r="5"
      class:drag={i === dragIndex}
      onmousedown={(e) => down(e, i)}
      ondblclick={(e) => {
        e.stopPropagation();
        remove(i);
      }}
    />
  {/each}
</svg>

<style>
  .curve {
    width: 100%;
    aspect-ratio: 1;
    background: var(--bg-canvas);
    border: 1px solid var(--border);
    border-radius: 4px;
    touch-action: none;
    user-select: none;
  }

  .curve .diag {
    stroke: var(--border);
    stroke-width: 1;
  }

  .curve circle {
    fill: var(--text);
    stroke: var(--bg-panel);
    stroke-width: 2;
    cursor: grab;
  }

  .curve circle.drag {
    fill: #48c;
  }
</style>
