<script lang="ts">
  import {
    filterSession,
    updateFilterSettings,
    commitFilter,
    cancelFilter,
    armedEyedropper,
    registerLevelsEyedropper,
    sampleLevels,
    autoLevels,
    isCurrentFilterSession,
  } from "../state/filters";
  import * as BridgeService from "../../../wailsjs/go/bridge/Service";
  import NumericField from "./NumericField.svelte";
  import { untrack } from "svelte";

  /**
   * The unified filter/adjustment dialog (Filters.swift's sheet family): one
   * shell, per-kind forms. Slider rows are range + number pairs; every
   * change flows through updateFilterSettings so the Go preview worker
   * repaints the canvas. Adjustment forms edit the domain.Adjustment
   * payload carried in the session's settings.adjustment.
   */

  const session = $derived($filterSession);

  let levelsChannel = $state(0);
  const LEVEL_CHANNELS = ["RGB", "Red", "Green", "Blue"] as const;

  interface LevelRange {
    black: number;
    gamma: number;
    white: number;
    outputBlack: number;
    outputWhite: number;
  }

  interface AdjustmentShape {
    kind?: string;
    blurRadius?: number;
    motionAngle?: number;
    motionDistance?: number;
    hue?: number;
    saturation?: number;
    lightness?: number;
    colorize?: boolean;
    levels?: { channel?: string; ranges: LevelRange[] };
    curves?: { channel?: string; channels: { x: number; y: number }[][] };
    exposureSettings?: { exposure: number; offset: number; gamma: number };
    gradientMapSettings?: {
      shadows: { red: number; green: number; blue: number };
      highlights: { red: number; green: number; blue: number };
      reversed: boolean;
    };
    grainSettings?: { amount: number; size: number; roughness: number; seed: number };
    blackWhiteSettings?: Record<string, number | boolean>;
    colorBalanceSettings?: Record<string, number | boolean>;
    noiseAmount?: number;
    noiseGaussian?: boolean;
    noiseMonochromatic?: boolean;
  }

  function adj(): AdjustmentShape {
    return (session?.settings.adjustment ?? {}) as AdjustmentShape;
  }

  function setAdj(patch: Partial<AdjustmentShape>): void {
    if (!session) return;
    const next = { ...adj(), ...patch };
    if (!next.kind && session.kind.startsWith("adjust:")) {
      next.kind = session.kind.slice("adjust:".length);
    }
    updateFilterSettings({ adjustment: next as Record<string, unknown> });
  }

  // ---- Levels -------------------------------------------------------------
  let histogramBins = $state<[number[], number[], number[], number[]] | null>(null);
  const eyedropper = $derived($armedEyedropper);

  // The canvas forwards clicks while an eyedropper is armed.
  $effect(() => {
    if (!session || session.kind !== "adjust:Levels") return;
    return registerLevelsEyedropper((docX, docY, mode) => {
      void sampleLevels(docX, docY, mode);
    });
  });

  $effect(() => {
    const seed = session?.seed;
    const current = untrack(() => session);
    histogramBins = null;
    if (seed !== undefined && current?.kind === "adjust:Levels") {
      let stale = false;
      void BridgeService.LayerHistogram()
        .then((raw: string) => {
          if (stale || !isCurrentFilterSession(current)) return;
          const parsed = JSON.parse(raw) as { bins: [number[], number[], number[], number[]] };
          histogramBins = parsed.bins;
        })
        .catch((err: unknown) => console.warn("直方图读取失败", err));
      return () => { stale = true; };
    }
  });

  const histMax = $derived(
    histogramBins
      ? Math.max(...histogramBins.slice(1).flatMap((b) => b.map((v) => Math.log10(1 + v))))
      : 1,
  );

  function drawHistAction(node: HTMLCanvasElement): { update(...args: unknown[]): void } | void {
    const repaint = (): void => {
      if (histogramBins) drawHistogram(node, histogramBins);
    };
    repaint();
    return {
      update: repaint,
    };
  }

  function drawHistogram(canvas: HTMLCanvasElement, bins: [number[], number[], number[], number[]]): void {
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const w = canvas.width;
    const h = canvas.height;
    ctx.clearRect(0, 0, w, h);
    const colors = ["#888", "#c44", "#4a4", "#48c"];
    for (let c = 1; c <= 3; c++) {
      ctx.beginPath();
      ctx.moveTo(0, h);
      for (let i = 0; i < 256; i++) {
        const v = Math.log10(1 + bins[c][i]) / histMax;
        ctx.lineTo((i / 255) * w, h - v * h);
      }
      ctx.lineTo(w, h);
      ctx.closePath();
      ctx.strokeStyle = colors[c];
      ctx.stroke();
    }
  }

  function pickLevelPoint(mode: "black" | "gray" | "white"): void {
    armedEyedropper.set($armedEyedropper === mode ? null : mode);
  }

  function defaultRanges(): LevelRange[] {
    return [0, 1, 2, 3].map(() => ({
      black: 0,
      gamma: 1,
      white: 255,
      outputBlack: 0,
      outputWhite: 255,
    }));
  }

  function levelsRange(): LevelRange {
    const ranges = adj().levels?.ranges ?? defaultRanges();
    return ranges[levelsChannel] ?? ranges[0];
  }

  function setRange(patch: Partial<LevelRange>): void {
    const ranges = [...(adj().levels?.ranges ?? defaultRanges())];
    ranges[levelsChannel] = { ...ranges[levelsChannel], ...patch };
    setAdj({ levels: { ranges } });
  }

  // ---- Curves -------------------------------------------------------------
  const CURVE_CHANNELS = ["RGB", "Red", "Green", "Blue"] as const;
  let curveChannel = $state(0);

  function curvePoints(): { x: number; y: number }[] {
    const channels = adj().curves?.channels;
    if (!channels || !channels[curveChannel] || channels[curveChannel].length < 2) {
      return [
        { x: 0, y: 0 },
        { x: 255, y: 255 },
      ];
    }
    return channels[curveChannel];
  }

  function setCurvePoints(points: { x: number; y: number }[]): void {
    const channels = [...(adj().curves?.channels ?? [[], [], [], []])];
    channels[curveChannel] = points;
    setAdj({ curves: { channels: channels as { x: number; y: number }[][] } });
  }

  let dragIndex = $state(-1);

  function curveSvgPoint(event: MouseEvent & { currentTarget: SVGSVGElement }): {
    x: number;
    y: number;
  } {
    const rect = event.currentTarget.getBoundingClientRect();
    const x = Math.round(((event.clientX - rect.left) / rect.width) * 255);
    const y = Math.round((1 - (event.clientY - rect.top) / rect.height) * 255);
    return { x: Math.max(0, Math.min(255, x)), y: Math.max(0, Math.min(255, y)) };
  }

  function curveDown(event: MouseEvent & { currentTarget: EventTarget & SVGCircleElement }, index: number): void {
    event.preventDefault();
    dragIndex = index;
  }

  function curveMove(event: MouseEvent & { currentTarget: SVGSVGElement }): void {
    if (dragIndex < 0) return;
    const points = [...curvePoints()];
    const p = curveSvgPoint(event);
    const first = dragIndex === 0;
    const last = dragIndex === points.length - 1;
    points[dragIndex] = {
      x: first ? 0 : last ? 255 : p.x,
      y: p.y,
    };
    setCurvePoints(points);
  }

  function curveUp(): void {
    dragIndex = -1;
  }

  function curveAdd(event: MouseEvent & { currentTarget: SVGSVGElement }): void {
    const points = curvePoints();
    if (points.length >= 16) return;
    const p = curveSvgPoint(event);
    const next = [...points, p].sort((a, b) => a.x - b.x);
    setCurvePoints(next);
  }

  function curveRemove(index: number): void {
    const points = curvePoints();
    if (points.length <= 2 || index === 0 || index === points.length - 1) return;
    setCurvePoints(points.filter((_, i) => i !== index));
  }

  function hueOr(kind: string, fallback: number): number {
    const v = (adj()[kind as keyof AdjustmentShape] as number | undefined) ?? fallback;
    return v;
  }

  const isFilterKind = $derived(
    !!session &&
      !session.kind.startsWith("adjust:") &&
      session.kind !== "invert" &&
      session.kind !== "invertMask",
  );
  const kind = $derived(session?.kind ?? "");
  const s = $derived(session?.settings);
</script>

<svelte:window
  onkeydown={(e) => {
    if (!session) return;
    if (e.key === "Escape") void cancelFilter();
    else if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) void commitFilter();
  }}
/>

{#if session}
  <div class="backdrop" role="presentation">
    <div class="dialog" role="dialog" aria-modal="true" aria-label={session.label}>
      <header>
        <h2>{session.label}</h2>
        {#if session.editLayerId !== null}
          <span class="badge">调整图层</span>
        {:else}
          <span class="badge">破坏性</span>
        {/if}
        {#if session.preparing}
          <span class="preparing">渲染中…</span>
        {/if}
      </header>

      <div class="body" inert={session.operation !== null}>
        {#if isFilterKind && s}
          {#if kind === "gaussianBlur"}
            <div class="row">
              <NumericField label="半径 (σ)" min={0.1} max={250} step={0.1} value={s.radius}
                onchange={(radius) => updateFilterSettings({ radius })} />
              <input type="range" min="0.1" max="250" step="0.1" value={s.radius} aria-label="半径 (σ)"
                oninput={(e) => updateFilterSettings({ radius: +e.currentTarget.value })} />
            </div>
            <label class="check" title="只模糊颜色，保留图层轮廓与透明度">
              <input type="checkbox" checked={s.preserveTransparency}
                onchange={(e) => updateFilterSettings({ preserveTransparency: e.currentTarget.checked })} />
              保留透明度
            </label>
          {:else if kind === "motionBlur"}
            <div class="row">
              <NumericField label="角度 (°)" min={-90} max={90} step={1} value={s.angle}
                onchange={(angle) => updateFilterSettings({ angle })} />
              <input type="range" min="-90" max="90" step="1" value={s.angle} aria-label="角度 (°)"
                oninput={(e) => updateFilterSettings({ angle: +e.currentTarget.value })} />
            </div>
            <div class="row">
              <NumericField label="距离 (px)" min={1} max={2000} step={1} value={s.distance}
                onchange={(distance) => updateFilterSettings({ distance })} />
              <input type="range" min="1" max="2000" step="1" value={s.distance} aria-label="距离 (px)"
                oninput={(e) => updateFilterSettings({ distance: +e.currentTarget.value })} />
            </div>
          {:else if kind === "progressiveBlur"}
            <div class="row">
              <span>形状</span>
              <select value={s.progressiveRadial ? "radial" : "linear"}
                onchange={(e) => updateFilterSettings({ progressiveRadial: e.currentTarget.value === "radial" })}>
                <option value="linear">线性</option>
                <option value="radial">径向</option>
              </select>
            </div>
            <div class="row">
              <NumericField label="From (px)" min={0} max={250} step={0.5} value={s.progressiveFrom}
                onchange={(progressiveFrom) => updateFilterSettings({ progressiveFrom })} />
              <input type="range" min="0" max="250" step="0.5" value={s.progressiveFrom} aria-label="From (px)"
                oninput={(e) => updateFilterSettings({ progressiveFrom: +e.currentTarget.value })} />
            </div>
            <div class="row">
              <NumericField label="To (px)" min={0} max={250} step={0.5} value={s.progressiveTo}
                onchange={(progressiveTo) => updateFilterSettings({ progressiveTo })} />
              <input type="range" min="0" max="250" step="0.5" value={s.progressiveTo} aria-label="To (px)"
                oninput={(e) => updateFilterSettings({ progressiveTo: +e.currentTarget.value })} />
            </div>
            <label class="check" title="两端互换：起点用 To、终点用 From">
              <input type="checkbox" checked={s.progressiveInverted}
                onchange={(e) => updateFilterSettings({ progressiveInverted: e.currentTarget.checked })} />
              反相
            </label>
            <p class="hint">在画布上拖动线段的两个端点调整方向与范围。</p>
          {:else if kind === "addNoise"}
            <div class="row">
              <NumericField label="数量 %" min={0.1} max={400} step={0.1} value={s.amount}
                onchange={(amount) => updateFilterSettings({ amount })} />
              <input type="range" min="0.1" max="400" step="0.1" value={s.amount} aria-label="数量 %"
                oninput={(e) => updateFilterSettings({ amount: +e.currentTarget.value })} />
            </div>
            <label class="check"><input type="checkbox" checked={s.gaussian}
              onchange={(e) => updateFilterSettings({ gaussian: e.currentTarget.checked })} /> 高斯分布</label>
            <label class="check"><input type="checkbox" checked={s.monochromatic}
              onchange={(e) => updateFilterSettings({ monochromatic: e.currentTarget.checked })} /> 单色</label>
          {:else if kind === "vignette"}
            <div class="row">
              <NumericField label="数量 %" min={0} max={100} step={1} value={s.vignetteAmount}
                onchange={(vignetteAmount) => updateFilterSettings({ vignetteAmount })} />
              <input type="range" min="0" max="100" step="1" value={s.vignetteAmount} aria-label="数量 %"
                oninput={(e) => updateFilterSettings({ vignetteAmount: +e.currentTarget.value })} />
            </div>
            <div class="row">
              <span>颜色</span>
              {#each [0, 1, 2] as ch (ch)}
                <input class="tiny" type="number" min="0" max="255" value={Math.round(s.vignetteColor[ch] * 255)}
                  onchange={(e) => {
                    const color = [...s.vignetteColor] as [number, number, number];
                    color[ch] = +e.currentTarget.value / 255;
                    updateFilterSettings({ vignetteColor: color });
                  }} />
              {/each}
            </div>
            <div class="row">
              <NumericField label="中点" min={0} max={100} step={1} value={s.vignetteMidpoint}
                onchange={(vignetteMidpoint) => updateFilterSettings({ vignetteMidpoint })} />
              <input type="range" min="0" max="100" value={s.vignetteMidpoint} aria-label="中点"
                oninput={(e) => updateFilterSettings({ vignetteMidpoint: +e.currentTarget.value })} />
            </div>
            <div class="row">
              <NumericField label="圆度" min={-100} max={100} step={1} value={s.vignetteRoundness}
                onchange={(vignetteRoundness) => updateFilterSettings({ vignetteRoundness })} />
              <input type="range" min="-100" max="100" value={s.vignetteRoundness} aria-label="圆度"
                oninput={(e) => updateFilterSettings({ vignetteRoundness: +e.currentTarget.value })} />
            </div>
            <div class="row">
              <NumericField label="羽化" min={0} max={100} step={1} value={s.vignetteFeather}
                onchange={(vignetteFeather) => updateFilterSettings({ vignetteFeather })} />
              <input type="range" min="0" max="100" value={s.vignetteFeather} aria-label="羽化"
                oninput={(e) => updateFilterSettings({ vignetteFeather: +e.currentTarget.value })} />
            </div>
            <div class="row">
              <NumericField label="高光" min={0} max={100} step={1} value={s.vignetteHighlights}
                onchange={(vignetteHighlights) => updateFilterSettings({ vignetteHighlights })} />
              <input type="range" min="0" max="100" value={s.vignetteHighlights} aria-label="高光"
                oninput={(e) => updateFilterSettings({ vignetteHighlights: +e.currentTarget.value })} />
            </div>
          {:else if kind === "bloomGlow"}
            <div class="row">
              <NumericField label="亮度" min={0} max={100} step={1} value={s.bloomAmount}
                onchange={(bloomAmount) => updateFilterSettings({ bloomAmount })} />
              <input type="range" min="0" max="100" value={s.bloomAmount} aria-label="亮度"
                oninput={(e) => updateFilterSettings({ bloomAmount: +e.currentTarget.value })} />
            </div>
            <div class="row">
              <NumericField label="半径" min={1} max={150} step={1} value={s.bloomRadius}
                onchange={(bloomRadius) => updateFilterSettings({ bloomRadius })} />
              <input type="range" min="1" max="150" value={s.bloomRadius} aria-label="半径"
                oninput={(e) => updateFilterSettings({ bloomRadius: +e.currentTarget.value })} />
            </div>
          {:else if kind === "tonalContrast"}
            <div class="row">
              <NumericField label="数量" min={0} max={100} step={1} value={s.tonalAmount}
                onchange={(tonalAmount) => updateFilterSettings({ tonalAmount })} />
              <input type="range" min="0" max="100" value={s.tonalAmount} aria-label="数量"
                oninput={(e) => updateFilterSettings({ tonalAmount: +e.currentTarget.value })} />
            </div>
            <div class="row">
              <NumericField label="半径" min={1} max={100} step={1} value={s.tonalRadius}
                onchange={(tonalRadius) => updateFilterSettings({ tonalRadius })} />
              <input type="range" min="1" max="100" value={s.tonalRadius} aria-label="半径"
                oninput={(e) => updateFilterSettings({ tonalRadius: +e.currentTarget.value })} />
            </div>
            <div class="row">
              <NumericField label="阴影" min={-100} max={100} step={1} value={s.tonalShadows}
                onchange={(tonalShadows) => updateFilterSettings({ tonalShadows })} />
              <input type="range" min="-100" max="100" value={s.tonalShadows} aria-label="阴影"
                oninput={(e) => updateFilterSettings({ tonalShadows: +e.currentTarget.value })} />
            </div>
            <div class="row">
              <NumericField label="中间调" min={-100} max={100} step={1} value={s.tonalMidtones}
                onchange={(tonalMidtones) => updateFilterSettings({ tonalMidtones })} />
              <input type="range" min="-100" max="100" value={s.tonalMidtones} aria-label="中间调"
                oninput={(e) => updateFilterSettings({ tonalMidtones: +e.currentTarget.value })} />
            </div>
            <div class="row">
              <NumericField label="高光" min={-100} max={100} step={1} value={s.tonalHighlights}
                onchange={(tonalHighlights) => updateFilterSettings({ tonalHighlights })} />
              <input type="range" min="-100" max="100" value={s.tonalHighlights} aria-label="高光"
                oninput={(e) => updateFilterSettings({ tonalHighlights: +e.currentTarget.value })} />
            </div>
          {:else if kind === "dither" && s}
            {@const d = s.dither as Record<string, number | boolean | (number[]) | string>}
            <label class="row">
              <span>风格</span>
              <select value={String(d.style)} onchange={(e) => updateFilterSettings({ dither: { ...d, style: +e.currentTarget.value } })}>
                {#each ["Atkinson (Classic Mac)", "Floyd–Steinberg", "Bayer 2 × 2", "Bayer 4 × 4", "Bayer 8 × 8", "Halftone Dots", "Halftone Lines", "Halftone Diamonds", "Mac Patterns", "ASCII", "Scanlines (CRT)"] as name, i (name)}
                  <option value={i}>{name}</option>
                {/each}
              </select>
            </label>
            <div class="row">
              <NumericField label="像素大小" min={1} max={32} step={1} value={Number(d.pixelSize)}
                onchange={(pixelSize) => updateFilterSettings({ dither: { ...d, pixelSize } })} />
              <input type="range" min="1" max="32" value={Number(d.pixelSize)} aria-label="像素大小"
                oninput={(e) => updateFilterSettings({ dither: { ...d, pixelSize: +e.currentTarget.value } })} />
            </div>
            <div class="row">
              <NumericField label="色调数" min={2} max={8} step={1} value={Number(d.levels)}
                onchange={(levels) => updateFilterSettings({ dither: { ...d, levels } })} />
              <input type="range" min="2" max="8" value={Number(d.levels)} aria-label="色调数"
                oninput={(e) => updateFilterSettings({ dither: { ...d, levels: +e.currentTarget.value } })} />
            </div>
            <label class="row">
              <span>扩散 %</span>
              <input type="range" min="0" max="100" value={Number(d.diffusion)}
                oninput={(e) => updateFilterSettings({ dither: { ...d, diffusion: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>密度</span>
              <input type="range" min="-100" max="100" value={Number(d.density)}
                oninput={(e) => updateFilterSettings({ dither: { ...d, density: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>对比度</span>
              <input type="range" min="-100" max="100" value={Number(d.contrast)}
                oninput={(e) => updateFilterSettings({ dither: { ...d, contrast: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>网格大小</span>
              <input type="range" min="4" max="64" value={Number(d.cellSize)}
                oninput={(e) => updateFilterSettings({ dither: { ...d, cellSize: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>角度 °</span>
              <input type="range" min="-90" max="90" value={Number(d.angle)}
                oninput={(e) => updateFilterSettings({ dither: { ...d, angle: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>文字大小</span>
              <input type="range" min="6" max="64" value={Number(d.textSize)}
                oninput={(e) => updateFilterSettings({ dither: { ...d, textSize: +e.currentTarget.value } })} />
              <input class="flex1" type="text" value={String(d.characters)}
                onchange={(e) => updateFilterSettings({ dither: { ...d, characters: e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>行间距</span>
              <input type="range" min="2" max="32" value={Number(d.lineSpacing)}
                oninput={(e) => updateFilterSettings({ dither: { ...d, lineSpacing: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>辉光 %</span>
              <input type="range" min="0" max="100" value={Number(d.glow)}
                oninput={(e) => updateFilterSettings({ dither: { ...d, glow: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>珠点 %</span>
              <input type="range" min="0" max="100" value={Number(d.dots)}
                oninput={(e) => updateFilterSettings({ dither: { ...d, dots: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>波动 px</span>
              <input type="range" min="0" max="64" value={Number(d.wobble)}
                oninput={(e) => updateFilterSettings({ dither: { ...d, wobble: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>颜色</span>
              <select value={String(d.colors)} onchange={(e) => updateFilterSettings({ dither: { ...d, colors: +e.currentTarget.value } })}>
                <option value="0">黑白</option>
                <option value="1">双色调</option>
                <option value="2">原始色</option>
              </select>
            </label>
            <label class="check"><input type="checkbox" checked={Boolean(d.lightOnDark)}
              onchange={(e) => updateFilterSettings({ dither: { ...d, lightOnDark: e.currentTarget.checked } })} /> 亮标记（暗底亮纹）</label>
          {:else if kind === "scanlines" && s}
            {@const sl = s.scanlines as Record<string, number | (number[])>}
            <div class="row">
              <NumericField label="行间距 px" min={2} max={32} step={1} value={Number(sl.lineSpacing)}
                onchange={(lineSpacing) => updateFilterSettings({ scanlines: { ...sl, lineSpacing } })} />
              <input type="range" min="2" max="32" value={Number(sl.lineSpacing)} aria-label="行间距 px"
                oninput={(e) => updateFilterSettings({ scanlines: { ...sl, lineSpacing: +e.currentTarget.value } })} />
            </div>
            <div class="row">
              <NumericField label="厚度 %" min={5} max={100} step={1} value={Number(sl.thickness)}
                onchange={(thickness) => updateFilterSettings({ scanlines: { ...sl, thickness } })} />
              <input type="range" min="5" max="100" value={Number(sl.thickness)} aria-label="厚度 %"
                oninput={(e) => updateFilterSettings({ scanlines: { ...sl, thickness: +e.currentTarget.value } })} />
            </div>
            <div class="row">
              <NumericField label="辉光 %" min={0} max={100} step={1} value={Number(sl.glow)}
                onchange={(glow) => updateFilterSettings({ scanlines: { ...sl, glow } })} />
              <input type="range" min="0" max="100" value={Number(sl.glow)} aria-label="辉光 %"
                oninput={(e) => updateFilterSettings({ scanlines: { ...sl, glow: +e.currentTarget.value } })} />
            </div>
            <div class="row">
              <NumericField label="珠点 %" min={0} max={100} step={1} value={Number(sl.dots)}
                onchange={(dots) => updateFilterSettings({ scanlines: { ...sl, dots } })} />
              <input type="range" min="0" max="100" value={Number(sl.dots)} aria-label="珠点 %"
                oninput={(e) => updateFilterSettings({ scanlines: { ...sl, dots: +e.currentTarget.value } })} />
            </div>
            <div class="row">
              <NumericField label="波动 px" min={0} max={64} step={1} value={Number(sl.wobble)}
                onchange={(wobble) => updateFilterSettings({ scanlines: { ...sl, wobble } })} />
              <input type="range" min="0" max="64" value={Number(sl.wobble)} aria-label="波动 px"
                oninput={(e) => updateFilterSettings({ scanlines: { ...sl, wobble: +e.currentTarget.value } })} />
            </div>
            <div class="row">
              <NumericField label="位移 px" min={-100} max={100} step={1} value={Number(sl.displace)}
                onchange={(displace) => updateFilterSettings({ scanlines: { ...sl, displace } })} />
              <input type="range" min="-100" max="100" value={Number(sl.displace)} aria-label="位移 px"
                oninput={(e) => updateFilterSettings({ scanlines: { ...sl, displace: +e.currentTarget.value } })} />
            </div>
            <div class="row">
              <NumericField label="平滑 %" min={0} max={100} step={1} value={Number(sl.smoothness)}
                onchange={(smoothness) => updateFilterSettings({ scanlines: { ...sl, smoothness } })} />
              <input type="range" min="0" max="100" value={Number(sl.smoothness)} aria-label="平滑 %"
                oninput={(e) => updateFilterSettings({ scanlines: { ...sl, smoothness: +e.currentTarget.value } })} />
            </div>
            <div class="row">
              <NumericField label="阈值 %" min={0} max={100} step={1} value={Number(sl.threshold)}
                onchange={(threshold) => updateFilterSettings({ scanlines: { ...sl, threshold } })} />
              <input type="range" min="0" max="100" value={Number(sl.threshold)} aria-label="阈值 %"
                oninput={(e) => updateFilterSettings({ scanlines: { ...sl, threshold: +e.currentTarget.value } })} />
            </div>
            <div class="row">
              <NumericField label="色彩分离 px" min={0} max={16} step={1} value={Number(sl.split)}
                onchange={(split) => updateFilterSettings({ scanlines: { ...sl, split } })} />
              <input type="range" min="0" max="16" value={Number(sl.split)} aria-label="色彩分离 px"
                oninput={(e) => updateFilterSettings({ scanlines: { ...sl, split: +e.currentTarget.value } })} />
            </div>
            <div class="row">
              <NumericField label="密度" min={-100} max={100} step={1} value={Number(sl.density)}
                onchange={(density) => updateFilterSettings({ scanlines: { ...sl, density } })} />
              <input type="range" min="-100" max="100" value={Number(sl.density)} aria-label="密度"
                oninput={(e) => updateFilterSettings({ scanlines: { ...sl, density: +e.currentTarget.value } })} />
            </div>
            <div class="row">
              <NumericField label="对比度" min={-100} max={100} step={1} value={Number(sl.contrast)}
                onchange={(contrast) => updateFilterSettings({ scanlines: { ...sl, contrast } })} />
              <input type="range" min="-100" max="100" value={Number(sl.contrast)} aria-label="对比度"
                oninput={(e) => updateFilterSettings({ scanlines: { ...sl, contrast: +e.currentTarget.value } })} />
            </div>
            <div class="row">
              <NumericField label="黑位 %" min={0} max={100} step={1} value={Number(sl.blackLevel)}
                onchange={(blackLevel) => updateFilterSettings({ scanlines: { ...sl, blackLevel } })} />
              <input type="range" min="0" max="100" value={Number(sl.blackLevel)} aria-label="黑位 %"
                oninput={(e) => updateFilterSettings({ scanlines: { ...sl, blackLevel: +e.currentTarget.value } })} />
            </div>
            <label class="row">
              <span>颜色</span>
              <select value={String(sl.colors)} onchange={(e) => updateFilterSettings({ scanlines: { ...sl, colors: +e.currentTarget.value } })}>
                <option value="0">黑白</option>
                <option value="1">双色调</option>
                <option value="2">原始色</option>
              </select>
            </label>
            {#if Number(sl.colors) === 1}
              <div class="row">
                <span>屏色</span>
                {#each [0, 1, 2] as ch (ch)}
                  <input class="tiny" type="number" min="0" max="255"
                    value={Math.round((sl.dark as number[])[ch] * 255)}
                    onchange={(e) => {
                      const dark = [...(sl.dark as number[])];
                      dark[ch] = +e.currentTarget.value / 255;
                      updateFilterSettings({ scanlines: { ...sl, dark } });
                    }} />
                {/each}
              </div>
              <div class="row">
                <span>线色</span>
                {#each [0, 1, 2] as ch (ch)}
                  <input class="tiny" type="number" min="0" max="255"
                    value={Math.round((sl.light as number[])[ch] * 255)}
                    onchange={(e) => {
                      const light = [...(sl.light as number[])];
                      light[ch] = +e.currentTarget.value / 255;
                      updateFilterSettings({ scanlines: { ...sl, light } });
                    }} />
                {/each}
              </div>
            {/if}
          {:else if kind === "lensCorrection"}
            <div class="row">
              <NumericField label="移除扭曲" min={-100} max={100} step={1} value={s.distortion}
                onchange={(distortion) => updateFilterSettings({ distortion })} />
              <input type="range" min="-100" max="100" value={s.distortion} aria-label="移除扭曲"
                oninput={(e) => updateFilterSettings({ distortion: +e.currentTarget.value })} />
            </div>
          {:else if kind === "removeBackground" && s}
            <label class="row">
              <span>质量</span>
              <select
                value={s.backgroundQuality || "basic"}
                onchange={(e) => updateFilterSettings({ backgroundQuality: e.currentTarget.value })}
              >
                <option value="basic">基础（模型原掩码）</option>
                <option value="advanced">精细（引导滤波抠边）</option>
              </select>
            </label>
            <div class="row">
              <NumericField label="抠边强度" min={0} max={40} step={1} value={s.refineEdges}
                onchange={(refineEdges) => updateFilterSettings({ refineEdges })} />
              <input type="range" min="0" max="40" value={s.refineEdges} aria-label="抠边强度"
                oninput={(e) => updateFilterSettings({ refineEdges: +e.currentTarget.value })} />
            </div>
            <div class="row">
              <NumericField label="蒙版对比" min={0} max={100} step={1} value={s.matteContrast}
                onchange={(matteContrast) => updateFilterSettings({ matteContrast })} />
              <input type="range" min="0" max="100" value={s.matteContrast} aria-label="蒙版对比"
                oninput={(e) => updateFilterSettings({ matteContrast: +e.currentTarget.value })} />
            </div>
            <div class="row">
              <NumericField label="边缘偏移" min={-10} max={10} step={1} value={s.shiftEdge}
                onchange={(shiftEdge) => updateFilterSettings({ shiftEdge })} />
              <input type="range" min="-10" max="10" value={s.shiftEdge} aria-label="边缘偏移"
                oninput={(e) => updateFilterSettings({ shiftEdge: +e.currentTarget.value })} />
            </div>
            <p class="hint">安装版已包含 u2netp 分割模型（Apache-2.0）；开发版缺失时会下载。</p>
          {/if}
        {:else if kind === "adjust:Gaussian Blur" && s}
          <div class="row">
            <NumericField label="半径 (σ)" min={0.1} max={250} step={0.1} value={adj().blurRadius ?? 10}
              onchange={(blurRadius) => setAdj({ blurRadius })} />
            <input type="range" min="0.1" max="250" step="0.1" value={adj().blurRadius ?? 10} aria-label="半径 (σ)"
              oninput={(e) => setAdj({ blurRadius: +e.currentTarget.value })} />
          </div>
        {:else if kind === "adjust:Motion Blur" && s}
          <div class="row">
            <NumericField label="角度 (°)" min={-90} max={90} step={1} value={adj().motionAngle ?? 0}
              onchange={(motionAngle) => setAdj({ motionAngle })} />
            <input type="range" min="-90" max="90" step="1" value={adj().motionAngle ?? 0} aria-label="角度 (°)"
              oninput={(e) => setAdj({ motionAngle: +e.currentTarget.value })} />
          </div>
          <div class="row">
            <NumericField label="距离 (px)" min={1} max={2000} step={1} value={adj().motionDistance ?? 10}
              onchange={(motionDistance) => setAdj({ motionDistance })} />
            <input type="range" min="1" max="2000" step="1" value={adj().motionDistance ?? 10} aria-label="距离 (px)"
              oninput={(e) => setAdj({ motionDistance: +e.currentTarget.value })} />
          </div>
        {:else if kind === "adjust:Invert" && s}
          <p class="hint">反相调整图层没有可调参数。</p>
        {:else if kind === "adjust:Levels" && s}
          <div class="hist-wrap">
            <canvas width="256" height="72" use:drawHistAction></canvas>
          </div>
          <div class="tabs">
            {#each LEVEL_CHANNELS as name, i (name)}
              <button class:active={levelsChannel === i} onclick={() => (levelsChannel = i)}>{name}</button>
            {/each}
            <span class="spacer"></span>
            <button onclick={() => void autoLevels(0)}>自动·对比</button>
            <button onclick={() => void autoLevels(1)}>自动·颜色</button>
            <button onclick={() => void autoLevels(2)}>自动·中性</button>
          </div>
          {@const r = levelsRange()}
          <div class="row">
            <NumericField label="黑场" min={0} max={254} step={1} value={r.black}
              onchange={(black) => setRange({ black })} />
            <button class="pip" class:armed={eyedropper === "black"} aria-label="黑场吸管"
              onclick={() => pickLevelPoint("black")}>吸</button>
            <NumericField label="灰场" min={0.01} max={9.99} step={0.01} value={r.gamma}
              onchange={(gamma) => setRange({ gamma })} />
            <NumericField label="白场" min={1} max={255} step={1} value={r.white}
              onchange={(white) => setRange({ white })} />
          </div>
          <div class="row">
            <NumericField label="输出黑" min={0} max={255} step={1} value={r.outputBlack}
              onchange={(outputBlack) => setRange({ outputBlack })} />
            <NumericField label="输出白" min={0} max={255} step={1} value={r.outputWhite}
              onchange={(outputWhite) => setRange({ outputWhite })} />
          </div>
        {:else if kind === "adjust:Curves" && s}
          <div class="tabs">
            {#each CURVE_CHANNELS as name, i (name)}
              <button class:active={curveChannel === i} onclick={() => (curveChannel = i)}>{name}</button>
            {/each}
          </div>
          <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
          <svg class="curve" viewBox="-8 -8 271 271" role="application" aria-label="曲线编辑器"
            onmousemove={curveMove}
            onmouseup={curveUp}
            onmouseleave={curveUp}
            ondblclick={curveAdd}>
            <line x1="0" y1="255" x2="255" y2="0" class="diag" />
            {#each curvePoints() as p, i (i)}
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <circle cx={p.x} cy={255 - p.y} r="5"
                class:drag={i === dragIndex}
                onmousedown={(e) => curveDown(e, i)}
                ondblclick={(e) => {
                  e.stopPropagation();
                  curveRemove(i);
                }} />
            {/each}
          </svg>
          <p class="hint">拖动锚点调整；空白处双击加点，锚点双击删除。</p>
        {:else if kind === "adjust:Hue/Saturation" && s}
          <div class="row">
            <NumericField label="色相" min={-180} max={180} step={1} value={hueOr("hue", 0)}
              onchange={(hue) => setAdj({ hue })} />
            <input type="range" min="-180" max="180" value={hueOr("hue", 0)} aria-label="色相"
              oninput={(e) => setAdj({ hue: +e.currentTarget.value })} />
          </div>
          <div class="row">
            <NumericField label="饱和度" min={-100} max={100} step={1} value={hueOr("saturation", 0)}
              onchange={(saturation) => setAdj({ saturation })} />
            <input type="range" min="-100" max="100" value={hueOr("saturation", 0)} aria-label="饱和度"
              oninput={(e) => setAdj({ saturation: +e.currentTarget.value })} />
          </div>
          <div class="row">
            <NumericField label="明度" min={-100} max={100} step={1} value={hueOr("lightness", 0)}
              onchange={(lightness) => setAdj({ lightness })} />
            <input type="range" min="-100" max="100" value={hueOr("lightness", 0)} aria-label="明度"
              oninput={(e) => setAdj({ lightness: +e.currentTarget.value })} />
          </div>
          <label class="check"><input type="checkbox" checked={(adj().colorize ?? false)}
            onchange={(e) => setAdj({ colorize: e.currentTarget.checked })} /> 着色</label>
        {:else if kind === "adjust:Exposure" && s}
          {@const ex = adj().exposureSettings ?? { exposure: 0, offset: 0, gamma: 1 }}
          <div class="row">
            <NumericField label="曝光 (档)" min={-20} max={20} step={0.05} value={ex.exposure}
              onchange={(exposure) => setAdj({ exposureSettings: { ...ex, exposure } })} />
            <input type="range" min="-20" max="20" step="0.05" value={ex.exposure} aria-label="曝光 (档)"
              oninput={(e) => setAdj({ exposureSettings: { ...ex, exposure: +e.currentTarget.value } })} />
          </div>
          <div class="row">
            <NumericField label="偏移" min={-0.5} max={0.5} step={0.01} value={ex.offset}
              onchange={(offset) => setAdj({ exposureSettings: { ...ex, offset } })} />
            <input type="range" min="-0.5" max="0.5" step="0.01" value={ex.offset} aria-label="偏移"
              oninput={(e) => setAdj({ exposureSettings: { ...ex, offset: +e.currentTarget.value } })} />
          </div>
          <div class="row">
            <NumericField label="灰度系数" min={0.01} max={9.99} step={0.01} value={ex.gamma}
              onchange={(gamma) => setAdj({ exposureSettings: { ...ex, gamma } })} />
            <input type="range" min="0.01" max="9.99" step="0.01" value={ex.gamma} aria-label="灰度系数"
              oninput={(e) => setAdj({ exposureSettings: { ...ex, gamma: +e.currentTarget.value } })} />
          </div>
        {:else if kind === "adjust:Gradient Map" && s}
          {@const gm = adj().gradientMapSettings ?? {
            shadows: { red: 0, green: 0, blue: 0 },
            highlights: { red: 1, green: 1, blue: 1 },
            reversed: false,
          }}
          <div class="row">
            <span>暗部 RGB</span>
            {#each ["red", "green", "blue"] as c (c)}
              <input class="tiny" type="number" min="0" max="255"
                value={Math.round(gm.shadows[c as "red"] * 255)}
                onchange={(e) =>
                  setAdj({
                    gradientMapSettings: {
                      ...gm,
                      shadows: { ...gm.shadows, [c]: +e.currentTarget.value / 255 },
                    },
                  })} />
            {/each}
          </div>
          <div class="row">
            <span>亮部 RGB</span>
            {#each ["red", "green", "blue"] as c (c)}
              <input class="tiny" type="number" min="0" max="255"
                value={Math.round(gm.highlights[c as "red"] * 255)}
                onchange={(e) =>
                  setAdj({
                    gradientMapSettings: {
                      ...gm,
                      highlights: { ...gm.highlights, [c]: +e.currentTarget.value / 255 },
                    },
                  })} />
            {/each}
          </div>
          <label class="check"><input type="checkbox" checked={gm.reversed}
            onchange={(e) => setAdj({ gradientMapSettings: { ...gm, reversed: e.currentTarget.checked } })} /> 反向</label>
        {:else if kind === "adjust:Grain" && s}
          {@const g = adj().grainSettings ?? { amount: 25, size: 1.5, roughness: 50, seed: session.seed }}
          <div class="row">
            <NumericField label="数量 %" min={0} max={100} step={1} value={g.amount}
              onchange={(amount) => setAdj({ grainSettings: { ...g, amount } })} />
            <input type="range" min="0" max="100" value={g.amount} aria-label="数量 %"
              oninput={(e) => setAdj({ grainSettings: { ...g, amount: +e.currentTarget.value } })} />
          </div>
          <div class="row">
            <NumericField label="大小" min={0.5} max={20} step={0.1} value={g.size}
              onchange={(size) => setAdj({ grainSettings: { ...g, size } })} />
            <input type="range" min="0.5" max="20" step="0.1" value={g.size} aria-label="大小"
              oninput={(e) => setAdj({ grainSettings: { ...g, size: +e.currentTarget.value } })} />
          </div>
          <div class="row">
            <NumericField label="粗糙度" min={0} max={100} step={1} value={g.roughness}
              onchange={(roughness) => setAdj({ grainSettings: { ...g, roughness } })} />
            <input type="range" min="0" max="100" value={g.roughness} aria-label="粗糙度"
              oninput={(e) => setAdj({ grainSettings: { ...g, roughness: +e.currentTarget.value } })} />
          </div>
        {:else if kind === "adjust:Add Noise" && s}
          <div class="row">
            <NumericField label="数量 %" min={0.1} max={400} step={0.1} value={adj().noiseAmount ?? 10}
              onchange={(noiseAmount) => setAdj({ noiseAmount })} />
            <input type="range" min="0.1" max="400" step="0.1" value={adj().noiseAmount ?? 10} aria-label="数量 %"
              oninput={(e) => setAdj({ noiseAmount: +e.currentTarget.value })} />
          </div>
          <label class="check"><input type="checkbox" checked={adj().noiseGaussian ?? false}
            onchange={(e) => setAdj({ noiseGaussian: e.currentTarget.checked })} /> 高斯分布</label>
          <label class="check"><input type="checkbox" checked={adj().noiseMonochromatic ?? false}
            onchange={(e) => setAdj({ noiseMonochromatic: e.currentTarget.checked })} /> 单色</label>
        {:else if kind === "adjust:Black & White" && s}
          {@const bw = (adj().blackWhiteSettings ?? {
            reds: 40, yellows: 60, greens: 40, cyans: 60, blues: 20, magentas: 80,
            tint: false, tintHue: 40, tintSaturation: 20,
          }) as Record<string, number | boolean>}
          {#each [["reds", "红"], ["yellows", "黄"], ["greens", "绿"], ["cyans", "青"], ["blues", "蓝"], ["magentas", "洋红"]] as [key, label] (key)}
            <div class="row">
              <NumericField label={label} min={-200} max={300} step={1} value={bw[key] as number}
                onchange={(value) => setAdj({ blackWhiteSettings: { ...bw, [key]: value } })} />
              <input type="range" min="-200" max="300" value={bw[key] as number} aria-label={label}
                oninput={(e) => setAdj({ blackWhiteSettings: { ...bw, [key]: +e.currentTarget.value } })} />
            </div>
          {/each}
          <label class="check"><input type="checkbox" checked={bw.tint as boolean}
            onchange={(e) => setAdj({ blackWhiteSettings: { ...bw, tint: e.currentTarget.checked } })} /> 着色</label>
          {#if bw.tint}
            <label class="row">
              <span>色相</span>
              <input type="range" min="0" max="360" value={bw.tintHue as number}
                oninput={(e) => setAdj({ blackWhiteSettings: { ...bw, tintHue: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>饱和度</span>
              <input type="range" min="0" max="100" value={bw.tintSaturation as number}
                oninput={(e) => setAdj({ blackWhiteSettings: { ...bw, tintSaturation: +e.currentTarget.value } })} />
            </label>
          {/if}
        {:else if kind === "adjust:Color Balance" && s}
          {@const cb = (adj().colorBalanceSettings ?? {
            shadowCyanRed: 0, shadowMagentaGreen: 0, shadowYellowBlue: 0,
            midCyanRed: 0, midMagentaGreen: 0, midYellowBlue: 0,
            highlightCyanRed: 0, highlightMagentaGreen: 0, highlightYellowBlue: 0,
            preserveLuminosity: true,
          }) as Record<string, number | boolean>}
          {#each [["shadow", "阴影"], ["mid", "中间调"], ["highlight", "高光"]] as [prefix, label] (prefix)}
            <p class="group-label">{label}</p>
            <label class="row">
              <span>青—红</span>
              <input type="range" min="-100" max="100" value={cb[`${prefix}CyanRed`] as number}
                oninput={(e) => setAdj({ colorBalanceSettings: { ...cb, [`${prefix}CyanRed`]: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>洋红—绿</span>
              <input type="range" min="-100" max="100" value={cb[`${prefix}MagentaGreen`] as number}
                oninput={(e) => setAdj({ colorBalanceSettings: { ...cb, [`${prefix}MagentaGreen`]: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>黄—蓝</span>
              <input type="range" min="-100" max="100" value={cb[`${prefix}YellowBlue`] as number}
                oninput={(e) => setAdj({ colorBalanceSettings: { ...cb, [`${prefix}YellowBlue`]: +e.currentTarget.value } })} />
            </label>
          {/each}
          <label class="check"><input type="checkbox" checked={cb.preserveLuminosity as boolean}
            onchange={(e) => setAdj({ colorBalanceSettings: { ...cb, preserveLuminosity: e.currentTarget.checked } })} /> 保持明度</label>
        {/if}
      </div>

      {#if session.error}
        <p class="operation-error" role="alert">{session.error}</p>
      {/if}
      <footer>
        <span class="hint">预览为 ≤2048px 降采样；确定按全尺寸重算</span>
        <button disabled={session.operation !== null} onclick={() => void cancelFilter()}>
          {session.operation === "cancel" ? "取消中…" : "取消"}
        </button>
        <button class="primary" disabled={session.operation !== null} onclick={() => void commitFilter()}>
          {session.operation === "commit" ? "提交中…" : "确定"}
        </button>
      </footer>
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgb(0 0 0 / 45%);
    display: grid;
    place-items: center;
    z-index: 40;
  }

  .dialog {
    width: min(480px, 92vw);
    max-height: 84vh;
    display: flex;
    flex-direction: column;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 8px;
    box-shadow: 0 18px 50px rgb(0 0 0 / 35%);
  }

  header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px 16px 8px;
  }

  header h2 {
    font-size: 14px;
    margin: 0;
  }

  .badge {
    font-size: 10px;
    padding: 1px 6px;
    border: 1px solid var(--border);
    border-radius: 999px;
    color: var(--text-dim);
  }

  .preparing {
    font-size: 11px;
    color: var(--text-dim);
  }

  .body {
    padding: 4px 16px 12px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
  }

  .row > span:first-child {
    min-width: 84px;
    color: var(--text-dim);
  }

  /* 数值框并进 NumericField（票 53/V06）后，标签和数字成为一个整体：保留原来的标签列
     宽度，让各行的滑块左边缘继续对齐；数字右对齐，和旧的"数字框在行尾"观感接近。 */
  .row:has(input[type="range"]) > :global(.numeric-field) {
    min-width: 136px;
    justify-content: space-between;
  }

  .row input[type="range"] {
    flex: 1;
  }

  .row input[type="number"] {
    width: 64px;
  }

  .tiny {
    width: 52px;
  }

  .check {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
  }

  .hint {
    color: var(--text-dim);
    font-size: 11px;
    margin: 0;
  }

  .operation-error {
    color: var(--error, #f28b82);
    font-size: 12px;
    margin: 8px 16px;
    overflow-wrap: anywhere;
  }

  .group-label {
    font-size: 11px;
    color: var(--text-dim);
    margin: 4px 0 0;
  }

  .tabs {
    display: flex;
    gap: 4px;
    align-items: center;
  }

  .tabs button {
    font-size: 11px;
    padding: 2px 8px;
  }

  .spacer {
    flex: 1;
  }

  .hist-wrap {
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 4px;
    background: var(--bg-canvas);
  }

  .hist-wrap canvas {
    display: block;
    width: 100%;
    height: 72px;
  }

  .flex1 {
    flex: 1;
  }

  .pip {
    font-size: 10px;
    padding: 0 4px;
    margin-left: 4px;
  }

  .pip.armed {
    outline: 2px solid #48c;
  }

  .curve {
    width: 100%;
    aspect-ratio: 1;
    background: var(--bg-canvas);
    border: 1px solid var(--border);
    border-radius: 4px;
    touch-action: none;
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

  footer {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 16px 12px;
    border-top: 1px solid var(--border);
  }

  footer .hint {
    flex: 1;
  }

  footer .primary {
    font-weight: 600;
  }
</style>
