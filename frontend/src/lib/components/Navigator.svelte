<script lang="ts">
  import { get } from "svelte/store";
  import { untrack } from "svelte";
  import { viewport } from "../state/viewport";
  import { showRulers, showsNavigator } from "../state/view";
  import { canvasOnly } from "../state/canvasOnly";
  import {
    centerView,
    navigatorDocumentPoint,
    navigatorKeyboardTarget,
    navigatorShown,
    navigatorSize,
    navigatorViewportBox,
    visibleDocumentRect,
  } from "../state/navigator";

  /**
   * 导航器（票 61 / 对等矩阵 U08）。真值：reference/Swift/Compositor/UI/
   * NavigatorMinimap.swift —— 画布右上角的缩略图，300% 起显示：整幅文档缩小
   * 摆好，用强调色框出画布当前显示的那一块，点或拖就把视图挪过去。
   *
   * 缩略图复用画布那份合成结果：`src` 是 CanvasSurface 已经用来画布的
   * /render/{id}.png（同一个 URL，浏览器缓存共用），不另开 GPU 管线；盒子
   * 尺寸固定，也不按文档尺寸缓存 JS 位图。编辑停下 250ms 才换图（原版
   * `task(id: RenderKey)` 的 sleep），所以一次笔触/拖动只重画一次。
   */
  let {
    src,
    docWidth,
    docHeight,
  }: { src: string | null; docWidth: number; docHeight: number } = $props();

  /** 原版 NavigatorMinimap 的 250ms：编辑先落定，再换缩略图。 */
  const PICTURE_SETTLE_MS = 250;

  let box = $state<HTMLDivElement>();

  const size = $derived(navigatorSize({ width: docWidth, height: docHeight }));
  const shown = $derived(
    navigatorShown({
      enabled: $showsNavigator,
      zoom: $viewport.zoom,
      documentWidth: docWidth,
      documentHeight: docHeight,
      canvasOnly: $canvasOnly,
    }),
  );
  const geometry = $derived({
    documentSize: { width: docWidth, height: docHeight },
    box: size,
  });
  /** 画布现在显示的那一块，映到缩略图并裁进图片范围；没有就不画框。 */
  const viewportBox = $derived(
    navigatorViewportBox(geometry, visibleDocumentRect($viewport, docWidth, docHeight)),
  );
  // 标尺是画布内部的覆盖层（18px），导航器往下让开，免得盖住刻度。原版标尺
  // 在画布之外，所以它的 padding 12 就是这里的 12 + 标尺高。
  const topOffset = $derived(($showRulers ? 18 : 0) + 12);

  let pictureSrc = $state<string | null>(null);
  let pictureBroken = $state(false);

  // 换图：首张立刻画，之后的变更等一拍（原版 `if picture != nil { sleep }`）。
  // 隐藏时把图丢掉，下次显示立刻重画；只有显示着才让 <img> 真的去取图。
  $effect(() => {
    const url = src;
    if (!shown || !url) {
      pictureSrc = null;
      return;
    }
    if (untrack(() => pictureSrc) === null) {
      pictureSrc = url;
      pictureBroken = false;
      return;
    }
    const timer = setTimeout(() => {
      pictureSrc = url;
      pictureBroken = false;
    }, PICTURE_SETTLE_MS);
    return () => clearTimeout(timer);
  });

  /** 缩略图上的一次指针 → 把那个文档点挪到画布正中（缩放不变）。 */
  function centerOn(event: PointerEvent): void {
    const el = box;
    if (!el) return;
    const bounds = el.getBoundingClientRect();
    const point = navigatorDocumentPoint(geometry, {
      x: event.clientX - bounds.left,
      y: event.clientY - bounds.top,
    });
    viewport.update((s) => centerView(s, point, docWidth, docHeight));
  }

  // 拖动中的指针 id；原版是 DragGesture(minimumDistance: 0)，按下即生效。
  let dragPointer: number | null = null;

  function onPointerDown(event: PointerEvent): void {
    const el = box;
    if (!el || event.button !== 0) return;
    // 画布的工具/平移手势挂在父元素上：缩略图上的一下只属于导航器。
    event.preventDefault();
    event.stopPropagation();
    el.focus(); // preventDefault 挡掉了默认聚焦，手动补上（键盘可达性）
    dragPointer = event.pointerId;
    el.setPointerCapture(event.pointerId);
    centerOn(event);
  }

  function onPointerMove(event: PointerEvent): void {
    if (dragPointer !== event.pointerId) return;
    event.stopPropagation();
    centerOn(event);
  }

  function onPointerEnd(event: PointerEvent): void {
    const el = box;
    if (!el || dragPointer !== event.pointerId) return;
    dragPointer = null;
    event.stopPropagation();
    if (el.hasPointerCapture(event.pointerId)) el.releasePointerCapture(event.pointerId);
  }

  /**
   * 键盘可达性的最小支持：方向键按可见区域的一格（1/8）平移视图。方向键在
   * 别处是图层微调，导航器拿着焦点时这一下归它，所以拦住冒泡（窗口快捷键是
   * 冒泡阶段的监听）。
   */
  function onKeyDown(event: KeyboardEvent): void {
    const stepX = event.key === "ArrowLeft" ? -1 : event.key === "ArrowRight" ? 1 : 0;
    const stepY = event.key === "ArrowUp" ? -1 : event.key === "ArrowDown" ? 1 : 0;
    if (stepX === 0 && stepY === 0) return;
    event.preventDefault();
    event.stopPropagation();
    const target = navigatorKeyboardTarget(
      visibleDocumentRect(get(viewport), docWidth, docHeight),
      stepX,
      stepY,
    );
    viewport.update((s) => centerView(s, target, docWidth, docHeight));
  }
</script>

{#if shown && size.width > 0 && size.height > 0}
  <!-- 缩略图是自绘的交互控件（点/拖平移视图、方向键微调），与原版
       accessibilityElement + DragGesture 对应；Svelte 的 a11y 规则把
       role="application" 当非交互元素，这里按仓库既有做法显式豁免。 -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <div
    class="navigator"
    bind:this={box}
    style:width="{size.width}px"
    style:height="{size.height}px"
    style:top="{topOffset}px"
    role="application"
    aria-label="导航器"
    title="点击或拖拽移动视图（方向键微调）"
    tabindex="0"
    onpointerdown={onPointerDown}
    onpointermove={onPointerMove}
    onpointerup={onPointerEnd}
    onpointercancel={onPointerEnd}
    onkeydown={onKeyDown}
    oncontextmenu={(event) => event.preventDefault()}
  >
    {#if pictureSrc && !pictureBroken}
      <img
        class="picture"
        src={pictureSrc}
        alt=""
        draggable="false"
        onerror={() => (pictureBroken = true)}
      />
    {/if}
    {#if viewportBox}
      <!-- 视口框：原版 strokeBorder 画在框内，所以用 border-box；小于 3px 的
           框也要看得见（原版 frame(width: max(3, …))）。 -->
      <div
        class="viewport-box"
        style:left="{viewportBox.x}px"
        style:top="{viewportBox.y}px"
        style:width="{Math.max(3, viewportBox.width)}px"
        style:height="{Math.max(3, viewportBox.height)}px"
      ></div>
    {/if}
  </div>
{/if}

<style>
  /* 固定在画布角落、尺寸由内容给定（最大 126 × 90）：绝对定位，不参与画布
     区域的布局，所以窄窗口与右侧图层面板都不会被它挤到。 */
  .navigator {
    position: absolute;
    right: 12px;
    z-index: 7;
    box-sizing: border-box;
    border-radius: 4px;
    background: #1c1c1c; /* 原版 Color(white: 0.11) */
    box-shadow:
      0 2px 6px rgba(0, 0, 0, 0.4),
      inset 0 0 0 1px rgba(255, 255, 255, 0.25);
    overflow: hidden;
    cursor: default;
    touch-action: none;
  }

  .picture {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    display: block;
    object-fit: contain; /* imageRect 就是等比居中，contain 与它一致 */
    pointer-events: none;
  }

  .viewport-box {
    position: absolute;
    box-sizing: border-box;
    border: 2.5px solid var(--accent, #3b82f6);
    pointer-events: none;
  }
</style>
