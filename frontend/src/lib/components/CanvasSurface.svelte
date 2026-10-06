<script lang="ts">
  import { get } from "svelte/store";
  import { untrack } from "svelte";
  import type { DocTab } from "../state/workspace";
  import {
    CRISP_ZOOM,
    PIXEL_GRID_ZOOM,
    documentPoint,
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
  import { eyedropperClick } from "../state/filters";
  import { currentSelection } from "../state/selection";

  /** One marching-ants loop as an SVG path (doc pixel coordinates). */
  function loopPath(loop: { x: number; y: number }[]): string {
    if (loop.length === 0) return "";
    let d = `M ${loop[0].x} ${loop[0].y}`;
    for (let i = 1; i < loop.length; i++) {
      d += ` L ${loop[i].x} ${loop[i].y}`;
    }
    return d + " Z";
  }
  import { document as documentState, reloadDocument } from "../state/document";
  import {
    BeginStroke,
    EndStroke,
    StrokePoint,
    BeginHealStroke,
    HealPoint,
    EndHealStroke,
  } from "../../../wailsjs/go/bridge/Service";
  import { healSettings } from "../state/tools";
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

  // The composited document image, rendered Go-side at /render/{id}.png and
  // cache-busted by the document rev. Only shown while this tab is the
  // document the store holds; a 404 (e.g. an empty new document) hides it
  // until the next rev.
  const docState = $derived($documentState);
  const renderSrc = $derived(
    docState.docId === doc.id
      ? `/render/${doc.id}.png?v=${docState.rev}-${docState.filterRev}`
      : null,
  );
  let renderBroken = $state(false);
  $effect(() => {
    void renderSrc;
    renderBroken = false;
  });

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

  // Brush strokes: the bridge paints into the active layer's image; the
  // frontend streams document-space points and reloads the render (rev bump)
  // when the stroke ends. Left-drag paints for the brush and the default
  // move tool (no canvas transform is wired yet); pan/zoom keep their own
  // press behavior above.
  const PAINT_TOOLS: ReadonlySet<string> = new Set(["move", "brush"]);

  let painting = false;
  let healing = false;
  let strokeQueued = 0; // pending requestAnimationFrame handle
  let strokePending: { x: number; y: number } | null = null;

  /** Pointer position clamped into document pixel space (integer contract). */
  function docPixelOf(e: PointerEvent): { x: number; y: number } {
    const r = stage!.getBoundingClientRect();
    const p = documentPoint(
      get(viewport),
      e.clientX - r.left,
      e.clientY - r.top,
      doc.width,
      doc.height,
    );
    return {
      x: Math.min(doc.width, Math.max(0, Math.round(p.x))),
      y: Math.min(doc.height, Math.max(0, Math.round(p.y))),
    };
  }

  /** Coalesce move floods into one StrokePoint per animation frame. */
  function queueStrokePoint(p: { x: number; y: number }): void {
    strokePending = p;
    if (strokeQueued) return;
    strokeQueued = requestAnimationFrame(() => {
      strokeQueued = 0;
      const pt = strokePending;
      strokePending = null;
      if (painting && pt) void StrokePoint(pt.x, pt.y);
    });
  }

  $effect(() => {
    return () => {
      if (strokeQueued) cancelAnimationFrame(strokeQueued);
    };
  });

  function onPointerDown(e: PointerEvent): void {
    // An armed Levels eyedropper swallows plain clicks before any tool.
    if (e.button === 0 && !panDrag && !painting) {
      const p = docPixelOf(e);
      if (eyedropperClick(Math.floor(p.x), Math.floor(p.y))) {
        e.preventDefault();
        return;
      }
    }
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
      return;
    }
    if (e.button === 0 && $activeTool === "spotHealing") {
      healing = true;
      stage?.setPointerCapture(e.pointerId);
      e.preventDefault();
      const hs = get(healSettings);
      void BeginHealStroke(
        hs.mode,
        hs.diameter,
        hs.hardness,
        hs.smoothing,
        get(viewport).zoom,
        hs.opacity,
        Math.floor(Math.random() * 0xffffffff),
      );
      return;
    }
    if (e.button === 0 && PAINT_TOOLS.has($activeTool)) {
      painting = true;
      stage?.setPointerCapture(e.pointerId);
      e.preventDefault();
      const p = docPixelOf(e);
      void BeginStroke(p.x, p.y);
    }
  }

  function onPointerMove(e: PointerEvent): void {
    if (panDrag) {
      panByStore(e.clientX - panDrag.x, e.clientY - panDrag.y);
      panDrag = { x: e.clientX, y: e.clientY };
      return;
    }
    if (painting) queueStrokePoint(docPixelOf(e));
    if (healing) {
      const p = docPixelOf(e);
      void HealPoint(p.x, p.y);
    }
  }

  function onPointerEnd(e: PointerEvent): void {
    if (healing) {
      healing = false;
      void EndHealStroke();
      return;
    }
    if (painting) {
      painting = false;
      if (strokeQueued) {
        cancelAnimationFrame(strokeQueued);
        strokeQueued = 0;
      }
      const last = strokePending;
      strokePending = null;
      void (async () => {
        try {
          if (last) await StrokePoint(last.x, last.y);
          await EndStroke();
        } finally {
          // The rev only moves if the stroke landed; an unchanged rev is a
          // no-op in the document store's rev cache (no img reload).
          void reloadDocument();
        }
      })();
    }
    if (panDrag) stage?.releasePointerCapture(e.pointerId);
    panDrag = null;
    panning = false;
  }

  function onContextMenu(e: MouseEvent): void {
    e.preventDefault(); // Alt-click / right-click belong to the tools
  }
</script>

<!--
  The document lives Go-side (internal/domain + render); the canvas maps it
  through the viewport store — anchored zoom, pan, pixel grid from 800%,
  checkerboard and document bounds — and streams brush strokes back.
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
  <!-- The document: checkerboard (transparency) + Go-rendered composite +
       document bounds, placed by the viewport mapping (center + pan), sized
       zoom * document pixels. The img keeps transparency so the checkerboard
       shows through. -->
  <div
    class="doc"
    style:left="{rect.x}px"
    style:top="{rect.y}px"
    style:width="{doc.width * ppp}px"
    style:height="{doc.height * ppp}px"
  >
    {#if renderSrc && !renderBroken}
      <img
        class="render"
        class:crisp={vp.zoom >= CRISP_ZOOM}
        src={renderSrc}
        alt=""
        draggable="false"
        onerror={() => (renderBroken = true)}
      />
    {/if}
    {#if $currentSelection && $currentSelection.active}
      <svg class="ants" viewBox="0 0 {doc.width} {doc.height}" preserveAspectRatio="none">
        {#each $currentSelection.loops as loop, li (li)}
          <path d={loopPath(loop)} />
        {/each}
      </svg>
    {/if}
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

  /* The Go-rendered composite fills the document rect. */
  .render {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    display: block;
  }

  /* Hard-edged document pixels from 200% (EditorCanvas.swift:923 crispZoom). */
  .render.crisp {
    image-rendering: pixelated;
  }

  /* Per-document-pixel grid: one hairline per pixel, tone from the original
     pixel grid painter (EditorCanvas.swift:1429: white 0.55, alpha 0.45).
     background-size is set inline to `ppp` px, so lines sit on real pixels. */
  .ants {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    pointer-events: none;
  }

  .ants path {
    fill: none;
    stroke: #fff;
    stroke-width: 1;
    vector-effect: non-scaling-stroke;
    stroke-dasharray: 4 4;
  }

  .pixel-grid {
    position: absolute;
    inset: 0;
    background-image:
      linear-gradient(to right, rgba(140, 140, 140, 0.45) 1px, transparent 1px),
      linear-gradient(to bottom, rgba(140, 140, 140, 0.45) 1px, transparent 1px);
    pointer-events: none;
  }
</style>
