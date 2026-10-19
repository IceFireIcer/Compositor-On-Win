<script lang="ts">
  import { get } from "svelte/store";
  import { untrack } from "svelte";
  import type { DocTab } from "../state/workspace";
  import {
    CRISP_ZOOM,
    PIXEL_GRID_ZOOM,
    documentPoint,
    documentRect,
    keyboardZoomTarget,
    middleZoomFactor,
    panByStore,
    pointsPerPixel,
    resizeViewport,
    viewport,
    zoomAt,
  } from "../state/viewport";
  import { Modifier, applyKey, clearModifiers, modifierBits } from "../state/modifiers";
  import {
    activeTool,
    BRUSH_TOOLS,
    brushSettings,
    cloneSettings,
    healSettings,
    selectionSettings,
    toolModes,
    updateBrushSettings,
    updateHealSettings,
    wandSettings,
  } from "../state/tools";
  import { eyedropperClick, filterSession, updateFilterSettings } from "../state/filters";
  import {
    PROGRESSIVE_HANDLE_RADIUS,
    beginProgressiveDrag,
    defaultProgressiveLine,
    moveProgressiveHandle,
    progressiveLine,
    type ProgressiveHandle,
  } from "../state/progressiveBlur";
  import {
    currentSelection,
    edgeAutoScroll,
    ellipseRegion,
    lassoRegion,
    rectRegion,
    select,
    selectionMode,
    selectionFromMask,
    type Point as SelectionPoint,
    type Region as SelectionRegion,
    type SelectionMode,
  } from "../state/selection";
  import {
    TEXT_BOX_HANDLES,
    editorBoxSize,
    heldSelection,
    resizeTextBox,
    sessionTextBox,
    setTextBox,
    textSession,
    startPointText,
    startBoxText,
    editTextLayerAt,
    type TextBoxHandle,
    type TextBoxRect,
    type TextStyle,
    type TextSession,
  } from "../state/text";
  import { dropRuns, reconcileRunsForEdit } from "../state/textRuns";

  /** One marching-ants loop as an SVG path (doc pixel coordinates). */
  function loopPath(loop: { x: number; y: number }[]): string {
    if (loop.length === 0) return "";
    let d = `M ${loop[0].x} ${loop[0].y}`;
    for (let i = 1; i < loop.length; i++) {
      d += ` L ${loop[i].x} ${loop[i].y}`;
    }
    return d + " Z";
  }
  import { document as documentState, applyDocumentSnapshot } from "../state/document";
  import { beginCanvasStroke, beginCanvasDrawing, moveCanvasDrawing, finishCanvasDrawing, setCanvasCloneSource, canvasCloneSources, type DrawingGesture } from "../state/canvasDrawing";
  import { getCanvasEditTarget } from "../state/layerTarget";
  import type { PointerStroke } from "../state/pointerStroke";
  import { StrokePreview } from "../state/strokePreview";
  import { beginBrushTipDrag, cursorCircle, dragBrushTip, type BrushTipDrag, type BrushTipHud } from "../gpu/brushCursor";
  import { WebGpuCompositor } from "../gpu/compositor";
  import { gamutWarning, proofColors, proofLookups } from "../state/cmykProof";
  import { palette, showsSampleRing } from "../state/palette";
  import { rgbToHex, setBackground, setForeground, type Rgb } from "../state/color";
  import {
    SampleCompositeColor,
    SelectWandAt,
    SelectObjectAt,
  } from "../../../wailsjs/go/bridge/Service";
  import { cursorForTool } from "./cursors";

  function decodeProof(encoded: string): Uint8Array {
    const binary = atob(encoded);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
    return bytes;
  }
  import {
    beginGuideCreation,
    beginGuideMove,
    cancelGuideDrag,
    draggingGuide,
    finishGuideDrag,
    guidesJSON,
    guidesState,
    layoutGrid,
    lockGuides,
    moveGuideDrag,
    showGuides,
    showGrid as showLayoutGrid,
    showRulers,
    showsPixelGrid,
    visibleGuides,
  } from "../state/view";
  import { gridLines, gridStep } from "../state/snap";
  import { commitGuides as commitGuidesAction } from "../shortcuts/actions";
  import CropOverlay from "./CropOverlay.svelte";
  import Navigator from "./Navigator.svelte";
  import {
    beginTransformDrag,
    beginTransformSession,
    cancelTransformSession,
    commitTransformSession,
    dragTransform,
    transformSession,
    transformPreviewURL,
    transformSnapGuides,
  } from "../state/transform";
  import { canvasSnapInput } from "../state/canvasSnap";
  import {
    HANDLES,
    rotationHandle,
    showsTransformControls,
    transformContains,
    transformCorners,
    transformedPoint,
    type DragMode,
  } from "../state/transformSession";

  let { doc }: { doc: DocTab } = $props();

  // View size in CSS pixels, measured from the stage (bind updates on resize).
  let stage = $state<HTMLDivElement>();
  let viewWidth = $state(0);
  let viewHeight = $state(0);
  let panning = $state(false);
  // 中键 + Command/Control：绕按下点缩放（票 75，EditorCanvas.swift:2041-2048）。
  // 锚点是按下时的视图坐标，拖到哪算到哪，与平移各自独立。
  let middleZoom = $state<{ anchorX: number; anchorY: number; zoom: number } | null>(null);
  let middleZoomOut = $state(false);
  // 票 73：渐进模糊线的拖动。端点本身存在滤镜会话的设置里（文档坐标），这里只记
  // 「正在拖哪一个」，所以对话框滑杆与画布拖动改的是同一份状态。
  let progressiveDrag: ProgressiveHandle | null = null;

  type SelectionGesture =
    | {
        kind: "marquee";
        start: SelectionPoint;
        current: SelectionPoint;
        shape: "rect" | "ellipse";
        mode: SelectionMode;
      }
    | {
        kind: "lasso";
        points: SelectionPoint[];
        polygon: boolean;
        mode: SelectionMode;
      };
  let selectionGesture = $state<SelectionGesture | null>(null);

  // Text tool (ticket 44): a press on a live text layer edits it, otherwise
  // a drag draws a paragraph box and a click makes point text.
  let textDrag = $state<{ x: number; y: number } | null>(null);
  let textDragRect = $state<{ x: number; y: number; w: number; h: number } | null>(null);
  let textEditor = $state<HTMLTextAreaElement | undefined>();

  // 只在 textarea 元素第一次出现时聚焦（原版 draftID 变化才算 fresh）：
  // 会话对象的每次更新（内容、选区写回）不代表重新打开，不能重复 focus()——
  // 否则点选项栏输入框时 blur 回写选区会把焦点抢回画布。
  let focusedEditor: HTMLTextAreaElement | undefined;
  $effect(() => {
    const el = textEditor;
    const sess = $textSession;
    if (!sess) {
      focusedEditor = undefined;
      return;
    }
    if (el && el !== focusedEditor) {
      focusedEditor = el;
      el.focus();
    }
  });

  function textColor(): string {
    const sess = get(textSession);
    if (!sess) return "#000";
    const byte = (v: number) => Math.max(0, Math.min(255, Math.round(v * 255)));
    return `rgb(${byte(sess.style.red)}, ${byte(sess.style.green)}, ${byte(sess.style.blue)})`;
  }

  // --- 逐 run 样式（票 45）：选区同步 + 编辑时的 run 迁移 ---------------------
  // textarea 是纯文本控件，画不出逐 run 的混排颜色/字体——完整结果在提交后由
  // Go 渲染端画到画布上，这里只预览基础样式。选中范围与 run 迁移的语义与原版
  // TypeTool.swift / InlineTextEditor.swift 一致，纯函数在 textRuns.ts。

  /** beforeinput 捕获到的编辑范围（DOM 更新前），input 时用于 run 迁移。 */
  let pendingEdit: { start: number; end: number } | null = null;

  function sameSel(
    a: { start: number; end: number } | null,
    b: { start: number; end: number } | null,
  ): boolean {
    if (a === null || b === null) return a === b;
    return a.start === b.start && a.end === b.end;
  }

  /** 写回选区（UTF-16 码元偏移，与 DOM selectionStart/End 同一坐标系）。 */
  function setSelection(next: { start: number; end: number } | null): void {
    // 先比再写：svelte/store 的 safe_not_equal 把任何对象都看成"变了"，
    // update 即使返回同一个对象也照样 notify —— 上面的 $effect 会被这次
    // notify 再唤醒一次，构成 setSelection ↔ effect 的死循环
    // （运行时实测 effect_update_depth_exceeded）。值没变就根本不碰 store。
    const cur = get(textSession);
    if (!cur || sameSel(cur.selection, next)) return;
    textSession.update((s) => (s ? { ...s, selection: next } : s));
  }

  /** 读 textarea 的当前选区；收拢的光标或未知都写 null。 */
  function syncSelection(): void {
    const el = textEditor;
    if (!el) return;
    const start = el.selectionStart ?? 0;
    const end = el.selectionEnd ?? 0;
    setSelection(start === end ? null : { start, end });
  }

  /**
   * 失焦：保留选区（票 45）。选项栏的字体选择器/颜色控件一夺焦，textarea 的
   * 高亮就塌陷，但逐 run 的字体与颜色要应用到"刚才选中的那批字母"——原版
   * InlineTextEditor.swift:26-36 的 holdsSelection/keepSelection 正是为此。
   * 编辑器内真正的塌陷（点击/按键）走 syncSelection，不受这里影响。
   */
  function onTextBlur(): void {
    const el = textEditor;
    if (!el) return;
    const start = el.selectionStart ?? 0;
    const end = el.selectionEnd ?? 0;
    setSelection(heldSelection(get(textSession), end > start ? { start, end } : null));
  }

  // --- 段落框缩放（票 45 / X01 尾项）-----------------------------------------
  // 原版 InlineTextEditor 的 8 向手柄：拖角/边改 boxSize，点文本第一次拖手柄
  // 先按当前显示尺寸物化成段落框（InlineTextEditor.swift:450-456）。手柄元素
  // 只负责指针，几何全在 text.ts 的纯函数里（resizeTextBox）。
  let boxDrag: { handle: TextBoxHandle; box: TextBoxRect; at: { x: number; y: number } } | null = null;

  function startBoxResize(e: PointerEvent, handle: TextBoxHandle): void {
    const sess = get(textSession);
    if (!sess || e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    boxDrag = { handle, box: sessionTextBox(sess), at: docPixelOf(e) };
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
  }

  function onBoxResizeMove(e: PointerEvent): void {
    if (!boxDrag) return;
    const p = docPixelOf(e);
    setTextBox(resizeTextBox(boxDrag.box, boxDrag.handle, p.x - boxDrag.at.x, p.y - boxDrag.at.y));
  }

  function endBoxResize(e: PointerEvent): void {
    if (!boxDrag) return;
    boxDrag = null;
    const el = e.currentTarget as HTMLElement;
    if (typeof el.hasPointerCapture === "function" && el.hasPointerCapture(e.pointerId)) {
      el.releasePointerCapture(e.pointerId);
    }
  }

  /**
   * 合成编辑与浏览器撤销/重做不能按码元对齐：前者 IME 会绕过 beforeinput 的
   * 逐字符范围，后者的 textarea 撤销只回退文本、不回退 runs。
   */
  function undiffableInputType(inputType: string): boolean {
    return (
      /composition/i.test(inputType) ||
      inputType === "historyUndo" ||
      inputType === "historyRedo"
    );
  }

  /** beforeinput 的目标范围（Chromium/WebKit 支持）；不可用时退回 DOM 选区。 */
  function beforeInputRange(
    e: InputEvent,
    el: HTMLTextAreaElement,
  ): { start: number; end: number } {
    try {
      const ranges = e.getTargetRanges();
      if (ranges.length > 0) {
        return { start: ranges[0].startOffset, end: ranges[0].endOffset };
      }
    } catch {
      // 个别实现不支持 getTargetRanges：退回选区（选择替换、粘贴等仍能对上）
    }
    return { start: el.selectionStart ?? 0, end: el.selectionEnd ?? 0 };
  }

  /**
   * 捕获编辑范围（原版 shouldChangeTextIn(affectedCharRange, replacementString)，
   * 在内容变化之前）。捕获不到就不迁移，交给 input 时丢 runs。
   */
  function onTextBeforeInput(e: InputEvent): void {
    pendingEdit = null;
    const el = textEditor;
    if (!el || e.isComposing || undiffableInputType(e.inputType)) return;
    pendingEdit = beforeInputRange(e, el);
  }

  /**
   * 校验捕获范围与新旧内容一致，返回这次编辑插入的码元数；对不上返回 null
   * （粘贴到别处、词级删除、拖放搬移文本等）。插入长度也可以从两个值反推，
   * 所以「粘贴长度未知」只要范围对得上仍能迁移。
   */
  function verifiedInserted(
    oldValue: string,
    newValue: string,
    edit: { start: number; end: number },
  ): number | null {
    const { start, end } = edit;
    if (!(start >= 0 && end >= start && end <= oldValue.length)) return null;
    const inserted = newValue.length - (oldValue.length - (end - start));
    if (inserted < 0) return null;
    if (newValue.slice(0, start) !== oldValue.slice(0, start)) return null;
    if (newValue.slice(start + inserted) !== oldValue.slice(end)) return null;
    return inserted;
  }

  /** 有 runs 时按捕获范围迁移；对不上（或没捕获到）就丢 runs——原版回退。 */
  function styleAfterEdit(
    style: TextStyle,
    oldValue: string,
    newValue: string,
    edit: { start: number; end: number } | null,
  ): TextStyle {
    if (!style.colorRuns && !style.fontRuns) return style;
    if (!edit) return dropRuns(style);
    const inserted = verifiedInserted(oldValue, newValue, edit);
    if (inserted === null) return dropRuns(style);
    // 用编辑前的内容长度——原版 replaceCharacters 在 content 变化之前调用。
    return reconcileRunsForEdit(style, edit.start, edit.end, inserted, oldValue.length);
  }

  /**
   * input：DOM 已经更新。内容写回会话时先迁移 runs（用编辑前的内容长度），
   * 再写新内容与 DOM 选区。
   */
  function onTextInput(e: Event): void {
    const el = e.target as HTMLTextAreaElement;
    const value = el.value;
    const edit = pendingEdit;
    pendingEdit = null;
    const start = el.selectionStart ?? 0;
    const end = el.selectionEnd ?? 0;
    const nextSelection = start === end ? null : { start, end };
    textSession.update((s) => {
      if (!s) return s;
      if (s.content === value) {
        return sameSel(s.selection, nextSelection) ? s : { ...s, selection: nextSelection };
      }
      const next: TextSession = {
        ...s,
        content: value,
        style: styleAfterEdit(s.style, s.content, value, edit),
        selection: nextSelection,
      };
      return next;
    });
  }

  // 会话内容变化（新建/载入既有文字）后读一次 DOM 选区写回会话。只在编辑器
  // 仍持有焦点时读：失焦后 DOM 保留旧选区，不该把 blur 写下的 null 复活。
  //
  // 依赖收在 content 这一个字段上（$derived 只在值真的变了才通知下游）：
  // 直接读 `$textSession?.content` 会让 effect 订阅整个会话对象，于是
  // syncSelection 写回的 selection 又把它叫醒 —— 运行时实测就是
  // `effect_update_depth_exceeded`（Svelte 5 的 effect 深度上限被打爆），
  // 每敲一个字都会抛一次。
  const sessionContent = $derived($textSession?.content);
  $effect(() => {
    const content = sessionContent;
    const el = textEditor;
    if (content === undefined || !el || document.activeElement !== el) return;
    syncSelection();
  });

  const vp = $derived($viewport);
  const ppp = $derived(pointsPerPixel(vp));
  const rect = $derived(documentRect(vp, doc.width, doc.height));
  // The pixel grid appears from 800% (EditorCanvas.swift:920); lines sit on
  // every document pixel, i.e. every `ppp` CSS px >= 8px apart.
  const showGrid = $derived($showsPixelGrid && vp.zoom >= PIXEL_GRID_ZOOM);
  // 布局网格（View > Show Grid，票 14）：与上面那个"每文档像素一条"的像素网格
  // 不是一回事 —— 像素网格由缩放决定，布局网格由用户开关决定。
  const grid = $derived($layoutGrid);
  const gridStepPx = $derived(gridStep(grid) * ppp);
  const gridMajorPx = $derived(grid.spacing * ppp);
  // 线太密（<4px）时只画主线，否则整块画布会被网格糊成灰的。
  const gridMinorVisible = $derived(gridStepPx >= 4);
  // 标尺刻度：主刻度间距（屏幕点），随缩放自适应到一个可读的量级。
  const rulerStep = $derived(
    (() => {
      const target = 80; // 期望的主刻度屏幕间距
      const raw = target / Math.max(ppp, 0.0001);
      const pow = Math.pow(10, Math.floor(Math.log10(Math.max(raw, 1))));
      for (const mult of [1, 2, 5, 10]) {
        if (pow * mult >= raw) return pow * mult;
      }
      return pow * 10;
    })(),
  );
  const cursor = $derived(
    cursorForTool({
    tool: $activeTool,
    bits: $modifierBits,
    panning,
    middleZoom: middleZoom ? (middleZoomOut ? "out" : "in") : null,
  }),
  );

  /**
   * 票 73：渐进模糊的线，文档坐标。滤镜会话的 kind 是渐进模糊时才有；设置里
   * 缺端点就按默认落点补（与 Go 侧同一条公式），所以线总是画得出来。
   */
  const progressiveOverlay = $derived.by(() => {
    const session = $filterSession;
    if (!session || session.kind !== "progressiveBlur") return null;
    const layer = docState.layers.find((candidate) => candidate.id === session.layerId);
    const fallback = defaultProgressiveLine(
      {
        origin: layer?.transform.origin ?? [0, 0],
        size: layer?.transform.size ?? [doc.width, doc.height],
      },
      doc.width,
      doc.height,
    );
    return progressiveLine(session.settings, fallback);
  });

  // The composited document image, rendered Go-side at /render/{id}.png and
  // cache-busted by the document rev. Only shown while this tab is the
  // document the store holds; a 404 (e.g. an empty new document) hides it
  // until the next rev.
  const docState = $derived($documentState);
  let strokePreviewSrc = $state<string | null>(null);
  const renderSrc = $derived(
    docState.docId === doc.id
      ? $transformPreviewURL ?? `/render/${doc.id}.png?v=${docState.rev}-${docState.filterRev}`
      : null,
  );
  let renderBroken = $state(false);
  $effect(() => {
    void renderSrc;
    renderBroken = false;
  });

  // Navigator（票 61）的缩略图与画布是同一张合成图（同一个 URL，浏览器缓存
  // 共用），只是不取变换预览——原版 navigatorImage 用"裸 session"画，进行中
  // 的预览不进缩略图。
  const navigatorSrc = $derived(
    docState.docId === doc.id
      ? `/render/${doc.id}.png?v=${docState.rev}-${docState.filterRev}`
      : null,
  );

  let gpuCanvas = $state<HTMLCanvasElement>();
  let compositor = $state.raw<WebGpuCompositor | null>(null);
  let gpuFrame = $state("");
  const frameKey = $derived(`${doc.id}:${docState.rev}:${docState.filterRev}`);
  const gpuVisible = $derived(!strokePreviewSrc && !$transformPreviewURL && gpuFrame === frameKey && docState.docId === doc.id);
  $effect(() => {
    const canvas = gpuCanvas;
    if (!canvas) return;
    let disposed = false;
    let owned: WebGpuCompositor | null = null;
    void WebGpuCompositor.create(canvas).then((value) => {
      owned = value;
      if (disposed) value.destroy();
      else compositor = value;
    }).catch((error: unknown) => { console.warn("WebGPU 初始化失败", error); });
    return () => { disposed = true; owned?.destroy(); };
  });
  $effect(() => {
    const renderer = compositor;
    const canvas = gpuCanvas;
    const key = frameKey;
    const proofOn = $proofColors;
    const gamutOn = $gamutWarning;
    const lookups = $proofLookups;
    const request = { docId: doc.id, rev: docState.rev, filterRev: docState.filterRev };
    if (!renderer || !canvas || docState.docId !== doc.id) return;
    if (canvas.width !== doc.width) canvas.width = doc.width;
    if (canvas.height !== doc.height) canvas.height = doc.height;
    const mode = proofOn ? 1 : gamutOn ? 2 : 0;
    const table = proofOn && gamutOn ? lookups?.warning : gamutOn ? lookups?.marked : lookups?.proof;
    renderer.setProof(mode, table ? decodeProof(table) : null, lookups?.dimension ?? 0);
    let stale = false;
    void renderer.render(request).then((result) => {
      if (stale) return;
      if (result.track === "gpu") gpuFrame = key;
      else if (result.track === "cpu-fallback") gpuFrame = "";
    }).catch((error: unknown) => {
      if (!stale) gpuFrame = "";
      console.warn("WebGPU 渲染失败，使用 CPU 合成", error);
    });
    return () => { stale = true; };
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

  // Window-level keyboard: modifier bits everywhere and Space = temporary hand
  // tool. Zoom/Fit/Actual Pixels and the text keys belong to the shortcut layer
  // (ticket 47), which dispatches through the remappable shortcut table.
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
  // when the stroke ends. 只有画笔类工具落笔——**move 不在这里**：它是移动/变换
  // 工具，左键拖拽应当搬动图层，而不是画一道黑笔（票 17 的接线见下方
  // beginMoveDrag / transform 覆盖层）。
  let strokeGesture: { pointerID: number; tabID: string; layerID: string | null; targetMask: boolean; brush: boolean; name: string; stroke: PointerStroke } | null = null;
  const strokePreview = new StrokePreview((url) => { strokePreviewSrc = url; });
  let previewOwner: PointerStroke | null = null;
  let stopStrokePreview: (() => void) | null = null;
  let lastBrushPoint: { tabID: string; layerID: string | null; targetMask: boolean; point: SelectionPoint; gesture: PointerStroke } | null = null;
  let drawingGesture = $state<(DrawingGesture & { pointerID: number }) | null>(null);
  let brushPointer = $state<{ x: number; y: number } | null>(null);
  let hoverPoint = $state<SelectionPoint | null>(null);
  let tipDrag = $state<{ pointerID: number; healing: boolean; drag: BrushTipDrag } | null>(null);
  let tipHud = $state<BrushTipHud | null>(null);
  const tipSettings = $derived($activeTool === "spotHealing" ? $healSettings : $brushSettings);
  const tipCircle = $derived(BRUSH_TOOLS.has($activeTool) && brushPointer
    ? cursorCircle(tipDrag?.drag.start.x ?? brushPointer.x, tipDrag?.drag.start.y ?? brushPointer.y, tipSettings.diameter, ppp)
    : null);
  const cloneSource = $derived($canvasCloneSources[doc.id]);
  const cloneSample = $derived(cloneSource
    ? $cloneSettings.aligned && cloneSource.offset && hoverPoint
      ? { x: hoverPoint.x + cloneSource.offset.x, y: hoverPoint.y + cloneSource.offset.y }
      : cloneSource.anchor
    : null);
  let sampling = false;
  let sampleRunning = false;
  let samplePending: { tabID: string; x: number; y: number; background: boolean } | null = null;
  let sampleRing = $state<{ x: number; y: number; color: Rgb; previous: Rgb } | null>(null);
  let picking = false;

  function toolError(name: string, err: unknown): void {
    console.warn(name, err);
    window.alert(`${name}\n\n${err instanceof Error ? err.message : String(err)}`);
  }

  function applyDrawingResult(tabID: string, reply: unknown): void {
    if (typeof reply === "string" && get(documentState).docId === tabID) {
      applyDocumentSnapshot(reply, tabID);
    }
  }

  function finishStroke(cancel: boolean): void {
    const gesture = strokeGesture;
    if (!gesture) return;
    strokeGesture = null;
    if (cancel) clearStrokePreview(gesture.stroke);
    void gesture.stroke.finish(cancel).then((reply) => {
      applyDrawingResult(gesture.tabID, reply);
      clearStrokePreview(gesture.stroke);
    })
      .catch((err: unknown) => {
        clearStrokePreview(gesture.stroke);
        if (lastBrushPoint?.gesture === gesture.stroke) lastBrushPoint = null;
        toolError(gesture.name, err);
      });
  }

  function clearStrokePreview(owner?: PointerStroke): void {
    if (owner && previewOwner !== owner) return;
    stopStrokePreview?.();
    stopStrokePreview = null;
    previewOwner = null;
    strokePreview.clear();
  }

  function finishDrawing(cancel: boolean): void {
    const gesture = drawingGesture;
    if (!gesture) return;
    drawingGesture = null;
    if (cancel && gesture.stroke) clearStrokePreview(gesture.stroke);
    void finishCanvasDrawing(gesture, cancel).then((reply) => {
      if (!cancel) applyDrawingResult(gesture.tabID, reply);
      if (gesture.stroke) clearStrokePreview(gesture.stroke);
    }).catch((err: unknown) => {
      if (gesture.stroke) clearStrokePreview(gesture.stroke);
      toolError(gesture.kind === "shape" ? "形状绘制失败" : "渐变绘制失败", err);
    });
  }

  // Teardown or tab switches cancel outstanding gestures, including a Begin
  // RPC that has not returned yet. They must never leave the old tab busy.
  $effect(() => {
    void doc.id;
    void $activeTool;
    return () => {
      finishStroke(true);
      finishDrawing(true);
      clearStrokePreview();
      stopEdgeScroll();
      selectionGesture = null;
      sampling = false;
      samplePending = null;
      sampleRing = null;
      cancelTipDrag();
      brushPointer = null;
      hoverPoint = null;
    };
  });

  function queueSample(p: { x: number; y: number }, background: boolean): void {
    samplePending = { tabID: doc.id, ...p, background };
    if (sampleRunning) return;
    sampleRunning = true;
    void (async () => {
      try {
        while (samplePending) {
          const request = samplePending;
          samplePending = null;
          const reply = JSON.parse(await SampleCompositeColor(request.tabID, request.x, request.y)) as { color: Rgb | null };
          if (!reply.color || get(documentState).docId !== request.tabID) continue;
          const previous = get(palette)[request.background ? "background" : "foreground"];
          palette.update((value) => request.background ? setBackground(value, reply.color!) : setForeground(value, reply.color!));
          sampleRing = { x: request.x, y: request.y, color: reply.color, previous };
        }
      } catch (err) {
        samplePending = null;
        toolError("取色失败", err);
      } finally { sampleRunning = false; }
    })();
  }

  async function pickSelection(e: PointerEvent): Promise<void> {
    if (picking) return;
    picking = true;
    const tabID = doc.id;
    const p = docPixelOf(e);
    const mode = selectionModeFor(e);
    const settings = get(wandSettings);
    const object = get(toolModes).wand === "object";
    try {
      const raw = object
        ? await SelectObjectAt(p.x, p.y)
        : await SelectWandAt(tabID, p.x, p.y, settings.tolerance, settings.sampleRadius, settings.contiguous, settings.sampleAllLayers);
      const reply = JSON.parse(raw) as { tabId: string; selection: { w: number; h: number; mask: string } };
      if (get(documentState).docId !== tabID || reply.tabId !== tabID) return;
      const bytes = Uint8Array.from(atob(reply.selection.mask), (c) => c.charCodeAt(0));
      const region = selectionFromMask(bytes, reply.selection.w, reply.selection.h);
      currentSelection.set(select(mode, get(currentSelection), region));
    } catch (err) { toolError("选择失败", err); }
    finally { picking = false; }
  }

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
      x: Math.min(Math.max(doc.width - 1, 0), Math.max(0, Math.round(p.x))),
      y: Math.min(Math.max(doc.height - 1, 0), Math.max(0, Math.round(p.y))),
    };
  }

  /** Selection edges use the document boundary, unlike brush samples. */
  function selectionPointOf(e: PointerEvent): SelectionPoint {
    const p = docPointOf(e);
    return {
      x: Math.min(doc.width, Math.max(0, p.x)),
      y: Math.min(doc.height, Math.max(0, p.y)),
    };
  }

  function selectionModeFor(e: PointerEvent): SelectionMode {
    const configured = get(selectionSettings).combination;
    return e.shiftKey || e.altKey ? selectionMode(e.shiftKey, e.altKey) : configured;
  }

  function selectionRegionFor(gesture: SelectionGesture): SelectionRegion {
    if (gesture.kind === "marquee") {
      const opts = {
        square: false,
        fromCenter: false,
      };
      return gesture.shape === "ellipse"
        ? ellipseRegion(gesture.start, gesture.current, doc.width, doc.height, opts)
        : rectRegion(gesture.start, gesture.current, doc.width, doc.height, opts);
    }
    return lassoRegion(gesture.points, doc.width, doc.height);
  }

  const selectionPreview = $derived.by(() =>
    selectionGesture ? selectionRegionFor(selectionGesture) : null,
  );

  /**
   * 选区轮廓合成**一条**复合路径（各闭环子路径），再画两遍：白色实线打底、
   * 黑色虚线叠上去走动 —— 与原版 drawSelection（TransformOverlay.swift:296）
   * 同一结构。用复合路径而不是每个闭环一个 <path>，是因为像素边描迹在细节
   * 丰富的画面上能出上万个闭环，一闭环一节点会把 DOM 拖死。
   */
  const antsOutline = $derived.by(() =>
    ($currentSelection?.loops ?? []).map((loop) => loopPath(loop)).join(" "),
  );

  function beginSelection(e: PointerEvent): boolean {
    if (e.button !== 0) return false;
    if ($activeTool === "marquee") {
      const p = selectionPointOf(e);
      selectionGesture = {
        kind: "marquee",
        start: p,
        current: p,
        shape: get(toolModes).marquee,
        mode: selectionModeFor(e),
      };
    } else if ($activeTool === "lasso") {
      const p = selectionPointOf(e);
      const polygon = get(toolModes).lasso === "polygon";
      const current = selectionGesture;
      if (polygon && current?.kind === "lasso" && current.polygon) {
        current.points = [...current.points, p];
      } else {
        selectionGesture = {
          kind: "lasso",
          points: [p],
          polygon,
          mode: selectionModeFor(e),
        };
      }
    } else {
      return false;
    }
    stage?.setPointerCapture(e.pointerId);
    e.preventDefault();
    return true;
  }

  // 选区拖拽到视口边缘时自动滚动（票 30/S05）。`edgeAutoScroll` 是现成的纯函数，这里只负责在
  // 指针停在边缘时按帧推动视口 —— 鼠标不动时 pointermove 不再触发，所以必须自己起一个 rAF 循环，
  // 松手或离开余量就停。
  const EDGE_SCROLL_SPEED = 12; // 每帧最多推这么多像素

  let edgeScrollFrame = 0;
  let edgeScrollPoint = { clientX: 0, clientY: 0 };

  function stopEdgeScroll(): void {
    if (edgeScrollFrame) cancelAnimationFrame(edgeScrollFrame);
    edgeScrollFrame = 0;
  }

  function noteEdgeScroll(e: PointerEvent): void {
    edgeScrollPoint = { clientX: e.clientX, clientY: e.clientY };
    if (edgeScrollFrame || !stage) return;
    const tick = (): void => {
      edgeScrollFrame = 0;
      const gesture = selectionGesture;
      if (!gesture || gesture.kind !== "marquee" || !stage) return;
      const r = stage.getBoundingClientRect();
      const { clientX, clientY } = edgeScrollPoint;
      const left = edgeAutoScroll(clientX - r.left, EDGE_SCROLL_SPEED);
      const right = edgeAutoScroll(r.right - clientX, EDGE_SCROLL_SPEED);
      const top = edgeAutoScroll(clientY - r.top, EDGE_SCROLL_SPEED);
      const bottom = edgeAutoScroll(r.bottom - clientY, EDGE_SCROLL_SPEED);
      if (left === 0 && right === 0 && top === 0 && bottom === 0) return;
      panByStore(right - left, bottom - top);
      // 视口动了，框的端点要按同一指针位置重算，否则框会停在原处。
      const p = documentPoint(get(viewport), clientX - r.left, clientY - r.top, doc.width, doc.height);
      gesture.current = { x: Math.min(doc.width, Math.max(0, p.x)), y: Math.min(doc.height, Math.max(0, p.y)) };
      edgeScrollFrame = requestAnimationFrame(tick);
    };
    edgeScrollFrame = requestAnimationFrame(tick);
  }

  function finishSelection(): void {
    stopEdgeScroll();
    const gesture = selectionGesture;
    if (!gesture || (gesture.kind === "lasso" && gesture.polygon)) return;
    const region = selectionRegionFor(gesture);
    currentSelection.set(select(gesture.mode, get(currentSelection), region));
    selectionGesture = null;
  }

  function finishPolygonSelection(): void {
    stopEdgeScroll();
    const gesture = selectionGesture;
    if (!gesture || gesture.kind !== "lasso" || !gesture.polygon) return;
    const points = gesture.points.slice();
    const last = points[points.length - 1];
    const previous = points[points.length - 2];
    if (last && previous && Math.hypot(last.x - previous.x, last.y - previous.y) < 1) {
      points.pop();
    }
    if (points.length < 3) return;
    currentSelection.set(select(gesture.mode, get(currentSelection), lassoRegion(points, doc.width, doc.height)));
    selectionGesture = null;
  }

  // --- 变换会话（票 17）----------------------------------------------------
  // 会话状态机在 ../state/transform.ts。这里负责：把把手/框画出来、把手命中
  // 测试、把指针位移喂给会话、松手时提交（非破坏，只写 layer.transform）。

  /** 变换框把手命中半径（屏幕点）。 */
  const HANDLE_GRAB_PX = 8;

  /** 会话草稿的四个角（文档坐标，已含旋转）。null = 没有会话。 */
  const draftCorners = $derived.by(() => {
    const s = $transformSession;
    if (!s) return null;
    return transformCorners(s.session.draft);
  });

  /** 会话草稿的一个单位点 → 文档坐标。 */
  const rotationGrip = $derived($transformSession && !$transformSession.session.corners
    ? rotationHandle($transformSession.session.draft, ppp) : null);

  function draftPoint(unit: { x: number; y: number }): { x: number; y: number } {
    const s = get(transformSession);
    if (!s) return { x: 0, y: 0 };
    return transformedPoint(s.session.draft, unit);
  }

  /** 指针命中哪个把手；没命中返回 null。 */
  function handleAt(e: PointerEvent): number | null {
    if (!get(transformSession)) return null;
    const p = docPointOf(e);
    const ppp = pointsPerPixel(get(viewport));
    const tol = HANDLE_GRAB_PX / Math.max(ppp, 0.0001);
    // 角优先于边（HANDLES 是 角,边,角,边… 交替），所以按距离找最近的一个。
    let best: number | null = null;
    let bestDist = Infinity;
    for (let i = 0; i < HANDLES.length; i++) {
      const hp = draftPoint(HANDLES[i]);
      const d = Math.hypot(hp.x - p.x, hp.y - p.y);
      if (d <= tol && d < bestDist) {
        best = i;
        bestDist = d;
      }
    }
    return best;
  }

  /** 指针是否落在变换框内部（用于整体拖动）。 */
  function insideDraft(e: PointerEvent): boolean {
    const s = get(transformSession);
    if (!s) return false;
    return transformContains(s.session.draft, docPointOf(e));
  }

  /** 变换拖动状态：记录按下点与模式。 */
  let moveDrag: { startX: number; startY: number; commitOnRelease: boolean } | null = null;

  /**
   * 左键按下：把手 → resize，框内 → move。
   *
   * 关键顺序：**先判断这次按下属不属于变换，再开会话**。之前是反过来的
   * （先 `beginTransformSession()` 再判断），于是任何落在图层之外的按下
   * ——从标尺拖参考线、点空白画布——都会顺手开一个变换会话，画布上莫名
   * 多出变换框。
   */
  function beginMoveDrag(e: PointerEvent): boolean {
    const active = get(transformSession);
    const controls = active && get(showsTransformControls);
    const p = docPointOf(e);
    const grip = controls && !active.session.corners
      ? rotationHandle(active.session.draft, pointsPerPixel(get(viewport))) : null;
    const rotate = grip !== null && Math.hypot(p.x - grip.x, p.y - grip.y) * pointsPerPixel(get(viewport)) <= HANDLE_GRAB_PX;
    const handle = controls ? handleAt(e) : null;
    const inside = active ? insideDraft(e) : pressInsideActiveLayer(e);
    // 没抓到把手、又不在图层框内：这次按下不属于变换，交给别的工具/平移逻辑。
    if (!rotate && handle === null && !inside) return false;
    // 到这里才确定要变换：还没有会话就开一个（V 工具直接拖图层 = 移动它）。
    if (!active && !beginTransformSession()) return false;
    const mode: DragMode = rotate ? { kind: "rotate" } : handle !== null ? { kind: "resize", handle } : { kind: "move" };
    if (!beginTransformDrag(mode, p)) return false;
    moveDrag = { startX: p.x, startY: p.y, commitOnRelease: !active };
    return true;
  }

  /**
   * 没有会话时，指针是否落在**活动图层**的框内。用图层自己的 transform
   * 判断，不先开会话 —— 这样"点图层内 = 开始移动"、"点图层外 = 什么都不做"。
   */
  function pressInsideActiveLayer(e: PointerEvent): boolean {
    const doc0 = get(documentState);
    const layer = doc0.layers.find((l) => l.id === doc0.activeLayerID);
    if (!layer || layer.isGroup) return false;
    const t = layer.transform;
    const box = {
      origin: [t.origin[0], t.origin[1]] as [number, number],
      size: [t.size[0], t.size[1]] as [number, number],
      rotation: t.rotation ?? 0,
      flipX: false,
      flipY: false,
      sampling: "High quality" as const,
    };
    return transformContains(box, docPointOf(e));
  }

  /** 拖动中：把文档空间位移交给会话。 */
  function updateMoveDrag(e: PointerEvent): void {
    if (!moveDrag) return;
    const p = docPointOf(e);
    dragTransform(
      { x: p.x - moveDrag.startX, y: p.y - moveDrag.startY },
      { shift: e.shiftKey, option: e.altKey, ctrl: e.ctrlKey },
      canvasSnapInput(get(documentState), get(transformSession)?.layerID ?? "", pointsPerPixel(get(viewport)), e.ctrlKey),
    );
  }

  /** V-tool moves commit on release; explicit Ctrl+T drafts wait for Enter. */
  function endMoveDrag(e: PointerEvent): void {
    if (!moveDrag) return;
    updateMoveDrag(e);
    const commitOnRelease = moveDrag.commitOnRelease;
    moveDrag = null;
    stage?.releasePointerCapture(e.pointerId);
    if (commitOnRelease) void commitTransformSession();
  }

  // --- 参考线（票 14）------------------------------------------------------
  // 状态机与开关在 ../state/view.ts；这里只做两件事：把指针位置换成文档
  // 坐标喂给状态机，以及把列表画出来。参考线的真源仍是 Go 的
  // domain.Document.guides —— 每次提交由 shortcuts/actions.ts 调 SetGuides。

  /** 抓住参考线时的抓取半径（屏幕点，原版 TransformSnap.distance 同量级）。 */
  const GUIDE_GRAB_PX = 6;

  /** 正在从标尺拖出一条新参考线。 */
  let rulerDrag: { axis: "horizontal" | "vertical" } | null = null;

  /** 指针 → 文档坐标（允许越界，参考线可以拖到画布外）。 */
  function docPointOf(e: PointerEvent): { x: number; y: number } {
    const r = stage!.getBoundingClientRect();
    return documentPoint(
      get(viewport),
      e.clientX - r.left,
      e.clientY - r.top,
      doc.width,
      doc.height,
    );
  }

  /**
   * 命中测试：指针是否落在某条参考线上（屏幕距离 ≤ GUIDE_GRAB_PX）。
   * 锁定时返回 null —— 锁住的参考线不参与抓取。
   */
  function guideAt(e: PointerEvent): { id: string; axis: "horizontal" | "vertical" } | null {
    if (get(lockGuides)) return null;
    const p = docPointOf(e);
    const ppp = pointsPerPixel(get(viewport));
    const tolerance = GUIDE_GRAB_PX / Math.max(ppp, 0.0001);
    for (const g of get(visibleGuides)) {
      const d = g.axis === "vertical" ? Math.abs(p.x - g.position) : Math.abs(p.y - g.position);
      if (d <= tolerance) return { id: g.id, axis: g.axis };
    }
    return null;
  }

  /**
   * 把当前参考线写回 Go（真源在文档里）。真正的 bridge 调用收敛在
   * shortcuts/actions.ts 的 commitGuides —— 组件只负责拖动交互，不直接谈协议。
   */
  function commitGuides(): void {
    void commitGuidesAction(guidesJSON());
  }

  /** 标尺按下：开始拖出一条新参考线（票 14 的"从标尺拖出"）。 */
  function startRulerDrag(e: PointerEvent, axis: "horizontal" | "vertical"): void {
    if (e.button !== 0) return;
    const p = docPointOf(e);
    const position = axis === "vertical" ? p.x : p.y;
    if (!beginGuideCreation(axis, position)) return;
    rulerDrag = { axis };
    stage?.setPointerCapture(e.pointerId);
    e.preventDefault();
  }

  function onPointerDown(e: PointerEvent): void {
    if (strokeGesture || drawingGesture || tipDrag) return;
    const spaceHeld = ($modifierBits & Modifier.Space) !== 0;
    if (e.button === 2 && BRUSH_TOOLS.has($activeTool)) {
      const bounds = stage!.getBoundingClientRect();
      brushPointer = { x: e.clientX - bounds.left, y: e.clientY - bounds.top };
      tipDrag = { pointerID: e.pointerId, healing: $activeTool === "spotHealing",
        drag: beginBrushTipDrag(brushPointer.x, brushPointer.y, tipSettings.diameter, tipSettings.hardness) };
      stage?.setPointerCapture(e.pointerId);
      e.preventDefault();
      return;
    }
    // 行内文字编辑器优先接管左键按下（放置光标、拖选）：让 textarea 拿到
    // 焦点，不让 Type 工具重开会话；空格/手形工具的平移仍然优先。
    if (e.button === 0 && !spaceHeld && $activeTool !== "hand" && isTextEditorTarget(e.target)) return;
    // 变换会话开着时，画布上的左键属于它（把手 = 缩放，框内 = 移动）。
    if (e.button === 0 && !spaceHeld && $activeTool !== "hand" && get(transformSession)) {
      if (beginMoveDrag(e)) {
        stage?.setPointerCapture(e.pointerId);
        e.preventDefault();
        return;
      }
    }
    // V（移动/变换）工具：左键拖拽搬动图层。原先这里被当成画笔，落笔就画黑笔。
    if (e.button === 0 && !spaceHeld && $activeTool === "move") {
      if (beginMoveDrag(e)) {
        stage?.setPointerCapture(e.pointerId);
        e.preventDefault();
        return;
      }
    }
    // 参考线优先于工具：抓住一条线就是移动它（票 14）。空格/手形平移仍然优先，
    // 所以只在没有平移意图时判定。
    if (e.button === 0 && !spaceHeld && $activeTool !== "hand" && get(showGuides)) {
      const hit = guideAt(e);
      if (hit && beginGuideMove(hit.id)) {
        stage?.setPointerCapture(e.pointerId);
        e.preventDefault();
        return;
      }
    }
    // An armed Levels eyedropper swallows plain clicks before any tool.
    if (e.button === 0 && !panDrag && !strokeGesture) {
      const p = docPixelOf(e);
      if (eyedropperClick(Math.floor(p.x), Math.floor(p.y))) {
        e.preventDefault();
        return;
      }
    }
    if (e.button === 1 && (e.ctrlKey || e.metaKey)) {
      // Middle + Command/Control zooms about the press point, as in Blender;
      // it keeps its own anchor so it cannot disturb a left-button drag.
      const r = stage!.getBoundingClientRect();
      middleZoom = {
        anchorX: e.clientX - r.left,
        anchorY: e.clientY - r.top,
        zoom: get(viewport).zoom,
      };
      middleZoomOut = false;
      stage?.setPointerCapture(e.pointerId);
      e.preventDefault();
      return;
    }
    if (e.button === 1 || ((spaceHeld || $activeTool === "hand") && e.button === 0)) {
      panDrag = { x: e.clientX, y: e.clientY };
      panning = true;
      stage?.setPointerCapture(e.pointerId);
      e.preventDefault();
      return;
    }
    // 票 73：渐进模糊打开时左键归它（原版 beginProgressiveDrag，
    // EditorCanvas.swift:2463-2474）：抓住某个端点就拖它，按在别处则**重开一条
    // 线**（两端先落在指针上，接着拖终点）—— 「在画布上拖出两点」是一次手势。
    if (e.button === 0 && progressiveOverlay) {
      const tolerance = PROGRESSIVE_HANDLE_RADIUS / Math.max(pointsPerPixel(get(viewport)), 0.0001);
      const begun = beginProgressiveDrag(docPointOf(e), progressiveOverlay, tolerance);
      if (begun.line !== progressiveOverlay) {
        updateFilterSettings({
          progressiveStart: [begun.line.start.x, begun.line.start.y],
          progressiveEnd: [begun.line.end.x, begun.line.end.y],
        });
      }
      progressiveDrag = begun.handle;
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
    if (e.button === 0 && $activeTool === "eyedropper") {
      sampling = true;
      stage?.setPointerCapture(e.pointerId);
      e.preventDefault();
      queueSample(docPixelOf(e), e.altKey);
      return;
    }
    if (e.button === 0 && $activeTool === "wand") {
      e.preventDefault();
      void pickSelection(e);
      return;
    }
    // Selection tools own the pointer before guides, painting, or layer
    // transforms. Polygon lasso stays open across clicks and closes on a
    // double-click; the other gestures commit on pointer-up.
    if (e.button === 0 && ($activeTool === "marquee" || $activeTool === "lasso")) {
      beginSelection(e);
      return;
    }
    if (e.button === 0 && $activeTool === "type") {
      const p = docPixelOf(e);
      if (!editTextLayerAt(p.x, p.y)) {
        textDrag = { x: p.x, y: p.y };
      }
      stage?.setPointerCapture(e.pointerId);
      e.preventDefault();
      return;
    }
    if (e.button === 0 && ($activeTool === "shape" || $activeTool === "gradient")) {
      drawingGesture = { ...beginCanvasDrawing($activeTool, doc.id, docPointOf(e)), pointerID: e.pointerId };
      if (drawingGesture.stroke) {
        clearStrokePreview();
        const gesture = drawingGesture;
        const stroke = gesture.stroke!;
        previewOwner = stroke;
        stopStrokePreview = stroke.onProgress((progress) => {
          if (previewOwner === stroke && get(documentState).docId === gesture.tabID) strokePreview.update(progress);
        });
      }
      stage?.setPointerCapture(e.pointerId);
      e.preventDefault();
      return;
    }
    if (e.button === 0 && ($activeTool === "brush" || $activeTool === "spotHealing" || $activeTool === "cloneStamp" || $activeTool === "blur")) {
      const point = docPointOf(e);
      if ($activeTool === "cloneStamp" && e.altKey) {
        setCanvasCloneSource(doc.id, point);
        hoverPoint = point;
        e.preventDefault();
        return;
      }
      const layerID = get(documentState).activeLayerID;
      const targetMask = getCanvasEditTarget(doc.id)?.targetMask ?? false;
      const lineStart = $activeTool === "brush" && e.shiftKey && lastBrushPoint?.tabID === doc.id && lastBrushPoint.layerID === layerID && lastBrushPoint.targetMask === targetMask
        ? lastBrushPoint.point : undefined;
      const name = $activeTool === "spotHealing" ? "污点修复失败" : $activeTool === "cloneStamp" ? "仿制失败" : $activeTool === "blur" ? "涂抹失败" : "绘制失败";
      try {
        strokeGesture = { pointerID: e.pointerId, tabID: doc.id, layerID, targetMask, brush: $activeTool === "brush", name,
          stroke: beginCanvasStroke($activeTool, doc.id, point, lineStart) };
        clearStrokePreview();
        const gesture = strokeGesture;
        previewOwner = gesture.stroke;
        stopStrokePreview = gesture.stroke.onProgress((progress) => {
          if (previewOwner === gesture.stroke && get(documentState).docId === gesture.tabID) {
            strokePreview.update(progress);
          }
        });
      } catch (err) { toolError(name, err); return; }
      stage?.setPointerCapture(e.pointerId);
      e.preventDefault();
    }
  }

  function onPointerMove(e: PointerEvent): void {
    const bounds = stage!.getBoundingClientRect();
    brushPointer = { x: e.clientX - bounds.left, y: e.clientY - bounds.top };
    hoverPoint = docPointOf(e);
    if (tipDrag) {
      if (tipDrag.pointerID !== e.pointerId) return;
      tipHud = dragBrushTip(tipDrag.drag, brushPointer.x, ppp, e.shiftKey);
      const settings = { diameter: tipHud.diameter, hardness: tipHud.hardness };
      if (tipDrag.healing) updateHealSettings(settings);
      else updateBrushSettings(settings);
      return;
    }
    if (strokeGesture) {
      if (strokeGesture.pointerID !== e.pointerId) return;
      const events = e.getCoalescedEvents?.() ?? [];
      for (const point of events.length ? events : [e]) {
        const p = docPointOf(point);
        strokeGesture.stroke.point(p.x, p.y);
      }
      return;
    }
    if (drawingGesture) {
      if (drawingGesture.pointerID === e.pointerId) {
        moveCanvasDrawing(drawingGesture, docPointOf(e), { shift: e.shiftKey, alt: e.altKey });
      }
      return;
    }
    if (sampling) {
      queueSample(docPixelOf(e), e.altKey);
      return;
    }
    if (progressiveDrag && progressiveOverlay) {
      const moved = moveProgressiveHandle(progressiveOverlay, progressiveDrag, docPointOf(e), e.shiftKey);
      updateFilterSettings({
        progressiveStart: [moved.start.x, moved.start.y],
        progressiveEnd: [moved.end.x, moved.end.y],
      });
      return;
    }
    if (middleZoom) {
      const r = stage!.getBoundingClientRect();
      const factor = middleZoomFactor(middleZoom.anchorY, e.clientY - r.top);
      middleZoomOut = factor < 1;
      zoomAt(middleZoom.zoom * factor, middleZoom.anchorX, middleZoom.anchorY, doc.width, doc.height);
      return;
    }
    if (panDrag) {
      panByStore(e.clientX - panDrag.x, e.clientY - panDrag.y);
      panDrag = { x: e.clientX, y: e.clientY };
      return;
    }
    // 变换拖动优先于其它工具。
    if (moveDrag) {
      updateMoveDrag(e);
      return;
    }
    if (selectionGesture) {
      if (selectionGesture.kind === "marquee") {
        selectionGesture.current = selectionPointOf(e);
        noteEdgeScroll(e);
      } else if (!selectionGesture.polygon) {
        const p = selectionPointOf(e);
        const last = selectionGesture.points[selectionGesture.points.length - 1];
        if (!last || Math.hypot(p.x - last.x, p.y - last.y) >= 0.5) {
          selectionGesture.points = [...selectionGesture.points, p];
        }
      }
      return;
    }
    // 参考线拖动：位置用文档坐标（可以拖到画布外，松手丢回标尺就是删除）。
    if ($draggingGuide) {
      const p = docPointOf(e);
      // 拖动中的那条线的方向决定读 x 还是 y；rulerDrag 只覆盖"刚从标尺拖出"，
      // 移动已有参考线时从状态机里读方向。
      const axis = rulerDrag?.axis ?? guidesState.drag?.axis;
      moveGuideDrag(axis === "horizontal" ? p.y : p.x);
      return;
    }
    if (textDrag) {
      const p = docPixelOf(e);
      textDragRect = {
        x: Math.min(textDrag.x, p.x),
        y: Math.min(textDrag.y, p.y),
        w: Math.abs(p.x - textDrag.x),
        h: Math.abs(p.y - textDrag.y),
      };
      return;
    }
  }

  function onPointerEnd(e: PointerEvent): void {
    if (tipDrag) {
      if (tipDrag.pointerID !== e.pointerId) return;
      tipDrag = null;
      tipHud = null;
      if (stage?.hasPointerCapture(e.pointerId)) stage.releasePointerCapture(e.pointerId);
      return;
    }
    if (strokeGesture) {
      if (strokeGesture.pointerID !== e.pointerId) return;
      const p = docPointOf(e);
      strokeGesture.stroke.point(p.x, p.y);
      if (strokeGesture.brush) lastBrushPoint = { tabID: strokeGesture.tabID, layerID: strokeGesture.layerID, targetMask: strokeGesture.targetMask, point: p, gesture: strokeGesture.stroke };
      finishStroke(false);
      if (stage?.hasPointerCapture(e.pointerId)) stage.releasePointerCapture(e.pointerId);
      return;
    }
    if (drawingGesture) {
      if (drawingGesture.pointerID !== e.pointerId) return;
      const gesture = drawingGesture;
      moveCanvasDrawing(gesture, docPointOf(e), { shift: e.shiftKey, alt: e.altKey });
      if (stage?.hasPointerCapture(e.pointerId)) stage.releasePointerCapture(e.pointerId);
      finishDrawing(false);
      return;
    }
    if (sampling) {
      sampling = false;
      if (stage?.hasPointerCapture(e.pointerId)) stage.releasePointerCapture(e.pointerId);
      return;
    }
    if (moveDrag) {
      endMoveDrag(e);
      return;
    }
    if (selectionGesture) {
      const polygon = selectionGesture.kind === "lasso" && selectionGesture.polygon;
      if (!polygon) finishSelection();
      if (stage?.hasPointerCapture(e.pointerId)) stage.releasePointerCapture(e.pointerId);
      return;
    }
    if ($draggingGuide) {
      const p = docPointOf(e);
      // 只有"松手时指针已在画布外"才算丢回标尺 = 删除。从标尺拖出来但最终
      // 落在画布上，是正常的"新建参考线"，不能删。
      const outside = p.x < 0 || p.y < 0 || p.x > doc.width || p.y > doc.height;
      rulerDrag = null;
      stage?.releasePointerCapture(e.pointerId);
      const edit = finishGuideDrag(outside);
      if (edit) void commitGuides();
      return;
    }
    if (textDrag) {
      const start = textDrag;
      const rect = textDragRect;
      textDrag = null;
      textDragRect = null;
      stage?.releasePointerCapture(e.pointerId);
      // A drag of at least 4 px makes a paragraph box; a click is point text.
      if (rect && rect.w >= 4 && rect.h >= 4) {
        startBoxText(rect.x, rect.y, rect.w, rect.h);
      } else {
        startPointText(start.x, start.y);
      }
      return;
    }
    if (panDrag || middleZoom || progressiveDrag) stage?.releasePointerCapture(e.pointerId);
    panDrag = null;
    panning = false;
    middleZoom = null;
    middleZoomOut = false;
    progressiveDrag = null;
  }

  function onPointerCancel(e: PointerEvent): void {
    if (strokeGesture && strokeGesture.pointerID !== e.pointerId) return;
    if (drawingGesture && drawingGesture.pointerID !== e.pointerId) return;
    if (tipDrag && tipDrag.pointerID !== e.pointerId) return;
    finishStroke(true);
    cancelTipDrag();
    finishDrawing(true);
    selectionGesture = null;
    textDrag = null;
    textDragRect = null;
    if (moveDrag) { moveDrag = null; cancelTransformSession(); }
    if (get(draggingGuide)) cancelGuideDrag();
    rulerDrag = null;
    sampling = false;
    samplePending = null;
    panDrag = null;
    panning = false;
    middleZoom = null;
    middleZoomOut = false;
    progressiveDrag = null;
    if (stage?.hasPointerCapture(e.pointerId)) stage.releasePointerCapture(e.pointerId);
  }

  function cancelTipDrag(): void {
    if (tipDrag) {
      const settings = { diameter: tipDrag.drag.diameter, hardness: tipDrag.drag.hardness };
      if (tipDrag.healing) updateHealSettings(settings);
      else updateBrushSettings(settings);
    }
    tipDrag = null;
    tipHud = null;
  }

  function onContextMenu(e: MouseEvent): void {
    e.preventDefault(); // Alt-click / right-click belong to the tools
  }

  function onDoubleClick(e: MouseEvent): void {
    if ($activeTool !== "lasso" || !selectionGesture) return;
    e.preventDefault();
    finishPolygonSelection();
  }

  const isTextEditorTarget = (t: EventTarget | null): boolean =>
    t instanceof Node && !!textEditor && textEditor.contains(t);
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
  onpointercancel={onPointerCancel}
  onpointerleave={() => { if (!strokeGesture && !tipDrag) { brushPointer = null; hoverPoint = null; } }}
  ondblclick={onDoubleClick}
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
    {#if renderSrc && !renderBroken && !gpuVisible && !strokePreviewSrc}
      <img
        class="render"
        class:crisp={vp.zoom >= CRISP_ZOOM}
        src={renderSrc}
        alt=""
        draggable="false"
        onerror={() => (renderBroken = true)}
      />
    {/if}
    <canvas bind:this={gpuCanvas} class="render" class:crisp={vp.zoom >= CRISP_ZOOM} class:gpu-hidden={!gpuVisible} aria-hidden="true"></canvas>
    {#if strokePreviewSrc}
      <img class="render" class:crisp={vp.zoom >= CRISP_ZOOM} src={strokePreviewSrc} alt="" draggable="false" />
    {/if}
    {#if $activeTool === "cloneStamp" && cloneSample}
      <svg class="clone-source" viewBox="0 0 {doc.width} {doc.height}" preserveAspectRatio="none" aria-hidden="true">
        <path d="M {cloneSample.x - 6 / ppp} {cloneSample.y} h {12 / ppp} M {cloneSample.x} {cloneSample.y - 6 / ppp} v {12 / ppp}" />
      </svg>
    {/if}
    {#if drawingGesture}
      {@const start = drawingGesture.start}
      {@const end = drawingGesture.end}
      <svg class="drawing-preview" viewBox="0 0 {doc.width} {doc.height}" preserveAspectRatio="none">
        {#if drawingGesture.kind === "gradient" || drawingGesture.shape === "Line"}
          <line x1={start.x} y1={start.y} x2={end.x} y2={end.y} />
        {:else if drawingGesture.shape === "Ellipse"}
          <ellipse cx={(start.x + end.x) / 2} cy={(start.y + end.y) / 2} rx={Math.abs(end.x - start.x) / 2} ry={Math.abs(end.y - start.y) / 2} />
        {:else}
          <rect x={Math.min(start.x, end.x)} y={Math.min(start.y, end.y)} width={Math.abs(end.x - start.x)} height={Math.abs(end.y - start.y)} />
        {/if}
      </svg>
    {/if}
    {#if $currentSelection && $currentSelection.active}
      <!-- 蚂蚁线（票 15/S02）：白色实线打底 + 走动的黑色虚线，白底上也看得见；
           全部闭环合成一条复合路径，所以再细的描迹也只占两个 DOM 节点。 -->
      <svg class="ants" viewBox="0 0 {doc.width} {doc.height}" preserveAspectRatio="none" aria-hidden="true">
        <path class="ants-line" d={antsOutline} />
        <path class="ants-dashes" d={antsOutline} />
      </svg>
    {/if}
    {#if selectionPreview}
      <svg class="selection-preview" viewBox="0 0 {doc.width} {doc.height}" preserveAspectRatio="none">
        {#each selectionPreview.loops as loop, li (li)}
          <path d={loopPath(loop)} />
        {/each}
      </svg>
    {/if}
    {#if sampleRing && $showsSampleRing && $activeTool === "eyedropper"}
      <div
        class="sample-ring"
        style:left="{sampleRing.x * ppp}px"
        style:top="{sampleRing.y * ppp}px"
        style:border-top-color="#{rgbToHex(sampleRing.color)}"
        style:border-bottom-color="#{rgbToHex(sampleRing.previous)}"
        aria-hidden="true"
      ></div>
    {/if}
    {#if showGrid}
      <div
        class="pixel-grid"
        style:background-size="{ppp}px {ppp}px"
      ></div>
    {/if}
    {#if $showLayoutGrid}
      <!-- 布局网格（票 14）：细线 = 细分，粗线 = 主格。背景尺寸按 ppp 缩放，
           所以缩放画布时网格跟着文档走。 -->
      <div
        class="layout-grid"
        class:major-only={!gridMinorVisible}
        style:background-size="{gridStepPx}px {gridStepPx}px, {gridStepPx}px {gridStepPx}px, {gridMajorPx}px {gridMajorPx}px, {gridMajorPx}px {gridMajorPx}px"
      ></div>
    {/if}
    {#if $transformSession && $showsTransformControls}
      <!-- 变换控件（票 17）：框 + 8 个把手。坐标是文档像素，和 .doc 同系，
           所以 viewBox 直接用 doc.width/height，preserveAspectRatio=none
           让旋转后的四边形也跟着非等比缩放走（.doc 尺寸 = 文档尺寸 × ppp）。 -->
      <svg
        class="transform-box"
        viewBox="0 0 {doc.width} {doc.height}"
        preserveAspectRatio="none"
      >
        {#if draftCorners}
          {#each $transformSnapGuides as guide, i (i)}
            <line class="snap-guide" x1={guide.axis === "vertical" ? guide.position : 0}
              y1={guide.axis === "horizontal" ? guide.position : 0}
              x2={guide.axis === "vertical" ? guide.position : doc.width}
              y2={guide.axis === "horizontal" ? guide.position : doc.height} />
          {/each}
          <polygon
            class="transform-frame"
            points={draftCorners.map((c) => `${c.x},${c.y}`).join(" ")}
          />
          {#each HANDLES as handle, i (i)}
            {@const hp = draftPoint(handle)}
            <circle class="transform-handle" cx={hp.x} cy={hp.y} r={4 / Math.max(ppp, 0.0001)} />
          {/each}
          {#if rotationGrip}
            {@const top = draftPoint(HANDLES[1])}
            <line class="transform-frame" x1={top.x} y1={top.y} x2={rotationGrip.x} y2={rotationGrip.y} />
            <circle class="transform-handle rotation-handle" cx={rotationGrip.x} cy={rotationGrip.y} r={4 / Math.max(ppp, 0.0001)} />
          {/if}
        {/if}
      </svg>
    {/if}
    {#if progressiveOverlay}
      <!-- 票 73：渐进模糊的线。viewBox 用文档像素（与 .doc 同系），所以端点直接
           是文档坐标；线宽/手柄半径除以 ppp 保持屏幕尺寸不随缩放变粗。 -->
      <svg class="progressive-line" viewBox="0 0 {doc.width} {doc.height}" preserveAspectRatio="none" aria-hidden="true">
        <line
          x1={progressiveOverlay.start.x}
          y1={progressiveOverlay.start.y}
          x2={progressiveOverlay.end.x}
          y2={progressiveOverlay.end.y}
          vector-effect="non-scaling-stroke"
        />
        <circle class="progressive-handle" cx={progressiveOverlay.start.x} cy={progressiveOverlay.start.y} r={PROGRESSIVE_HANDLE_RADIUS / Math.max(ppp, 0.0001)} />
        <circle class="progressive-handle" cx={progressiveOverlay.end.x} cy={progressiveOverlay.end.y} r={PROGRESSIVE_HANDLE_RADIUS / Math.max(ppp, 0.0001)} />
      </svg>
    {/if}
    {#if $showGuides && $visibleGuides.length > 0}
      <!-- 参考线（票 14）：位置是文档像素，乘 ppp 落到 .doc 的 CSS 像素。
           拖动中的那条由 view.ts 的 displayedGuides 一并给出，所以跟手。 -->
      <div class="guides" aria-hidden="true">
        {#each $visibleGuides as guide (guide.id)}
          {#if guide.axis === "vertical"}
            <div
              class="guide vertical"
              class:locked={$lockGuides}
              style:left="{guide.position * ppp}px"
            ></div>
          {:else}
            <div
              class="guide horizontal"
              class:locked={$lockGuides}
              style:top="{guide.position * ppp}px"
            ></div>
          {/if}
        {/each}
      </div>
    {/if}
    {#if textDragRect}
      <!-- 拖拽框（文档像素）按 ppp 换算到 .doc 的 CSS 像素，缩放时跟手。 -->
      <div
        class="text-box-drag"
        style:left="{textDragRect.x * ppp}px"
        style:top="{textDragRect.y * ppp}px"
        style:width="{textDragRect.w * ppp}px"
        style:height="{textDragRect.h * ppp}px"
      ></div>
    {/if}
    {#if $textSession}
      <!-- 行内编辑器：画布上的原生 textarea，IME 由 WebView2 原生合成。
           提交/取消（⌘回车 / Esc）与方向键间距归快捷表层（票 47），这里
           不再有 keydown 处理。几何按 ppp 换算——editorAt/boxSize/字号/行高
           是文档像素，.doc 已按 doc.width * ppp 定位与缩放，乘上 ppp 后
           任意缩放级别都落在同一个文档点上。纯 textarea 画不出逐 run 的
           混排颜色/字体（浏览器限制），提交后由 Go 渲染端按 colorRuns/
           fontRuns 画出完整结果——这里只是基础样式的预览。 -->
      {@const tbox = editorBoxSize($textSession)}
      <textarea
        bind:this={textEditor}
        class="text-editor"
        style:left="{$textSession.editorAt.x * ppp}px"
        style:top="{$textSession.editorAt.y * ppp}px"
        style:width="{tbox[0] * ppp}px"
        style:height="{tbox[1] * ppp}px"
        style:font-size="{$textSession.style.fontSize * ppp}px"
        style:line-height="{($textSession.style.leading > 0
          ? $textSession.style.leading
          : $textSession.style.fontSize * 1.2) * ppp}px"
        style:letter-spacing="{$textSession.style.tracking * ppp}px"
        style:text-align={$textSession.style.alignment.toLowerCase()}
        style:color={textColor()}
        style:font-family={$textSession.style.fontName}
        value={$textSession.content}
        onbeforeinput={onTextBeforeInput}
        oninput={onTextInput}
        onselect={syncSelection}
        onkeyup={syncSelection}
        onmouseup={syncSelection}
        onblur={onTextBlur}
        placeholder="输入文字…"
      ></textarea>
      <!-- 8 向缩放手柄：角/边跟手改段落框；点文本第一次拖手柄先物化成框。 -->
      <div
        class="text-frame"
        style:left="{$textSession.editorAt.x * ppp}px"
        style:top="{$textSession.editorAt.y * ppp}px"
        style:width="{tbox[0] * ppp}px"
        style:height="{tbox[1] * ppp}px"
      >
        {#each TEXT_BOX_HANDLES as handle (handle)}
          <div
            class="text-handle {handle}"
            role="presentation"
            onpointerdown={(e) => startBoxResize(e, handle)}
            onpointermove={onBoxResizeMove}
            onpointerup={endBoxResize}
            onpointercancel={endBoxResize}
          ></div>
        {/each}
      </div>
    {/if}
  </div>

  <!-- 裁剪覆盖层（票 18）：自包含组件，自己监听 activeTool === "crop" 开会话，
       所以挂上就够、不需要额外接线。必须挂在 .doc **外面**（它的 inset:0 要等于
       舞台盒子，文档→屏幕映射走 documentRect(viewport)）。 -->
  <CropOverlay doc={{ id: doc.id, width: doc.width, height: doc.height }} />

  {#if tipCircle}
    <div class="brush-tip" style:left="{tipCircle.x}px" style:top="{tipCircle.y}px" style:width="{tipCircle.diameter}px" style:height="{tipCircle.diameter}px" aria-hidden="true">
      {#if tipHud?.hardnessShown && tipHud.hardness > 0}
        <div class="hardness-ring" style:width="{tipHud.hardness * 100}%" style:height="{tipHud.hardness * 100}%"></div>
      {/if}
      {#if tipHud}
        <span class="tip-hud">{tipHud.mode === "size" ? `${Math.round(tipHud.diameter)} px` : `${Math.round(tipHud.hardness * 100)}%`}</span>
      {/if}
    </div>
  {/if}

  <!-- Navigator（票 61）：自己决定何时显示（视图 ▸ 导航器且缩放 ≥ 300%），
       所以这里无条件挂载；它读同一个 viewport store 平移画布。 -->
  <Navigator src={navigatorSrc} docWidth={doc.width} docHeight={doc.height} />

  {#if $showRulers}
    <!-- 标尺（票 14）：从标尺拖出参考线。刻度按文档坐标标号，主刻度随缩放
         自适应（rulerStep），零点是文档左上角 —— 和 .doc 的 rect 对齐。 -->
    <div
      class="ruler horizontal"
      role="presentation"
      onpointerdown={(e) => startRulerDrag(e, "horizontal")}
    >
      {#each gridLines({ spacing: rulerStep, subdivisions: 1 }, Math.max(doc.width, 0)) as tick (tick)}
        {#if tick * ppp + rect.x > -60 && tick * ppp + rect.x < viewWidth + 60}
          <div class="tick" style:left="{tick * ppp}px"></div>
          <div class="tick-label" style:left="{tick * ppp}px">{tick}</div>
        {/if}
      {/each}
    </div>
    <div
      class="ruler vertical"
      role="presentation"
      onpointerdown={(e) => startRulerDrag(e, "vertical")}
    >
      {#each gridLines({ spacing: rulerStep, subdivisions: 1 }, Math.max(doc.height, 0)) as tick (tick)}
        {#if tick * ppp + rect.y > -60 && tick * ppp + rect.y < viewHeight + 60}
          <div class="tick" style:top="{tick * ppp}px"></div>
        {/if}
      {/each}
    </div>
  {/if}
</div>

<style>
  .brush-tip {
    position: absolute;
    box-sizing: border-box;
    border: 1px solid #fff;
    border-radius: 50%;
    box-shadow: 0 0 0 1px #000;
    pointer-events: none;
    z-index: 10;
  }
  .hardness-ring {
    position: absolute;
    box-sizing: border-box;
    left: 50%;
    top: 50%;
    transform: translate(-50%, -50%);
    border: 1px dashed #fff;
    border-radius: 50%;
  }
  .tip-hud {
    position: absolute;
    left: 50%;
    top: 100%;
    margin-top: 8px;
    padding: 3px 6px;
    color: #fff;
    background: #202024;
    border-radius: 3px;
    white-space: nowrap;
  }
  .clone-source {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    pointer-events: none;
    stroke: #fff;
    filter: drop-shadow(1px 1px 0 #000);
  }
  .clone-source path { vector-effect: non-scaling-stroke; }
  .sample-ring {
    position: absolute;
    width: 40px;
    height: 40px;
    border: 10px solid #888;
    border-radius: 50%;
    transform: translate(-50%, -50%);
    box-shadow: 0 0 0 1px #000, inset 0 0 0 1px #fff;
    pointer-events: none;
    z-index: 8;
  }
  .text-box-drag {
    position: absolute;
    border: 1px dashed var(--accent, #3b82f6);
    background: rgba(59, 130, 246, 0.08);
    pointer-events: none;
  }

  /* 与 Go 光栅端对齐：TextPadding 是 12（layerrender/text.go），所以内容区
     也留 12px，盒内宽度 = boxSize − 24，提交前后换行位置一致；字距与对齐由
     letter-spacing / text-align 反映（原版每帧重光栅所以天然所见即所得）。 */
  .text-editor {
    position: absolute;
    box-sizing: border-box;
    background: rgba(255, 255, 255, 0.04);
    border: 1px dashed var(--accent, #3b82f6);
    outline: none;
    resize: none;
    overflow: hidden;
    padding: 12px;
    white-space: pre-wrap;
    caret-color: var(--accent, #3b82f6);
  }

  /* 段落框的手柄层：overlay 不吃事件，只有 8 个方块吃（原版 6pt 手柄）。 */
  .text-frame {
    position: absolute;
    pointer-events: none;
  }

  .text-handle {
    position: absolute;
    width: 8px;
    height: 8px;
    margin: -4px 0 0 -4px;
    background: var(--bg-panel, #2a2a2e);
    border: 1px solid var(--accent, #3b82f6);
    border-radius: 1px;
    pointer-events: auto;
  }

  .text-handle.nw { left: 0; top: 0; cursor: nwse-resize; }
  .text-handle.n  { left: 50%; top: 0; cursor: ns-resize; }
  .text-handle.ne { left: 100%; top: 0; cursor: nesw-resize; }
  .text-handle.w  { left: 0; top: 50%; cursor: ew-resize; }
  .text-handle.e  { left: 100%; top: 50%; cursor: ew-resize; }
  .text-handle.sw { left: 0; top: 100%; cursor: nesw-resize; }
  .text-handle.s  { left: 50%; top: 100%; cursor: ns-resize; }
  .text-handle.se { left: 100%; top: 100%; cursor: nwse-resize; }

  .canvas-stage {
    position: absolute;
    inset: 0;
    overflow: hidden; /* pan is handled by the viewport, not by scrolling */
    touch-action: none;
    user-select: none;
    background: var(--bg-canvas, #232327);
  }

  /* 布局网格（View > Show Grid）：两组细线 + 两组主线，用渐变当线条。 */
  .layout-grid {
    position: absolute;
    inset: 0;
    pointer-events: none;
    background-image:
      linear-gradient(to right, rgba(255, 255, 255, 0.07) 1px, transparent 1px),
      linear-gradient(to bottom, rgba(255, 255, 255, 0.07) 1px, transparent 1px),
      linear-gradient(to right, rgba(255, 255, 255, 0.16) 1px, transparent 1px),
      linear-gradient(to bottom, rgba(255, 255, 255, 0.16) 1px, transparent 1px);
    background-repeat: repeat;
  }
  /* 缩得太小时只留主线，否则整块画布被细线糊成灰的。 */
  .layout-grid.major-only {
    background-image:
      linear-gradient(to right, rgba(255, 255, 255, 0.16) 1px, transparent 1px),
      linear-gradient(to bottom, rgba(255, 255, 255, 0.16) 1px, transparent 1px);
  }

  /* 变换控件（票 17）：框是细实线，把手是小白方块（用非等比缩放的 svg 画，
     所以 r 会被拉伸 —— 用 vector-effect 抵消掉）。 */
  .transform-box {
    position: absolute;
    inset: 0;
    pointer-events: none;
  }
  .transform-frame {
    fill: none;
    stroke: #3b82f6;
    stroke-width: 1;
    vector-effect: non-scaling-stroke;
  }

  /* 渐进模糊的线（票 73）：与变换框同一套画法 —— svg 被非等比拉伸，所以线宽靠
     vector-effect 固定，手柄半径则按 ppp 折算，屏幕上始终一样大。 */
  .progressive-line {
    position: absolute;
    inset: 0;
    pointer-events: none;
  }
  .progressive-line line {
    stroke: #f59e0b;
    stroke-width: 2;
    stroke-dasharray: 6 4;
    vector-effect: non-scaling-stroke;
  }
  .progressive-handle {
    fill: #ffffff;
    stroke: #f59e0b;
    stroke-width: 2;
    vector-effect: non-scaling-stroke;
  }

  .snap-guide { stroke: #ff68ba; stroke-width: 1; vector-effect: non-scaling-stroke; }
  .transform-handle {
    fill: #ffffff;
    stroke: #3b82f6;
    stroke-width: 1;
    vector-effect: non-scaling-stroke;
  }

  /* 参考线：1px 实线，青色（原版参考线是青蓝）。锁定后改成虚线。 */
  .guides {
    position: absolute;
    inset: 0;
    pointer-events: none;
  }
  .guide {
    position: absolute;
    background: #38bdf8;
    box-shadow: 0 0 0 0.5px rgba(0, 0, 0, 0.55);
  }
  .guide.vertical {
    top: -100000px;
    height: 200000px;
    width: 1px;
  }
  .guide.horizontal {
    left: -100000px;
    width: 200000px;
    height: 1px;
  }
  .guide.locked {
    background: repeating-linear-gradient(
      to bottom,
      #38bdf8 0 3px,
      transparent 3px 6px
    );
  }
  .guide.horizontal.locked {
    background: repeating-linear-gradient(
      to right,
      #38bdf8 0 3px,
      transparent 3px 6px
    );
  }

  /* 标尺（票 14）：贴画布上边与左边，从上面拖出参考线。 */
  .ruler {
    position: absolute;
    background: var(--bg-panel, #2a2a2e);
    border: 1px solid #141416;
    z-index: 5;
    cursor: crosshair;
  }
  .ruler.horizontal {
    left: 0;
    right: 0;
    top: 0;
    height: 18px;
  }
  .ruler.vertical {
    top: 0;
    bottom: 0;
    left: 0;
    width: 18px;
  }
  .ruler .tick {
    position: absolute;
    background: #6b6b74;
  }
  .ruler.horizontal .tick {
    top: 12px;
    width: 1px;
    height: 6px;
  }
  .ruler.vertical .tick {
    left: 12px;
    height: 1px;
    width: 6px;
  }
  .ruler .tick-label {
    position: absolute;
    top: 0;
    font-size: 8px;
    line-height: 10px;
    color: #9a9aa4;
    transform: translateX(1px);
    pointer-events: none;
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

  .gpu-hidden { visibility: hidden; }

  .drawing-preview {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    pointer-events: none;
    fill: rgba(255, 255, 255, 0.12);
    stroke: #fff;
  }

  .drawing-preview :is(line, ellipse, rect) { vector-effect: non-scaling-stroke; }

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
    stroke-width: 1;
    vector-effect: non-scaling-stroke;
  }

  /* 原版是白色实线打底、上面叠一条走动的黑色虚线（TransformOverlay.swift:296
     drawSelection）。相位每 0.12s 前进 1px（EditorCanvas.swift:2446-2456），
     [4,4] 一个周期 8px 走完 0.96s；steps(8) 保留原版一格一格的顿挫，而不是
     平滑滑动。 */
  .ants .ants-line {
    stroke: #fff;
  }

  .ants .ants-dashes {
    stroke: #000;
    stroke-dasharray: 4 4;
    animation: ants-march 0.96s steps(8, end) infinite;
  }

  @keyframes ants-march {
    to {
      stroke-dashoffset: 8;
    }
  }

  .selection-preview {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    pointer-events: none;
  }

  .selection-preview path {
    fill: rgba(59, 130, 246, 0.08);
    stroke: #60a5fa;
    stroke-width: 1;
    vector-effect: non-scaling-stroke;
    stroke-dasharray: 4 3;
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
