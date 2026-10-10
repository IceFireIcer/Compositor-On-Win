<script lang="ts">
  /**
   * 裁剪覆盖层（票 18 尾项）——自包含组件：几何/会话语义全在 ../state/crop.ts 的
   * CropState 与纯函数里，这里只负责"画出来 + 接指针 + 提交/取消"，不 import 也
   * 不修改 CanvasSurface 的任何内部状态（挂载点由调用方决定，见文件末尾注释）。
   *
   * 坐标：组件铺满 .canvas-stage（position:absolute; inset:0），所以它自己的
   * getBoundingClientRect() 就是舞台矩形；文档像素 → 舞台 CSS 像素一律走
   * viewport.ts 的 documentRect/documentPoint，和 CanvasSurface 用同一套映射，
   * 缩放/平移时框跟着文档走。
   *
   * 生命周期：会话跟着工具走——activeTool === "crop" 时自动 beginCropSession()
   * （起点是选区 bbox 的整数框，没有选区就是整幅画布），离开工具或按 Esc 就结束。
   * 会话期间注册 dispatch 的画布模态守卫，字母工具键不会在拖框时把工具换走；
   * 出口是 Esc（回移动工具）或 Enter（提交，提交后仍留在裁剪工具、框重开）。
   *
   * 吸附：目标来自 view.ts 的 guideTargets()/snapOptions()（参考线 + 布局网格 +
   * 图层边界），容差按原版 8 个屏幕点换算成文档像素（SNAP_TOLERANCE_PX / ppp）。
   * 定比（选了比例预设）时不做边吸附：吸附会破坏比例，而先吸附再回填比例会让
   * 框在拖动中跳动——原版也是先定尺寸再吸附，这里从简。
   */

  import { get } from "svelte/store";
  import { untrack } from "svelte";
  import {
    CROP_RATIO_CHOICES,
    CropState,
    commitCrop,
    commitCropToDocument,
    createCropRect,
    cropFromSelection,
    offsetGuides,
    resolveCropRatio,
    snapCropRect,
    type CropRatioChoice,
    type Point,
  } from "../state/crop";
  import { SNAP_TOLERANCE_PX, type Rect, type SnapTargets } from "../state/snap";
  import { documentPoint, documentRect, pointsPerPixel, viewport } from "../state/viewport";
  import { activeTool, selectTool } from "../state/tools";
  import { currentSelection } from "../state/selection";
  import { document as documentState } from "../state/document";
  import { guideTargets, layoutGrid, showGuides, snapOptions, visibleGuides } from "../state/view";
  import { has, Modifier, modifierBits } from "../state/modifiers";
  import { registerCanvasModalGuard } from "../shortcuts/dispatch";

  /** 最小输入：当前标签的文档尺寸（id 用来识别换标签）。 */
  let { doc }: { doc: { id?: string | null; width: number; height: number } } = $props();

  // --- 会话 ---------------------------------------------------------------

  /** 会话状态机（纯文档空间）；组件的 rect/active 只是它的响应式镜像。 */
  const session = new CropState();
  let active = $state(false);
  let rect = $state<Rect | null>(null);
  let ratio = $state<CropRatioChoice>("free");
  let error = $state<string | null>(null);
  let root = $state<HTMLDivElement | undefined>();

  /** beginCropSession 实际是按哪个文档开的（幂等判定用）。 */
  let openedFor = "";
  /** 生命周期 effect 最近一次处理的键；离开工具时清掉，好让再选 C 能重开。 */
  let lastKey = "";
  /**
   * 手动重开信号。整幅裁剪提交后文档尺寸不变，effect 靠 doc 变化重开不了，
   * 所以提交成功时自己加一，保证"提交后仍留在裁剪工具并给出新的整幅框"。
   */
  let restartEpoch = $state(0);

  const vp = $derived($viewport);
  const ppp = $derived(pointsPerPixel(vp));
  /** 文档矩形在舞台里的位置（和 CanvasSurface 的 .doc 同一映射）。 */
  const box = $derived(documentRect(vp, doc.width, doc.height));
  const size = $derived({ width: doc.width, height: doc.height });
  const ratioValue = $derived(resolveCropRatio(ratio, size));

  const HANDLES = ["nw", "n", "ne", "e", "se", "s", "sw", "w"] as const;
  type HandleId = (typeof HANDLES)[number];

  const frameBox = $derived.by(() => {
    const r = rect;
    if (!r) return null;
    return {
      left: box.x + r.x * ppp,
      top: box.y + r.y * ppp,
      width: r.width * ppp,
      height: r.height * ppp,
    };
  });

  const handleBoxes = $derived.by(() => {
    const r = rect;
    if (!r) return [] as { id: HandleId; left: number; top: number }[];
    return HANDLES.map((id) => {
      const p = handlePoint(r, id);
      return { id, left: box.x + p.x * ppp, top: box.y + p.y * ppp };
    });
  });

  /** 压暗遮罩：文档整幅一层暗色，用 evenodd 把裁剪框挖成亮区。 */
  const dimPath = $derived.by(() => {
    const r = rect;
    if (!r) return "";
    return (
      `M0 0H${doc.width}V${doc.height}H0Z ` +
      `M${r.x} ${r.y}H${r.x + r.width}V${r.y + r.height}H${r.x}Z`
    );
  });

  /** 三分线（原版裁剪框的构图辅助线）。 */
  const thirds = $derived.by(() => {
    const r = rect;
    if (!r) return [] as { x1: number; y1: number; x2: number; y2: number }[];
    const out: { x1: number; y1: number; x2: number; y2: number }[] = [];
    for (const t of [1 / 3, 2 / 3]) {
      out.push({ x1: r.x + r.width * t, y1: r.y, x2: r.x + r.width * t, y2: r.y + r.height });
      out.push({ x1: r.x, y1: r.y + r.height * t, x2: r.x + r.width, y2: r.y + r.height * t });
    }
    return out;
  });

  /**
   * 参考线预览：offsetGuides 算出裁后仍在画布内的参考线，再把裁后坐标加回裁剪
   * 原点，画在框内同一处——提交前就能看出哪些参考线会活下来。
   */
  const previewGuides = $derived.by(() => {
    const r = rect;
    if (!r || !$showGuides) return [] as { x1: number; y1: number; x2: number; y2: number }[];
    return offsetGuides($visibleGuides, r.x, r.y, r.width, r.height).map((g) => {
      const position = g.position + (g.axis === "vertical" ? r.x : r.y);
      return g.axis === "vertical"
        ? { x1: position, y1: r.y, x2: position, y2: r.y + r.height }
        : { x1: r.x, y1: position, x2: r.x + r.width, y2: position };
    });
  });

  /** HUD 贴着框的上沿；框顶太靠上就改挂在框下方。 */
  const hudBox = $derived.by(() => {
    const f = frameBox;
    if (!f) return { left: box.x, top: 8 };
    const above = f.top - 46;
    return {
      left: Math.max(4, Math.min(f.left, Math.max(4, vp.viewWidth - 320))),
      top: above >= 4 ? above : f.top + f.height + 8,
    };
  });

  // --- 指针 ---------------------------------------------------------------

  type Drag =
    | { kind: "create"; from: Point; moved: boolean }
    | { kind: "move"; start: Rect; at: Point }
    | { kind: "handle"; handle: HandleId; start: Rect };

  let drag: Drag | null = null;

  function ctrlHeld(): boolean {
    return has(get(modifierBits), Modifier.Ctrl);
  }

  function altHeld(): boolean {
    return has(get(modifierBits), Modifier.Alt);
  }

  /** 会话对应的文档标识：换标签或尺寸变化都算换文档。 */
  function docKey(): string {
    return `${doc.id ?? ""}:${doc.width}x${doc.height}`;
  }

  /** 指针位置 → 文档像素（不夹取：框的合法化交给 normalizeCropRect）。 */
  function docPixelOf(e: PointerEvent): Point {
    const el = root;
    if (!el) return { x: 0, y: 0 };
    const r = el.getBoundingClientRect();
    return documentPoint(get(viewport), e.clientX - r.left, e.clientY - r.top, doc.width, doc.height);
  }

  /** 把手光标（方向随把手位置，写在内联样式里以免动态 class 被 CSS 裁剪掉）。 */
  function cursorFor(handle: HandleId): string {
    switch (handle) {
      case "nw":
      case "se":
        return "nwse-resize";
      case "ne":
      case "sw":
        return "nesw-resize";
      case "n":
      case "s":
        return "ns-resize";
      default:
        return "ew-resize";
    }
  }

  function handlePoint(r: Rect, handle: HandleId): Point {
    return {
      x: handle.includes("w") ? r.x : handle.includes("e") ? r.x + r.width : r.x + r.width / 2,
      y: handle.includes("n") ? r.y : handle.includes("s") ? r.y + r.height : r.y + r.height / 2,
    };
  }

  function applyRect(next: Rect): void {
    if (session.setRect(next)) rect = session.rect;
  }

  /** 当前吸附目标：参考线 + 布局网格 + 图层边界（+ 文档边界供网格取长度）。 */
  function snapTargets(): SnapTargets {
    const layers = get(documentState).layers;
    return {
      guides: guideTargets(),
      grid: get(layoutGrid),
      layerBounds: layers.map((l) => ({
        x: l.transform.origin[0],
        y: l.transform.origin[1],
        width: l.transform.size[0],
        height: l.transform.size[1],
      })),
      docBounds: { width: doc.width, height: doc.height },
    };
  }

  function snapEdges(r: Rect, handle: HandleId | null): Rect {
    const edges =
      handle === null
        ? undefined
        : {
            left: handle.includes("w"),
            right: handle.includes("e"),
            top: handle.includes("n"),
            bottom: handle.includes("s"),
          };
    return snapCropRect(r, snapTargets(), size, {
      ...snapOptions(ctrlHeld()),
      tolerance: SNAP_TOLERANCE_PX / ppp,
      mode: "edges",
      edges,
    });
  }

  function snapMove(r: Rect): Rect {
    return snapCropRect(r, snapTargets(), size, {
      ...snapOptions(ctrlHeld()),
      tolerance: SNAP_TOLERANCE_PX / ppp,
      mode: "move",
    });
  }

  /** 自由 reshape：只动被抓住的那条边，另一维保持。 */
  function freeReshape(start: Rect, handle: HandleId, p: Point): Rect {
    const left = handle.includes("w") ? Math.min(p.x, start.x + start.width - 1) : start.x;
    const right = handle.includes("e") ? Math.max(p.x, start.x + 1) : start.x + start.width;
    const top = handle.includes("n") ? Math.min(p.y, start.y + start.height - 1) : start.y;
    const bottom = handle.includes("s") ? Math.max(p.y, start.y + 1) : start.y + start.height;
    return { x: left, y: top, width: right - left, height: bottom - top };
  }

  /** 定比角手柄：以对角为锚，取较大的一维为准，另一维按比例补齐。 */
  function ratioCornerRect(start: Rect, handle: HandleId, p: Point, fixed: number): Rect {
    const ax = handle.includes("w") ? start.x + start.width : start.x;
    const ay = handle.includes("n") ? start.y + start.height : start.y;
    let w = Math.max(1, Math.abs(p.x - ax));
    let h = Math.max(1, Math.abs(p.y - ay));
    if (w > h * fixed) h = w / fixed;
    else w = h * fixed;
    return {
      x: handle.includes("w") ? ax - w : ax,
      y: handle.includes("n") ? ay - h : ay,
      width: w,
      height: h,
    };
  }

  /** 定比边手柄：主轴定尺寸，另一维按比例从框中心对称长出来。 */
  function ratioEdgeRect(start: Rect, handle: HandleId, p: Point, fixed: number): Rect {
    const cx = start.x + start.width / 2;
    const cy = start.y + start.height / 2;
    if (handle === "e" || handle === "w") {
      const width = Math.max(1, handle === "e" ? p.x - start.x : start.x + start.width - p.x);
      const height = width / fixed;
      return {
        x: handle === "e" ? start.x : start.x + start.width - width,
        y: cy - height / 2,
        width,
        height,
      };
    }
    const height = Math.max(1, handle === "s" ? p.y - start.y : start.y + start.height - p.y);
    const width = height * fixed;
    return {
      x: cx - width / 2,
      y: handle === "s" ? start.y : start.y + start.height - height,
      width,
      height,
    };
  }

  function reshape(start: Rect, handle: HandleId, p: Point, fixed: number | null): Rect {
    if (fixed === null) return freeReshape(start, handle, p);
    if (handle.length === 2) return ratioCornerRect(start, handle, p, fixed);
    return ratioEdgeRect(start, handle, p, fixed);
  }

  function startCreate(e: PointerEvent): void {
    if (!active || e.button !== 0) return;
    e.preventDefault();
    // 覆盖层是 .canvas-stage 的子节点：不拦住冒泡的话，CanvasSurface 的
    // onPointerDown 会把同一次按下也当成"抓参考线/变换/画笔"处理。
    e.stopPropagation();
    drag = { kind: "create", from: docPixelOf(e), moved: false };
    root?.setPointerCapture(e.pointerId);
  }

  function startMove(e: PointerEvent): void {
    if (!active || e.button !== 0 || !rect) return;
    e.preventDefault();
    e.stopPropagation();
    drag = { kind: "move", start: { ...rect }, at: docPixelOf(e) };
    root?.setPointerCapture(e.pointerId);
  }

  function startHandle(e: PointerEvent, handle: HandleId): void {
    if (!active || e.button !== 0 || !rect) return;
    e.preventDefault();
    e.stopPropagation();
    drag = { kind: "handle", handle, start: { ...rect } };
    root?.setPointerCapture(e.pointerId);
  }

  function onPointerMove(e: PointerEvent): void {
    if (!drag) return;
    e.stopPropagation();
    const p = docPixelOf(e);
    if (drag.kind === "create") {
      // 原版是"拖出选框"：没真正拖动过（<2 文档像素）就不动当前框，避免误点缩框。
      if (!drag.moved && Math.abs(p.x - drag.from.x) < 2 && Math.abs(p.y - drag.from.y) < 2) return;
      drag.moved = true;
      applyRect(snapEdges(createCropRect(drag.from, p, ratioValue, altHeld()), null));
      return;
    }
    if (drag.kind === "move") {
      applyRect(
        snapMove({
          x: drag.start.x + (p.x - drag.at.x),
          y: drag.start.y + (p.y - drag.at.y),
          width: drag.start.width,
          height: drag.start.height,
        }),
      );
      return;
    }
    const raw = reshape(drag.start, drag.handle, p, ratioValue);
    applyRect(ratioValue === null ? snapEdges(raw, drag.handle) : raw);
  }

  function onPointerUp(e: PointerEvent): void {
    if (drag) e.stopPropagation();
    drag = null;
    const el = root;
    if (el && typeof el.hasPointerCapture === "function" && el.hasPointerCapture(e.pointerId)) {
      el.releasePointerCapture(e.pointerId);
    }
  }

  // --- 会话命令（对外接口）-------------------------------------------------

  /** 选区 bbox（文档空间，取轮廓点集的包围盒）；没有可用选区时 null。 */
  function selectionBBox(): Rect | null {
    const sel = get(currentSelection);
    if (!sel || !sel.active || sel.loops.length === 0) return null;
    let minX = Infinity;
    let minY = Infinity;
    let maxX = -Infinity;
    let maxY = -Infinity;
    for (const loop of sel.loops) {
      for (const p of loop) {
        if (p.x < minX) minX = p.x;
        if (p.y < minY) minY = p.y;
        if (p.x > maxX) maxX = p.x;
        if (p.y > maxY) maxY = p.y;
      }
    }
    if (!Number.isFinite(minX) || !Number.isFinite(minY)) return null;
    return { x: minX, y: minY, width: maxX - minX, height: maxY - minY };
  }

  /**
   * 开始一次裁剪会话：起点是选区 bbox 的整数框（cropFromSelection），没有选区
   * 就是整幅画布。已经有会话时是幂等的，工具切换可以放心重复调用。
   */
  export function beginCropSession(): boolean {
    if (!Number.isFinite(doc.width) || !Number.isFinite(doc.height)) return false;
    if (active && rect && openedFor === docKey()) return true;
    const canvas = { width: doc.width, height: doc.height };
    if (!session.beginCrop(canvas, cropFromSelection(selectionBBox(), canvas))) return false;
    rect = session.rect;
    active = rect !== null;
    ratio = "free";
    error = null;
    openedFor = docKey();
    return active;
  }

  /** 结束会话（丢弃裁剪框，文档不动）。 */
  export function cancelCropSession(): void {
    session.cancelCrop();
    rect = null;
    active = false;
    error = null;
    openedFor = "";
  }

  /**
   * 提交：commitCrop 的纯数据 → Go Crop RPC → 应用快照。成功后关掉会话，尺寸
   * 变化会让会话在同一个工具下按新文档重开（Photoshop 的裁剪工具也是提交后
   * 仍留在裁剪工具）。失败保留会话与框，错误显示在 HUD 里。
   */
  export async function commitCropSession(): Promise<boolean> {
    const r = rect;
    if (!active || !r) return false;
    const payload = commitCrop(r, { width: doc.width, height: doc.height });
    if (!payload) {
      error = "裁剪框无效";
      return false;
    }
    const result = await commitCropToDocument(payload);
    if (!result.ok) {
      error = result.error ?? "裁剪失败";
      return false;
    }
    error = null;
    cancelCropSession();
    restartEpoch += 1; // 尺寸没变（整幅裁剪）时也重开一个新框
    return true;
  }

  /** Esc：结束会话并退回移动工具（裁剪期间画布快捷键被模态守卫挡住，Esc 是出口）。 */
  function exitCrop(): void {
    cancelCropSession();
    selectTool("move");
  }

  function onRatioChange(value: string): void {
    ratio = value as CropRatioChoice;
    const fixed = resolveCropRatio(ratio, size);
    if (fixed === null) return; // 自由比例：保留当前框
    if (session.applyAspect(fixed, altHeld())) rect = session.rect;
  }

  // --- 生命周期 -------------------------------------------------------------

  /**
   * 会话跟随工具与文档：activeTool 变成 crop 就开会话，离开就关；文档换了标签、
   * 尺寸变了，或提交成功（restartEpoch +1，覆盖"整幅裁剪尺寸没变"的情况）就按
   * 新文档重开。untrack 包住命令调用，避免会话命令里读到的 store 把 effect 变成
   * "任何文档变化都重开会话"。
   */
  $effect(() => {
    const tool = $activeTool;
    const key = `${docKey()}#${restartEpoch}`;
    untrack(() => {
      if (tool !== "crop") {
        if (active) cancelCropSession();
        lastKey = ""; // 再选回裁剪工具时要能重新开会话
        return;
      }
      if (lastKey === key) return;
      if (beginCropSession()) lastKey = key;
    });
  });

  // 会话期间屏蔽画布字母键：拖框时按到工具键不该把工具换走（dispatch 的模态守卫）。
  $effect(() => {
    if (!active) return;
    return registerCanvasModalGuard(() => true);
  });

  // Enter 提交 / Esc 取消。捕获阶段拦下，别让快捷键分发层再看到这两个键。
  $effect(() => {
    if (!active) return;
    const onKeyDown = (e: KeyboardEvent): void => {
      if (e.key === "Escape") {
        e.preventDefault();
        e.stopPropagation();
        exitCrop();
        return;
      }
      if (e.key !== "Enter") return;
      const target = e.target as HTMLElement | null;
      if (target && (target.tagName === "TEXTAREA" || target.tagName === "INPUT" || target.isContentEditable)) {
        return; // 输入控件里的回车归控件
      }
      e.preventDefault();
      e.stopPropagation();
      void commitCropSession();
    };
    window.addEventListener("keydown", onKeyDown, true);
    return () => window.removeEventListener("keydown", onKeyDown, true);
  });

  /** 比例下拉的中文标签（原版 CropControls 的选项）。 */
  function ratioLabel(choice: CropRatioChoice): string {
    switch (choice) {
      case "free":
        return "自由";
      case "original":
        return "原始比例";
      default:
        return choice;
    }
  }
</script>

<!--
  裁剪覆盖层。铺满 .canvas-stage（inset: 0），只在一个会话里吃指针：
  SVG 画压暗遮罩 + 框 + 三分线 + 参考线预览，HTML 层画框体与 8 个把手（把手不随
  非等比缩放变成椭圆，所以不用 SVG 画）。z-index 低于标尺，标尺仍然可拖。
-->
{#if active && rect && frameBox}
  <div
    class="crop-overlay"
    bind:this={root}
    role="application"
    aria-label="裁剪"
    onpointerdown={startCreate}
    onpointermove={onPointerMove}
    onpointerup={onPointerUp}
    onpointercancel={onPointerUp}
  >
    <svg
      class="crop-svg"
      viewBox="0 0 {doc.width} {doc.height}"
      preserveAspectRatio="none"
      style:left="{box.x}px"
      style:top="{box.y}px"
      style:width="{doc.width * ppp}px"
      style:height="{doc.height * ppp}px"
    >
      <path class="crop-dim" d={dimPath} fill-rule="evenodd" />
      {#each thirds as line, i (i)}
        <line
          class="crop-third"
          x1={line.x1}
          y1={line.y1}
          x2={line.x2}
          y2={line.y2}
          vector-effect="non-scaling-stroke"
        />
      {/each}
      {#each previewGuides as line, i (i)}
        <line
          class="crop-guide"
          x1={line.x1}
          y1={line.y1}
          x2={line.x2}
          y2={line.y2}
          vector-effect="non-scaling-stroke"
        />
      {/each}
      <rect
        class="crop-frame"
        x={rect.x}
        y={rect.y}
        width={rect.width}
        height={rect.height}
        vector-effect="non-scaling-stroke"
      />
    </svg>

    <!-- 框体：拖动整框（move 吸附保持尺寸）。 -->
    <div
      class="crop-frame-body"
      role="presentation"
      style:left="{frameBox.left}px"
      style:top="{frameBox.top}px"
      style:width="{frameBox.width}px"
      style:height="{frameBox.height}px"
      onpointerdown={startMove}
    ></div>

    {#each handleBoxes as h (h.id)}
      <div
        class="crop-handle"
        role="presentation"
        style:left="{h.left}px"
        style:top="{h.top}px"
        style:cursor={cursorFor(h.id)}
        onpointerdown={(e) => startHandle(e, h.id)}
      ></div>
    {/each}

    <div
      class="crop-hud"
      role="presentation"
      style:left="{hudBox.left}px"
      style:top="{hudBox.top}px"
      onpointerdown={(e) => e.stopPropagation()}
    >
      <select
        class="crop-ratio"
        aria-label="裁剪比例"
        value={ratio}
        onchange={(e) => onRatioChange(e.currentTarget.value)}
      >
        {#each CROP_RATIO_CHOICES as choice (choice)}
          <option value={choice}>{ratioLabel(choice)}</option>
        {/each}
      </select>
      <span class="crop-size">{rect.width} × {rect.height} px</span>
      <button type="button" class="crop-action" onclick={() => void commitCropSession()}>
        提交 ⏎
      </button>
      <button type="button" class="crop-action ghost" onclick={exitCrop}>取消 Esc</button>
      {#if error}
        <span class="crop-error">{error}</span>
      {/if}
    </div>
  </div>
{/if}

<style>
  /* 覆盖层：铺满舞台但不挡标尺（标尺 z-index 5）。 */
  .crop-overlay {
    position: absolute;
    inset: 0;
    z-index: 4;
    touch-action: none;
    cursor: crosshair;
  }

  .crop-svg {
    position: absolute;
    overflow: visible;
    pointer-events: none;
  }

  /* 框外压暗：文档整幅一层，evenodd 把框挖空。 */
  .crop-dim {
    fill: rgba(0, 0, 0, 0.55);
  }

  .crop-frame {
    fill: none;
    stroke: rgba(255, 255, 255, 0.92);
    stroke-width: 1;
    filter: drop-shadow(0 0 1px rgba(0, 0, 0, 0.9));
  }

  .crop-third {
    stroke: rgba(255, 255, 255, 0.32);
    stroke-width: 1;
    stroke-dasharray: 3 3;
  }

  /* 参考线预览：裁后仍活着的参考线会落在框内的这个位置。 */
  .crop-guide {
    stroke: #38bdf8;
    stroke-width: 1;
    stroke-dasharray: 2 4;
    opacity: 0.75;
  }

  /* 框体：只吃框内指针（框外归 startCreate 画新框）。 */
  .crop-frame-body {
    position: absolute;
    cursor: move;
  }

  .crop-handle {
    position: absolute;
    width: 10px;
    height: 10px;
    margin: -5px 0 0 -5px;
    background: var(--bg-panel, #2a2a2e);
    border: 1px solid var(--accent, #3b82f6);
    border-radius: 1px;
  }

  .crop-hud {
    position: absolute;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 4px 8px;
    background: var(--bg-panel, #2a2a2e);
    border: 1px solid #141416;
    border-radius: 4px;
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
    font-size: 11px;
    color: var(--text, #e6e6ea);
    white-space: nowrap;
  }

  .crop-ratio {
    background: transparent;
    color: inherit;
    border: 1px solid #3a3a40;
    border-radius: 3px;
    font-size: 11px;
    padding: 1px 2px;
  }

  .crop-size {
    font-variant-numeric: tabular-nums;
    color: #9a9aa4;
  }

  .crop-action {
    background: var(--accent, #3b82f6);
    color: #fff;
    border: none;
    border-radius: 3px;
    padding: 2px 8px;
    font-size: 11px;
    cursor: pointer;
  }

  .crop-action.ghost {
    background: transparent;
    border: 1px solid #3a3a40;
    color: inherit;
  }

  .crop-error {
    color: #f87171;
  }
</style>
