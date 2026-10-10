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
    panByStore,
    pointsPerPixel,
    resizeViewport,
    viewport,
    zoomAt,
  } from "../state/viewport";
  import { Modifier, applyKey, clearModifiers, modifierBits } from "../state/modifiers";
  import { activeTool } from "../state/tools";
  import { eyedropperClick } from "../state/filters";
  import { currentSelection } from "../state/selection";
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
  import {
    beginGuideCreation,
    beginGuideMove,
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
    visibleGuides,
  } from "../state/view";
  import { gridLines, gridStep } from "../state/snap";
  import { commitGuides as commitGuidesAction } from "../shortcuts/actions";
  import CropOverlay from "./CropOverlay.svelte";
  import {
    beginTransformDrag,
    beginTransformSession,
    commitTransformSession,
    dragTransform,
    transformSession,
  } from "../state/transform";
  import {
    HANDLES,
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
    textSession.update((s) => {
      if (!s || sameSel(s.selection, next)) return s;
      return { ...s, selection: next };
    });
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
  $effect(() => {
    const content = $textSession?.content;
    const el = textEditor;
    if (content === undefined || !el || document.activeElement !== el) return;
    syncSelection();
  });

  const vp = $derived($viewport);
  const ppp = $derived(pointsPerPixel(vp));
  const rect = $derived(documentRect(vp, doc.width, doc.height));
  // The pixel grid appears from 800% (EditorCanvas.swift:920); lines sit on
  // every document pixel, i.e. every `ppp` CSS px >= 8px apart.
  const showGrid = $derived(vp.zoom >= PIXEL_GRID_ZOOM);
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
  const PAINT_TOOLS: ReadonlySet<string> = new Set(["brush"]);

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
  let moveDrag: { startX: number; startY: number } | null = null;

  /** 左键按下：把手 → resize，框内 → move。没有会话时先按需开一个。 */
  function beginMoveDrag(e: PointerEvent): boolean {
    // 还没有会话就先开一个（V 工具直接拖图层 = 移动它，与原版一致）。
    if (!get(transformSession) && !beginTransformSession()) return false;
    const handle = handleAt(e);
    // 没抓到把手、又不在框内：这次按下不属于变换（交给别的工具/平移逻辑）。
    // 但"刚开的会话"要留着，让框显示出来，用户可以按 Esc 取消。
    if (handle === null && !insideDraft(e)) return false;
    const mode: DragMode = handle !== null ? { kind: "resize", handle } : { kind: "move" };
    const p = docPointOf(e);
    if (!beginTransformDrag(mode, p)) return false;
    moveDrag = { startX: p.x, startY: p.y };
    return true;
  }

  /** 拖动中：把文档空间位移交给会话。 */
  function updateMoveDrag(e: PointerEvent): void {
    if (!moveDrag) return;
    const p = docPointOf(e);
    dragTransform(
      { x: p.x - moveDrag.startX, y: p.y - moveDrag.startY },
      { shift: e.shiftKey, option: e.altKey, ctrl: e.ctrlKey },
    );
  }

  /** 松手：提交到文档（一次历史项）。 */
  function endMoveDrag(e: PointerEvent): void {
    if (!moveDrag) return;
    moveDrag = null;
    stage?.releasePointerCapture(e.pointerId);
    void commitTransformSession();
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
    const spaceHeld = ($modifierBits & Modifier.Space) !== 0;
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
    if (e.button === 0 && !panDrag && !painting) {
      const p = docPixelOf(e);
      if (eyedropperClick(Math.floor(p.x), Math.floor(p.y))) {
        e.preventDefault();
        return;
      }
    }
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
    if (e.button === 0 && $activeTool === "type") {
      const p = docPixelOf(e);
      if (!editTextLayerAt(p.x, p.y)) {
        textDrag = { x: p.x, y: p.y };
      }
      stage?.setPointerCapture(e.pointerId);
      e.preventDefault();
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
    // 变换拖动优先于其它工具。
    if (moveDrag) {
      updateMoveDrag(e);
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
    if (painting) queueStrokePoint(docPixelOf(e));
    if (healing) {
      const p = docPixelOf(e);
      void HealPoint(p.x, p.y);
    }
  }

  function onPointerEnd(e: PointerEvent): void {
    if (moveDrag) {
      endMoveDrag(e);
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
          <polygon
            class="transform-frame"
            points={draftCorners.map((c) => `${c.x},${c.y}`).join(" ")}
          />
          {#each HANDLES as handle, i (i)}
            {@const hp = draftPoint(handle)}
            <circle class="transform-handle" cx={hp.x} cy={hp.y} r={6} />
          {/each}
        {/if}
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
