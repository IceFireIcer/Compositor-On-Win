<script lang="ts">
  import {
    filterSession,
    updateFilterSettings,
    commitFilter,
    cancelFilter,
    armedEyedropper,
    registerLevelsEyedropper,
  } from "../state/filters";
  import * as BridgeService from "../../../wailsjs/go/bridge/Service";

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
      const settings = adj().levels ?? { ranges: defaultRanges() };
      const modeIndex = mode === "black" ? 0 : mode === "gray" ? 1 : 2;
      void BridgeService.ApplyLevelsSample(JSON.stringify(settings), docX, docY, modeIndex)
        .then((raw: string) => {
          const updated = JSON.parse(raw) as LevelRange;
          const ranges = [...(adj().levels?.ranges ?? defaultRanges())];
          ranges[levelsChannel] = updated;
          setAdj({ levels: { ranges } });
          armedEyedropper.set(null);
        })
        .catch((err: unknown) => console.warn("吸管取样失败", err));
    });
  });

  $effect(() => {
    if (session && session.kind === "adjust:Levels" && histogramBins === null) {
      void BridgeService.LayerHistogram()
        .then((raw: string) => {
          const parsed = JSON.parse(raw) as { bins: [number[], number[], number[], number[]] };
          histogramBins = parsed.bins;
        })
        .catch((err: unknown) => console.warn("直方图读取失败", err));
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

  async function autoLevels(mode: number): Promise<void> {
    try {
      const raw = await BridgeService.LevelsAuto(mode);
      const updated = JSON.parse(raw) as {
        channel?: string;
        ranges: LevelRange[];
      };
      setAdj({ levels: { ranges: updated.ranges } });
    } catch (err: unknown) {
      console.warn("自动色阶失败", err);
    }
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

      <div class="body">
        {#if isFilterKind && s}
          {#if kind === "gaussianBlur"}
            <label class="row">
              <span>半径 (σ)</span>
              <input type="range" min="0.1" max="250" step="0.1" value={s.radius}
                oninput={(e) => updateFilterSettings({ radius: +e.currentTarget.value })} />
              <input type="number" min="0.1" max="250" step="0.1" value={s.radius}
                onchange={(e) => updateFilterSettings({ radius: +e.currentTarget.value })} />
            </label>
          {:else if kind === "motionBlur"}
            <label class="row">
              <span>角度 (°)</span>
              <input type="range" min="-90" max="90" step="1" value={s.angle}
                oninput={(e) => updateFilterSettings({ angle: +e.currentTarget.value })} />
              <input type="number" min="-90" max="90" value={s.angle}
                onchange={(e) => updateFilterSettings({ angle: +e.currentTarget.value })} />
            </label>
            <label class="row">
              <span>距离 (px)</span>
              <input type="range" min="1" max="2000" step="1" value={s.distance}
                oninput={(e) => updateFilterSettings({ distance: +e.currentTarget.value })} />
              <input type="number" min="1" max="2000" value={s.distance}
                onchange={(e) => updateFilterSettings({ distance: +e.currentTarget.value })} />
            </label>
          {:else if kind === "addNoise"}
            <label class="row">
              <span>数量 %</span>
              <input type="range" min="0.1" max="400" step="0.1" value={s.amount}
                oninput={(e) => updateFilterSettings({ amount: +e.currentTarget.value })} />
              <input type="number" min="0.1" max="400" step="0.1" value={s.amount}
                onchange={(e) => updateFilterSettings({ amount: +e.currentTarget.value })} />
            </label>
            <label class="check"><input type="checkbox" checked={s.gaussian}
              onchange={(e) => updateFilterSettings({ gaussian: e.currentTarget.checked })} /> 高斯分布</label>
            <label class="check"><input type="checkbox" checked={s.monochromatic}
              onchange={(e) => updateFilterSettings({ monochromatic: e.currentTarget.checked })} /> 单色</label>
          {:else if kind === "vignette"}
            <label class="row">
              <span>数量 %</span>
              <input type="range" min="0" max="100" step="1" value={s.vignetteAmount}
                oninput={(e) => updateFilterSettings({ vignetteAmount: +e.currentTarget.value })} />
              <input type="number" min="0" max="100" value={s.vignetteAmount}
                onchange={(e) => updateFilterSettings({ vignetteAmount: +e.currentTarget.value })} />
            </label>
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
            <label class="row">
              <span>中点</span>
              <input type="range" min="0" max="100" value={s.vignetteMidpoint}
                oninput={(e) => updateFilterSettings({ vignetteMidpoint: +e.currentTarget.value })} />
              <input type="number" min="0" max="100" value={s.vignetteMidpoint}
                onchange={(e) => updateFilterSettings({ vignetteMidpoint: +e.currentTarget.value })} />
            </label>
            <label class="row">
              <span>圆度</span>
              <input type="range" min="-100" max="100" value={s.vignetteRoundness}
                oninput={(e) => updateFilterSettings({ vignetteRoundness: +e.currentTarget.value })} />
              <input type="number" min="-100" max="100" value={s.vignetteRoundness}
                onchange={(e) => updateFilterSettings({ vignetteRoundness: +e.currentTarget.value })} />
            </label>
            <label class="row">
              <span>羽化</span>
              <input type="range" min="0" max="100" value={s.vignetteFeather}
                oninput={(e) => updateFilterSettings({ vignetteFeather: +e.currentTarget.value })} />
              <input type="number" min="0" max="100" value={s.vignetteFeather}
                onchange={(e) => updateFilterSettings({ vignetteFeather: +e.currentTarget.value })} />
            </label>
            <label class="row">
              <span>高光</span>
              <input type="range" min="0" max="100" value={s.vignetteHighlights}
                oninput={(e) => updateFilterSettings({ vignetteHighlights: +e.currentTarget.value })} />
              <input type="number" min="0" max="100" value={s.vignetteHighlights}
                onchange={(e) => updateFilterSettings({ vignetteHighlights: +e.currentTarget.value })} />
            </label>
          {:else if kind === "bloomGlow"}
            <label class="row">
              <span>亮度</span>
              <input type="range" min="0" max="100" value={s.bloomAmount}
                oninput={(e) => updateFilterSettings({ bloomAmount: +e.currentTarget.value })} />
              <input type="number" min="0" max="100" value={s.bloomAmount}
                onchange={(e) => updateFilterSettings({ bloomAmount: +e.currentTarget.value })} />
            </label>
            <label class="row">
              <span>半径</span>
              <input type="range" min="1" max="150" value={s.bloomRadius}
                oninput={(e) => updateFilterSettings({ bloomRadius: +e.currentTarget.value })} />
              <input type="number" min="1" max="150" value={s.bloomRadius}
                onchange={(e) => updateFilterSettings({ bloomRadius: +e.currentTarget.value })} />
            </label>
          {:else if kind === "tonalContrast"}
            <label class="row">
              <span>数量</span>
              <input type="range" min="0" max="100" value={s.amount}
                oninput={(e) => updateFilterSettings({ amount: +e.currentTarget.value })} />
              <input type="number" min="0" max="100" value={s.amount}
                onchange={(e) => updateFilterSettings({ amount: +e.currentTarget.value })} />
            </label>
            <label class="row">
              <span>半径</span>
              <input type="range" min="1" max="100" value={s.tonalRadius}
                oninput={(e) => updateFilterSettings({ tonalRadius: +e.currentTarget.value })} />
              <input type="number" min="1" max="100" value={s.tonalRadius}
                onchange={(e) => updateFilterSettings({ tonalRadius: +e.currentTarget.value })} />
            </label>
            <label class="row">
              <span>阴影</span>
              <input type="range" min="-100" max="100" value={s.tonalShadows}
                oninput={(e) => updateFilterSettings({ tonalShadows: +e.currentTarget.value })} />
              <input type="number" min="-100" max="100" value={s.tonalShadows}
                onchange={(e) => updateFilterSettings({ tonalShadows: +e.currentTarget.value })} />
            </label>
            <label class="row">
              <span>中间调</span>
              <input type="range" min="-100" max="100" value={s.tonalMidtones}
                oninput={(e) => updateFilterSettings({ tonalMidtones: +e.currentTarget.value })} />
              <input type="number" min="-100" max="100" value={s.tonalMidtones}
                onchange={(e) => updateFilterSettings({ tonalMidtones: +e.currentTarget.value })} />
            </label>
            <label class="row">
              <span>高光</span>
              <input type="range" min="-100" max="100" value={s.tonalHighlights}
                oninput={(e) => updateFilterSettings({ tonalHighlights: +e.currentTarget.value })} />
              <input type="number" min="-100" max="100" value={s.tonalHighlights}
                onchange={(e) => updateFilterSettings({ tonalHighlights: +e.currentTarget.value })} />
            </label>
          {:else if kind === "lensCorrection"}
            <label class="row">
              <span>移除扭曲</span>
              <input type="range" min="-100" max="100" value={s.distortion}
                oninput={(e) => updateFilterSettings({ distortion: +e.currentTarget.value })} />
              <input type="number" min="-100" max="100" value={s.distortion}
                onchange={(e) => updateFilterSettings({ distortion: +e.currentTarget.value })} />
            </label>
          {:else if kind === "cameraRaw"}
            <p class="hint">Light / Color 快速组 —— 完整面板（曲线/混色器/分级/细节/光学）随 34 号票。</p>
            <label class="row">
              <span>曝光</span>
              <input type="range" min="-5" max="5" step="0.05" value={s.cameraRaw.exposure ?? 0}
                oninput={(e) => updateFilterSettings({ cameraRaw: { ...s.cameraRaw, exposure: +e.currentTarget.value } })} />
              <input type="number" min="-5" max="5" step="0.05" value={s.cameraRaw.exposure ?? 0}
                onchange={(e) => updateFilterSettings({ cameraRaw: { ...s.cameraRaw, exposure: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>对比度</span>
              <input type="range" min="-100" max="100" value={s.cameraRaw.contrast ?? 0}
                oninput={(e) => updateFilterSettings({ cameraRaw: { ...s.cameraRaw, contrast: +e.currentTarget.value } })} />
              <input type="number" min="-100" max="100" value={s.cameraRaw.contrast ?? 0}
                onchange={(e) => updateFilterSettings({ cameraRaw: { ...s.cameraRaw, contrast: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>高光</span>
              <input type="range" min="-100" max="100" value={s.cameraRaw.highlights ?? 0}
                oninput={(e) => updateFilterSettings({ cameraRaw: { ...s.cameraRaw, highlights: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>阴影</span>
              <input type="range" min="-100" max="100" value={s.cameraRaw.shadows ?? 0}
                oninput={(e) => updateFilterSettings({ cameraRaw: { ...s.cameraRaw, shadows: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>色温</span>
              <input type="range" min="-100" max="100" value={s.cameraRaw.temperature ?? 0}
                oninput={(e) => updateFilterSettings({ cameraRaw: { ...s.cameraRaw, temperature: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>色调</span>
              <input type="range" min="-100" max="100" value={s.cameraRaw.tint ?? 0}
                oninput={(e) => updateFilterSettings({ cameraRaw: { ...s.cameraRaw, tint: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>自然饱和度</span>
              <input type="range" min="-100" max="100" value={s.cameraRaw.vibrance ?? 0}
                oninput={(e) => updateFilterSettings({ cameraRaw: { ...s.cameraRaw, vibrance: +e.currentTarget.value } })} />
            </label>
            <label class="row">
              <span>饱和度</span>
              <input type="range" min="-100" max="100" value={s.cameraRaw.saturation ?? 0}
                oninput={(e) => updateFilterSettings({ cameraRaw: { ...s.cameraRaw, saturation: +e.currentTarget.value } })} />
            </label>
          {/if}
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
          <label class="row">
            <span>黑场 <button class="pip" class:armed={eyedropper === "black"} onclick={() => pickLevelPoint("black")}>吸</button></span>
            <input type="number" min="0" max="254" value={r.black}
              onchange={(e) => setRange({ black: +e.currentTarget.value })} />
            <span>灰场</span>
            <input type="number" min="0.01" max="9.99" step="0.01" value={r.gamma}
              onchange={(e) => setRange({ gamma: +e.currentTarget.value })} />
            <span>白场</span>
            <input type="number" min="1" max="255" value={r.white}
              onchange={(e) => setRange({ white: +e.currentTarget.value })} />
          </label>
          <label class="row">
            <span>输出黑</span>
            <input type="number" min="0" max="255" value={r.outputBlack}
              onchange={(e) => setRange({ outputBlack: +e.currentTarget.value })} />
            <span>输出白</span>
            <input type="number" min="0" max="255" value={r.outputWhite}
              onchange={(e) => setRange({ outputWhite: +e.currentTarget.value })} />
          </label>
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
          <label class="row">
            <span>色相</span>
            <input type="range" min="-180" max="180" value={hueOr("hue", 0)}
              oninput={(e) => setAdj({ hue: +e.currentTarget.value })} />
            <input type="number" min="-180" max="180" value={hueOr("hue", 0)}
              onchange={(e) => setAdj({ hue: +e.currentTarget.value })} />
          </label>
          <label class="row">
            <span>饱和度</span>
            <input type="range" min="-100" max="100" value={hueOr("saturation", 0)}
              oninput={(e) => setAdj({ saturation: +e.currentTarget.value })} />
            <input type="number" min="-100" max="100" value={hueOr("saturation", 0)}
              onchange={(e) => setAdj({ saturation: +e.currentTarget.value })} />
          </label>
          <label class="row">
            <span>明度</span>
            <input type="range" min="-100" max="100" value={hueOr("lightness", 0)}
              oninput={(e) => setAdj({ lightness: +e.currentTarget.value })} />
            <input type="number" min="-100" max="100" value={hueOr("lightness", 0)}
              onchange={(e) => setAdj({ lightness: +e.currentTarget.value })} />
          </label>
          <label class="check"><input type="checkbox" checked={(adj().colorize ?? false)}
            onchange={(e) => setAdj({ colorize: e.currentTarget.checked })} /> 着色</label>
        {:else if kind === "adjust:Exposure" && s}
          {@const ex = adj().exposureSettings ?? { exposure: 0, offset: 0, gamma: 1 }}
          <label class="row">
            <span>曝光 (档)</span>
            <input type="range" min="-20" max="20" step="0.05" value={ex.exposure}
              oninput={(e) => setAdj({ exposureSettings: { ...ex, exposure: +e.currentTarget.value } })} />
            <input type="number" min="-20" max="20" step="0.05" value={ex.exposure}
              onchange={(e) => setAdj({ exposureSettings: { ...ex, exposure: +e.currentTarget.value } })} />
          </label>
          <label class="row">
            <span>偏移</span>
            <input type="range" min="-0.5" max="0.5" step="0.01" value={ex.offset}
              oninput={(e) => setAdj({ exposureSettings: { ...ex, offset: +e.currentTarget.value } })} />
            <input type="number" min="-0.5" max="0.5" step="0.01" value={ex.offset}
              onchange={(e) => setAdj({ exposureSettings: { ...ex, offset: +e.currentTarget.value } })} />
          </label>
          <label class="row">
            <span>灰度系数</span>
            <input type="range" min="0.01" max="9.99" step="0.01" value={ex.gamma}
              oninput={(e) => setAdj({ exposureSettings: { ...ex, gamma: +e.currentTarget.value } })} />
            <input type="number" min="0.01" max="9.99" step="0.01" value={ex.gamma}
              onchange={(e) => setAdj({ exposureSettings: { ...ex, gamma: +e.currentTarget.value } })} />
          </label>
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
          <label class="row">
            <span>数量 %</span>
            <input type="range" min="0" max="100" value={g.amount}
              oninput={(e) => setAdj({ grainSettings: { ...g, amount: +e.currentTarget.value } })} />
            <input type="number" min="0" max="100" value={g.amount}
              onchange={(e) => setAdj({ grainSettings: { ...g, amount: +e.currentTarget.value } })} />
          </label>
          <label class="row">
            <span>大小</span>
            <input type="range" min="0.5" max="20" step="0.1" value={g.size}
              oninput={(e) => setAdj({ grainSettings: { ...g, size: +e.currentTarget.value } })} />
            <input type="number" min="0.5" max="20" step="0.1" value={g.size}
              onchange={(e) => setAdj({ grainSettings: { ...g, size: +e.currentTarget.value } })} />
          </label>
          <label class="row">
            <span>粗糙度</span>
            <input type="range" min="0" max="100" value={g.roughness}
              oninput={(e) => setAdj({ grainSettings: { ...g, roughness: +e.currentTarget.value } })} />
            <input type="number" min="0" max="100" value={g.roughness}
              onchange={(e) => setAdj({ grainSettings: { ...g, roughness: +e.currentTarget.value } })} />
          </label>
        {:else if kind === "adjust:Add Noise" && s}
          <label class="row">
            <span>数量 %</span>
            <input type="range" min="0.1" max="400" step="0.1" value={adj().noiseAmount ?? 10}
              oninput={(e) => setAdj({ noiseAmount: +e.currentTarget.value })} />
            <input type="number" min="0.1" max="400" step="0.1" value={adj().noiseAmount ?? 10}
              onchange={(e) => setAdj({ noiseAmount: +e.currentTarget.value })} />
          </label>
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
            <label class="row">
              <span>{label}</span>
              <input type="range" min="-200" max="300" value={bw[key] as number}
                oninput={(e) => setAdj({ blackWhiteSettings: { ...bw, [key]: +e.currentTarget.value } })} />
              <input type="number" min="-200" max="300" value={bw[key] as number}
                onchange={(e) => setAdj({ blackWhiteSettings: { ...bw, [key]: +e.currentTarget.value } })} />
            </label>
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

      <footer>
        <span class="hint">预览为 ≤2048px 降采样；确定按全尺寸重算</span>
        <button onclick={() => void cancelFilter()}>取消</button>
        <button class="primary" onclick={() => void commitFilter()}>确定</button>
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
