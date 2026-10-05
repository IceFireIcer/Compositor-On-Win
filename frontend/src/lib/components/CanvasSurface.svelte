<script lang="ts">
  import { get } from "svelte/store";
  import { untrack } from "svelte";
  import type { DocTab } from "../state/workspace";
  import {
    PIXEL_GRID_ZOOM,
    documentRect,
    fitViewport,
    keyboardZoom,
    keyboardZoomTarget,
    panByStore,
    pointsPerPixel,
    resizeViewport,
    viewport,
    zoomAt,
    zoomToValue,
  } from "../state/viewport";
  import { Modifier, applyKey, clearModifiers, modifierBits } from "../state/modifiers";
  import { activeTool } from "../state/tools";
  import { cursorForTool } from "./cursors";

  let { doc }: { doc: DocTab } = $props();

  // View size in CSS pixels, measured from the stage (bind updates on resize).
  let stage = $state<HTMLDivElement>();
  let viewWidth = $state(0);
  let viewHeight = $state(0);
  let panning = $state(false);

  const vp = $derived($viewport);
  const ppp = $derived(pointsPerPixel(vp));
  const rect = $derived(documentRect(vp, doc.width, doc.height));
  // The pixel grid appears from 800% (EditorCanvas.swift:920); lines sit on
  // every document pixel, i.e. every `ppp` CSS px >= 8px apart.
  const showGrid = $derived(vp.zoom >= PIXEL_GRID_ZOOM);
  const cursor = $derived(
    cursorForTool({ tool: $activeTool, bits: $modifierBits, panning }),
  );

  // Report the measured view size (and refit while followsFit). doc.id is a
  // dependency too: a fresh document starts from a fresh, fit-centered view.
  $effect(() => {
    void doc.id;
    resizeViewport(viewWidth, viewHeight, window.devicePixelRatio, doc.width, doc.height);
  });

  // Wheel: ctrl/cmd/alt+wheel zooms about the pointer (non-passive so the
  // browser's own ctrl+wheel page zoom is suppressed), plain wheel scrolls.
  $effect(() => {
    const el = stage;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      if (e.ctrlKey || e.metaKey || e.altKey) {
        // Smooth zoom: one screen notch (~100 delta) is roughly a ladder step.
        const factor = Math.exp(-e.deltaY * 0.002);
        const s = untrack(() => get(viewport));
        const r = el.getBoundingClientRect();
        zoomAt(s.zoom * factor, e.clientX - r.left, e.clientY - r.top, doc.width, doc.height);
      } else {
        panByStore(-e.deltaX, -e.deltaY);
      }
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => el.removeEventListener("wheel", onWheel);
  });

  // Window-level keyboard: modifier bits everywhere, zoom shortcuts while the
  // surface is mounted (guarding text entry), Space = temporary hand tool.
  $effect(() => {
    const editable = (t: EventTarget | null): boolean => {
      const el = t as HTMLElement | null;
      return (
        !!el &&
        (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable)
      );
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (editable(e.target)) return;
      if (applyKey(e.code, true)) {
        // Keep Space from re-clicking focused buttons and scrolling pages.
        if (e.code === "Space" || e.code.startsWith("Alt")) e.preventDefault();
        return;
      }
      if (!(e.ctrlKey || e.metaKey)) return;
      switch (e.code) {
        case "Equal":
        case "NumpadAdd":
          e.preventDefault();
          keyboardZoom(1, doc.width, doc.height);
          break;
        case "Minus":
        case "NumpadSubtract":
          e.preventDefault();
          keyboardZoom(-1, doc.width, doc.height);
          break;
        case "Digit0":
          e.preventDefault();
          fitViewport(doc.width, doc.height);
          break;
        case "Digit1":
          e.preventDefault();
          zoomToValue(1, doc.width, doc.height); // actual pixels
          break;
      }
    };
    const onKeyUp = (e: KeyboardEvent) => {
      applyKey(e.code, false);
    };
    const onBlur = () => clearModifiers(); // a stuck Space would freeze the canvas
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("keyup", onKeyUp);
    window.addEventListener("blur", onBlur);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("keyup", onKeyUp);
      window.removeEventListener("blur", onBlur);
      clearModifiers();
    };
  });

  // Pan drag state: Space (any tool), the hand tool, or the middle button.
  let panDrag: { x: number; y: number } | null = null;

  function onPointerDown(e: PointerEvent): void {
    const spaceHeld = ($modifierBits & Modifier.Space) !== 0;
    if (e.button === 1 || ((spaceHeld || $activeTool === "hand") && e.button === 0)) {
      panDrag = { x: e.clientX, y: e.clientY };
      panning = true;
      stage?.setPointerCapture(e.pointerId);
      e.preventDefault();
      return;
    }
    if (e.button === 0 && $activeTool === "zoom") {
      // Zoom tool press: a step on the keyboard ladder about the click point,
      // out with Alt (EditorCanvas.swift zoomDrag's press-release behavior).
      const dir = e.altKey ? -1 : 1;
      const target = keyboardZoomTarget(get(viewport), dir);
      const r = stage!.getBoundingClientRect();
      zoomAt(target, e.clientX - r.left, e.clientY - r.top, doc.width, doc.height);
    }
  }

  function onPointerMove(e: PointerEvent): void {
    if (!panDrag) return;
    panByStore(e.clientX - panDrag.x, e.clientY - panDrag.y);
    panDrag = { x: e.clientX, y: e.clientY };
  }

  function onPointerEnd(e: PointerEvent): void {
    if (panDrag) stage?.releasePointerCapture(e.pointerId);
    panDrag = null;
    panning = false;
  }

  function onContextMenu(e: MouseEvent): void {
    e.preventDefault(); // Alt-click / right-click belong to the tools
  }
</script>

<!--
  Ticket 13: the document rendered through the viewport store — anchored
  zoom, pan, pixel grid from 800%, checkerboard and document bounds.
-->
<div
  class="canvas-stage"
  role="application"
  aria-label="画布"
  bind:this={stage}
  bind:clientWidth={viewWidth}
  bind:clientHeight={viewHeight}
  style:cursor={cursor}
  onpointerdown={onPointerDown}
  onpointermove={onPointerMove}
  onpointerup={onPointerEnd}
  onpointercancel={onPointerEnd}
  oncontextmenu={onContextMenu}
>
  <!-- The document: checkerboard (transparency) + document bounds, placed by
       the viewport mapping (center + pan), sized zoom * document pixels. -->
  <div
    class="doc"
    style:left="{rect.x}px"
    style:top="{rect.y}px"
    style:width="{doc.width * ppp}px"
    style:height="{doc.height * ppp}px"
  >
    {#if showGrid}
      <div
        class="pixel-grid"
        style:background-size="{ppp}px {ppp}px"
      ></div>
    {/if}
  </div>
</div>

<style>
  .canvas-stage {
    position: absolute;
    inset: 0;
    overflow: hidden; /* pan is handled by the viewport, not by scrolling */
    touch-action: none;
    user-select: none;
    background: var(--bg-canvas, #232327);
  }

  /* Checkerboard + document bounds (carried over from ticket 02). */
  .doc {
    position: absolute;
    background: repeating-conic-gradient(#3c3c41 0% 25%, #4a4a50 0% 50%) 0 0 / 16px 16px;
    border: 1px solid #141416;
    box-shadow: 0 0 0 1px rgba(255, 255, 255, 0.08), 0 8px 32px rgba(0, 0, 0, 0.45);
  }

  /* Per-document-pixel grid: one hairline per pixel, tone from the original
     pixel grid painter (EditorCanvas.swift:1429: white 0.55, alpha 0.45).
     background-size is set inline to `ppp` px, so lines sit on real pixels. */
  .pixel-grid {
    position: absolute;
    inset: 0;
    background-image:
      linear-gradient(to right, rgba(140, 140, 140, 0.45) 1px, transparent 1px),
      linear-gradient(to bottom, rgba(140, 140, 140, 0.45) 1px, transparent 1px);
    pointer-events: none;
  }
</style>
