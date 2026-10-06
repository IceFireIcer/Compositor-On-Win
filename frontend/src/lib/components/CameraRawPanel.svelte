<script lang="ts">
  import {
    filterSession,
    updateFilterSettings,
    commitFilter,
    cancelFilter,
    armedEyedropper,
    autoWhiteBalance,
  } from "../state/filters";
  import { document } from "../state/document";
  import * as BridgeService from "../../../wailsjs/go/bridge/Service";
  import CameraRawSlider from "./CameraRawSlider.svelte";
  import CurveEditor from "./CurveEditor.svelte";

  /**
   * The Camera Raw panel (CameraRawControls.swift): ten disclosure sections
   * (Light / Color / Curve / Color Mixer / Color Grading / Effects / Detail
   * / Optics / Geometry / Calibration) with per-group eyes that drop that
   * group from the preview and the commit, the histogram + vectorscope pair
   * fed from the graded preview pixels, the white-balance and defringe
   * eyedroppers, and the Option-drag clipping views. Sliders are
   * CameraRawSlider rows: double-click resets, Option-drag on the tone
   * sliders previews the clipping.
   */

  const session = $derived($filterSession);
  const active = $derived(session?.kind === "cameraRaw");

  interface CurvePoint {
    x: number;
    y: number;
  }

  interface CameraRawShape {
    whiteBalance?: string;
    temperature?: number;
    tint?: number;
    exposure?: number;
    contrast?: number;
    highlights?: number;
    shadows?: number;
    whites?: number;
    blacks?: number;
    vibrance?: number;
    saturation?: number;
    texture?: number;
    clarity?: number;
    dehaze?: number;
    glow?: number;
    glowStyle?: number;
    glowRange?: number;
    glowSpread?: number;
    glowWarmth?: number;
    vignetteAmount?: number;
    vignetteStyle?: number;
    vignetteMidpoint?: number;
    vignetteRoundness?: number;
    vignetteFeather?: number;
    vignetteHighlights?: number;
    grainAmount?: number;
    grainSize?: number;
    grainRoughness?: number;
    curve?: {
      shadows?: number;
      darks?: number;
      lights?: number;
      highlights?: number;
      shadowSplit?: number;
      darkSplit?: number;
      lightSplit?: number;
      rgb?: CurvePoint[];
      red?: CurvePoint[];
      green?: CurvePoint[];
      blue?: CurvePoint[];
      refineSaturation?: number;
    };
    mixer?: {
      hue?: number[];
      saturation?: number[];
      luminance?: number[];
      points?: Record<string, unknown>[];
    };
    grading?: {
      shadows?: Wheel;
      midtones?: Wheel;
      highlights?: Wheel;
      global?: Wheel;
      blending?: number;
      balance?: number;
    };
    detail?: Record<string, number>;
    optics?: Record<string, number | boolean>;
    geometry?: Record<string, string | number | boolean>;
    calibration?: Record<string, number>;
  }

  interface Wheel {
    hue?: number;
    saturation?: number;
    luminance?: number;
  }

  const cr = $derived(((session?.settings.cameraRaw ?? {}) as CameraRawShape));
  const shows = $derived(session?.settings.cameraRawShows ?? {});

  function setCR(patch: Partial<CameraRawShape>): void {
    updateFilterSettings({ cameraRaw: { ...cr, ...patch } });
  }

  function setCurve(patch: Partial<NonNullable<CameraRawShape["curve"]>>): void {
    setCR({ curve: { ...cr.curve, ...patch } });
  }

  function setDetail(patch: Record<string, number>): void {
    setCR({ detail: { ...cr.detail, ...patch } });
  }

  function setOptics(patch: Record<string, number | boolean>): void {
    setCR({ optics: { ...cr.optics, ...patch } });
  }

  function setGeometry(patch: Record<string, string | number | boolean>): void {
    setCR({ geometry: { ...cr.geometry, ...patch } });
  }

  function setCalibration(patch: Record<string, number>): void {
    setCR({ calibration: { ...cr.calibration, ...patch } });
  }

  function num(obj: Record<string, unknown> | undefined, key: string, fallback: number): number {
    const v = obj?.[key];
    return typeof v === "number" ? v : fallback;
  }

  function bool(obj: Record<string, unknown> | undefined, key: string, fallback: boolean): boolean {
    const v = obj?.[key];
    return typeof v === "boolean" ? v : fallback;
  }

  // ---- Section expansion + eyes -------------------------------------------
  const SECTIONS = [
    "light",
    "color",
    "curve",
    "mixer",
    "grading",
    "effects",
    "detail",
    "optics",
    "geometry",
    "calibration",
  ] as const;
  type Section = (typeof SECTIONS)[number];
  const SECTION_LABELS: Record<Section, string> = {
    light: "光",
    color: "颜色",
    curve: "曲线",
    mixer: "混色器",
    grading: "颜色分级",
    effects: "特效",
    detail: "细节",
    optics: "光学",
    geometry: "几何",
    calibration: "校准",
  };

  let expanded = $state<Set<Section>>(new Set(["light", "color", "grading"]));
  let scopeMode = $state<"histogram" | "vectorscope">("histogram");
  let shadowClipping = $state(false);
  let highlightClipping = $state(false);

  function toggle(section: Section): void {
    const next = new Set(expanded);
    if (next.has(section)) next.delete(section);
    else next.add(section);
    expanded = next;
  }

  function eye(section: Section): void {
    updateFilterSettings({
      cameraRawShows: { ...shows, [section]: !(shows[section] ?? true) },
    });
  }

  function eyeOn(section: Section): boolean {
    return shows[section] ?? true;
  }

  // Alt release clears the Option-drag clip view (installOptionMonitor).
  function onKeyUp(event: KeyboardEvent): void {
    if (event.key !== "Alt" || !session) return;
    if (session.settings.clipping !== 0 || session.settings.sharpenMask) {
      updateFilterSettings({ clipping: 0, sharpenMask: false });
    }
  }

  // ---- Scopes ---------------------------------------------------------------
  let scopeCanvas = $state<HTMLCanvasElement | null>(null);
  let scopeBins = $state<[number[], number[], number[], number[]] | null>(null);
  let scopeCells = $state<number[] | null>(null);

  $effect(() => {
    if (!active) return;
    // Refresh on every landed preview (document.filterRev) and on open.
    void $document.filterRev;
    const sessionNow = session;
    if (!sessionNow) return;
    BridgeService.CameraRawScope()
      .then((raw: string) => {
        const parsed = JSON.parse(raw) as {
          histogram: [number[], number[], number[], number[]];
          vectorscope: number[];
        };
        scopeBins = parsed.histogram;
        scopeCells = parsed.vectorscope;
      })
      .catch((err: unknown) => console.warn("示波器读取失败", err));
  });

  function histPeak(bins: [number[], number[], number[], number[]]): number {
    let peak = 0;
    for (let c = 1; c <= 3; c++) {
      const sorted = [...bins[c]].sort((a, b) => a - b);
      const p95 = sorted[Math.floor(sorted.length * 0.95)] ?? 0;
      peak = Math.max(peak, p95 * 4);
    }
    return Math.max(peak, 1);
  }

  function hueCellColor(column: number, row: number): string {
    const dx = column / 64 - 0.5;
    const dy = row / 64 - 0.5;
    const angle = Math.atan2(dy, dx);
    let hue = (angle / (2 * Math.PI)) * 360;
    if (hue < 0) hue += 360;
    const sat = Math.min(1, (Math.hypot(dx, dy) / 0.48) * 0.9);
    return `hsl(${hue.toFixed(0)}, ${(sat * 100).toFixed(0)}%, 50%)`;
  }

  $effect(() => {
    if (!scopeCanvas) return;
    const ctx = scopeCanvas.getContext("2d");
    if (!ctx) return;
    const w = scopeCanvas.width;
    const h = scopeCanvas.height;
    ctx.clearRect(0, 0, w, h);
    if (scopeMode === "histogram" && scopeBins) {
      const peak = histPeak(scopeBins);
      const colors = ["#c44", "#4a4", "#48c"];
      ctx.globalCompositeOperation = "lighter";
      for (let c = 1; c <= 3; c++) {
        ctx.beginPath();
        ctx.moveTo(0, h);
        for (let i = 0; i < 256; i++) {
          const v = Math.min(1, scopeBins[c][i] / peak);
          ctx.lineTo((i / 255) * w, h - Math.log10(1 + v * 9) / 1 * h);
        }
        ctx.lineTo(w, h);
        ctx.closePath();
        ctx.fillStyle = colors[c - 1];
        ctx.globalAlpha = 0.55;
        ctx.fill();
      }
      ctx.globalAlpha = 1;
      ctx.globalCompositeOperation = "source-over";
    } else if (scopeMode === "vectorscope" && scopeCells) {
      const peak = Math.max(...scopeCells, 1e-6);
      const cell = w / 64;
      for (let row = 0; row < 64; row++) {
        for (let column = 0; column < 64; column++) {
          const amount = scopeCells[row * 64 + column];
          if (amount <= 0) continue;
          ctx.globalAlpha = Math.min(1, amount / peak);
          ctx.fillStyle = hueCellColor(column, row);
          ctx.fillRect(column * cell, row * cell, cell, cell);
        }
      }
      ctx.globalAlpha = 1;
    }
  });

  function toggleClipping(kind: "shadow" | "highlight"): void {
    if (kind === "shadow") shadowClipping = !shadowClipping;
    else highlightClipping = !highlightClipping;
    const clipping = shadowClipping ? 2 : highlightClipping ? 1 : 0;
    updateFilterSettings({ clipping });
  }

  // ---- Mixer / grading helpers ----------------------------------------------
  const MIXER_FAMILIES = ["红", "橙", "黄", "绿", "青", "蓝", "紫", "洋红"];
  const MIXER_CENTERS = [0, 30, 60, 120, 180, 240, 270, 300];
  let mixerTab = $state<"hue" | "saturation" | "luminance">("hue");

  function mixerValues(): number[] {
    const m = cr.mixer ?? {};
    if (mixerTab === "hue") return m.hue ?? [0, 0, 0, 0, 0, 0, 0, 0];
    if (mixerTab === "saturation") return m.saturation ?? [0, 0, 0, 0, 0, 0, 0, 0];
    return m.luminance ?? [0, 0, 0, 0, 0, 0, 0, 0];
  }

  function setMixer(index: number, value: number): void {
    const m = cr.mixer ?? {};
    const key = mixerTab;
    const arr = [...(m[key] ?? [0, 0, 0, 0, 0, 0, 0, 0])];
    arr[index] = value;
    setCR({ mixer: { ...m, [key]: arr } });
  }

  function mixerTrack(family: number): string[] | undefined {
    const degrees = MIXER_CENTERS[family];
    if (mixerTab === "hue") {
      return [hsl(degrees - 50), hsl(degrees + 50)];
    }
    if (mixerTab === "saturation") {
      return ["#8c8c8f", hsl(degrees, 0.9, 0.9)];
    }
    return [hsl(degrees, 0.55, 0.18), hsl(degrees, 0.35, 0.95)];
  }

  function hsl(degrees: number, sat = 0.85, light = 0.9): string {
    const turns = ((degrees % 360) + 360) % 360;
    return `hsl(${turns.toFixed(0)}, ${(sat * 100).toFixed(0)}%, ${(light * 100).toFixed(0)}%)`;
  }

  function wheel(section: "shadows" | "midtones" | "highlights" | "global"): Wheel {
    return cr.grading?.[section] ?? {};
  }

  function setWheel(section: "shadows" | "midtones" | "highlights" | "global", patch: Wheel): void {
    const g = cr.grading ?? {};
    setCR({ grading: { ...g, [section]: { ...wheel(section), ...patch } } });
  }

  function wheelTrack(w: Wheel): string[] {
    const hue = w.hue ?? 0;
    return [hsl(hue, 0.9, 0.2), hsl(hue, 0.9, 0.75)];
  }

  const WHEEL_SECTIONS = [
    ["shadows", "阴影"],
    ["midtones", "中间调"],
    ["highlights", "高光"],
    ["global", "全局"],
  ] as const;

  // ---- Point colors -----------------------------------------------------------
  function pointColors(): Record<string, unknown>[] {
    return (cr.mixer?.points as Record<string, unknown>[] | undefined) ?? [];
  }

  function setPointColors(points: Record<string, unknown>[]): void {
    setCR({ mixer: { ...(cr.mixer ?? {}), points } });
  }

  function addPointColor(): void {
    const points = pointColors();
    if (points.length >= 8) return;
    setPointColors([...points, {
      hue: 0, saturation: 0, luminance: 0,
      hueShift: 0, saturationShift: 0, luminanceShift: 0,
      hueRange: 30, saturationRange: 0.4, luminanceRange: 0.4,
      visualize: false,
    }]);
  }

  let curveChannel = $state(0);
  const CURVE_CHANNELS = ["rgb", "red", "green", "blue"] as const;
  const CURVE_LABELS = ["RGB", "红", "绿", "蓝"] as const;

  function curvePoints(): CurvePoint[] {
    const c = cr.curve;
    const list = c?.[CURVE_CHANNELS[curveChannel]];
    if (!list || list.length < 2) {
      return [
        { x: 0, y: 0 },
        { x: 255, y: 255 },
      ];
    }
    return list;
  }

  const styleGlow = $derived((cr.glowStyle ?? 0) === 1 ? "bloom" : (cr.glowStyle ?? 0) === 2 ? "halation" : "diffusion");
</script>

<svelte:window onkeyup={onKeyUp} />

{#if active && session}
  <div class="panel" role="complementary" aria-label="Camera Raw">
    <header class="panel-header">
      <h2>Camera Raw</h2>
      <span class="badge">实时预览</span>
      {#if session.preparing}
        <span class="preparing">渲染中…</span>
      {/if}
    </header>

    <!-- Scope: histogram / vectorscope of the graded pixels -->
    <div class="scope">
      <canvas bind:this={scopeCanvas} width="256" height="84"></canvas>
      <div class="scope-actions">
        <button
          class:active={scopeMode === "vectorscope"}
          title="直方图 / 矢量示波器切换"
          onclick={() => (scopeMode = scopeMode === "histogram" ? "vectorscope" : "histogram")}
        >
          {scopeMode === "histogram" ? "直方图" : "示波器"}
        </button>
        <button
          class:active={shadowClipping}
          title="阴影裁剪指示（蓝色）"
          onclick={() => toggleClipping("shadow")}
        >
          阴影裁剪
        </button>
        <button
          class:active={highlightClipping}
          title="高光裁剪指示（红色）"
          onclick={() => toggleClipping("highlight")}
        >
          高光裁剪
        </button>
      </div>
    </div>

    <div class="sections">
      {#each SECTIONS as section (section)}
        <div class="section">
          <div class="section-head">
            <button class="disclose" onclick={() => toggle(section)} aria-label={SECTION_LABELS[section]}>
              {expanded.has(section) ? "▾" : "▸"}
              {SECTION_LABELS[section]}
            </button>
            <button
              class="eye"
              class:off={!eyeOn(section)}
              title={eyeOn(section) ? `预览中隐藏${SECTION_LABELS[section]}` : `预览中显示${SECTION_LABELS[section]}`}
              onclick={() => eye(section)}
            >
              {eyeOn(section) ? "◉" : "◌"}
            </button>
          </div>

          {#if expanded.has(section)}
            <div class="controls">
              {#if section === "light"}
                <CameraRawSlider label="曝光" value={cr.exposure ?? 0} min={-5} max={5} step={0.05} decimals={2}
                  track={["#3876f2", "#fad12e"]} help="以线性光档位整体提亮或压暗。"
                  onalt={(_, c) => updateFilterSettings({ clipping: c })}
                  oninput={(v) => setCR({ exposure: v })}
                  onreset={() => setCR({ exposure: 0 })} />
                <CameraRawSlider label="对比度" value={cr.contrast ?? 0} min={-100} max={100}
                  help="以中间灰为轴拉伸或收缩色调。"
                  oninput={(v) => setCR({ contrast: v })} onreset={() => setCR({ contrast: 0 })} />
                <CameraRawSlider label="高光" value={cr.highlights ?? 0} min={-100} max={100}
                  help="回收或提亮最亮的区域。"
                  onalt={(_, c) => updateFilterSettings({ clipping: c })}
                  oninput={(v) => setCR({ highlights: v })} onreset={() => setCR({ highlights: 0 })} />
                <CameraRawSlider label="阴影" value={cr.shadows ?? 0} min={-100} max={100}
                  help="提亮或压实最暗的区域。"
                  onalt={(_, c) => updateFilterSettings({ clipping: c })}
                  oninput={(v) => setCR({ shadows: v })} onreset={() => setCR({ shadows: 0 })} />
                <CameraRawSlider label="白色" value={cr.whites ?? 0} min={-100} max={100}
                  help="白点定位。"
                  onalt={(_, c) => updateFilterSettings({ clipping: c })}
                  oninput={(v) => setCR({ whites: v })} onreset={() => setCR({ whites: 0 })} />
                <CameraRawSlider label="黑色" value={cr.blacks ?? 0} min={-100} max={100}
                  help="黑点定位。"
                  onalt={(_, c) => updateFilterSettings({ clipping: c })}
                  oninput={(v) => setCR({ blacks: v })} onreset={() => setCR({ blacks: 0 })} />
              {:else if section === "color"}
                <div class="row">
                  <span class="row-label">白平衡</span>
                  <select
                    value={String(cr.whiteBalance ?? "Custom")}
                    onchange={(e) => setCR({ whiteBalance: e.currentTarget.value })}
                  >
                    <option value="Custom">自定义</option>
                    <option value="Auto">自动</option>
                  </select>
                  <button
                    class:armed={$armedEyedropper === "wb"}
                    title="白平衡吸管：点画布取样应为中性的像素"
                    onclick={() => armedEyedropper.set($armedEyedropper === "wb" ? null : "wb")}
                  >吸管</button>
                  <button title="灰世界自动白平衡" onclick={() => void autoWhiteBalance()}>自动</button>
                </div>
                <CameraRawSlider label="色温" value={cr.temperature ?? 0} min={-100} max={100}
                  track={["#3876f2", "#fad12e"]} help="蓝—黄偏移。"
                  oninput={(v) => setCR({ temperature: v, whiteBalance: "Custom" })}
                  onreset={() => setCR({ temperature: 0 })} />
                <CameraRawSlider label="色调" value={cr.tint ?? 0} min={-100} max={100}
                  track={["#48b357", "#b366a3"]} help="绿—洋红偏移。"
                  oninput={(v) => setCR({ tint: v, whiteBalance: "Custom" })}
                  onreset={() => setCR({ tint: 0 })} />
                <CameraRawSlider label="自然饱和度" value={cr.vibrance ?? 0} min={-100} max={100}
                  help="对安静的颜色加得更多，保护肤色。"
                  oninput={(v) => setCR({ vibrance: v })} onreset={() => setCR({ vibrance: 0 })} />
                <CameraRawSlider label="饱和度" value={cr.saturation ?? 0} min={-100} max={100}
                  track={["#9e9ea3", "#db2e33"]} help="同幅度增强或减弱所有颜色。"
                  oninput={(v) => setCR({ saturation: v })} onreset={() => setCR({ saturation: 0 })} />
              {:else if section === "curve"}
                {@const c = cr.curve ?? {}}
                <CameraRawSlider label="阴影" value={c.shadows ?? 0} min={-100} max={100}
                  oninput={(v) => setCurve({ shadows: v })} onreset={() => setCurve({ shadows: 0 })} />
                <CameraRawSlider label="暗部" value={c.darks ?? 0} min={-100} max={100}
                  oninput={(v) => setCurve({ darks: v })} onreset={() => setCurve({ darks: 0 })} />
                <CameraRawSlider label="亮部" value={c.lights ?? 0} min={-100} max={100}
                  oninput={(v) => setCurve({ lights: v })} onreset={() => setCurve({ lights: 0 })} />
                <CameraRawSlider label="高光" value={c.highlights ?? 0} min={-100} max={100}
                  oninput={(v) => setCurve({ highlights: v })} onreset={() => setCurve({ highlights: 0 })} />
                <CameraRawSlider label="阴影分割" value={c.shadowSplit ?? 25} min={5} max={90} reset={25}
                  oninput={(v) => setCurve({ shadowSplit: v })} onreset={() => setCurve({ shadowSplit: 25 })} />
                <CameraRawSlider label="中间分割" value={c.darkSplit ?? 50} min={7} max={95} reset={50}
                  oninput={(v) => setCurve({ darkSplit: v })} onreset={() => setCurve({ darkSplit: 50 })} />
                <CameraRawSlider label="高光分割" value={c.lightSplit ?? 75} min={9} max={98} reset={75}
                  oninput={(v) => setCurve({ lightSplit: v })} onreset={() => setCurve({ lightSplit: 75 })} />
                <CameraRawSlider label="精修饱和" value={c.refineSaturation ?? 0} min={-100} max={100}
                  help="复合曲线附带多少饱和度变化。"
                  oninput={(v) => setCurve({ refineSaturation: v })}
                  onreset={() => setCurve({ refineSaturation: 0 })} />
                <div class="tabs">
                  {#each CURVE_LABELS as name, i (name)}
                    <button class:active={curveChannel === i} onclick={() => (curveChannel = i)}>{name}</button>
                  {/each}
                </div>
                <CurveEditor
                  points={curvePoints()}
                  onchange={(next) =>
                    setCurve({ [CURVE_CHANNELS[curveChannel]]: next } as NonNullable<CameraRawShape["curve"]>)}
                />
              {:else if section === "mixer"}
                <div class="tabs">
                  <button class:active={mixerTab === "hue"} onclick={() => (mixerTab = "hue")}>色相</button>
                  <button class:active={mixerTab === "saturation"} onclick={() => (mixerTab = "saturation")}>饱和度</button>
                  <button class:active={mixerTab === "luminance"} onclick={() => (mixerTab = "luminance")}>明度</button>
                </div>
                {#each MIXER_FAMILIES as family, i (family)}
                  <CameraRawSlider label={family} value={mixerValues()[i] ?? 0} min={-100} max={100}
                    track={mixerTrack(i)}
                    oninput={(v) => setMixer(i, v)}
                    onreset={() => setMixer(i, 0)} />
                {/each}
                <div class="row">
                  <span class="row-label">点色 ({pointColors().length}/8)</span>
                  <button onclick={addPointColor} disabled={pointColors().length >= 8}>添加点色</button>
                </div>
                {#each pointColors() as point, pi (pi)}
                  <details class="point-color">
                    <summary>点色 {pi + 1}</summary>
                    <CameraRawSlider label="色相" value={num(point, "hue", 0)} min={0} max={360} reset={0}
                      oninput={(v) => setPointColors(pointColors().map((p, i) => (i === pi ? { ...p, hue: v } : p)))}
                      onreset={() => setPointColors(pointColors().map((p, i) => (i === pi ? { ...p, hue: 0 } : p)))} />
                    <CameraRawSlider label="色相偏移" value={num(point, "hueShift", 0)} min={-100} max={100}
                      oninput={(v) => setPointColors(pointColors().map((p, i) => (i === pi ? { ...p, hueShift: v } : p)))}
                      onreset={() => setPointColors(pointColors().map((p, i) => (i === pi ? { ...p, hueShift: 0 } : p)))} />
                    <CameraRawSlider label="饱和偏移" value={num(point, "saturationShift", 0)} min={-100} max={100}
                      oninput={(v) => setPointColors(pointColors().map((p, i) => (i === pi ? { ...p, saturationShift: v } : p)))}
                      onreset={() => setPointColors(pointColors().map((p, i) => (i === pi ? { ...p, saturationShift: 0 } : p)))} />
                    <CameraRawSlider label="明度偏移" value={num(point, "luminanceShift", 0)} min={-100} max={100}
                      oninput={(v) => setPointColors(pointColors().map((p, i) => (i === pi ? { ...p, luminanceShift: v } : p)))}
                      onreset={() => setPointColors(pointColors().map((p, i) => (i === pi ? { ...p, luminanceShift: 0 } : p)))} />
                    <CameraRawSlider label="色相范围" value={num(point, "hueRange", 30)} min={5} max={180} reset={30}
                      oninput={(v) => setPointColors(pointColors().map((p, i) => (i === pi ? { ...p, hueRange: v } : p)))}
                      onreset={() => setPointColors(pointColors().map((p, i) => (i === pi ? { ...p, hueRange: 30 } : p)))} />
                    <label class="check">
                      <input
                        type="checkbox"
                        checked={point.visualize === true}
                        onchange={(e) =>
                          setPointColors(pointColors().map((p, i) => (i === pi ? { ...p, visualize: e.currentTarget.checked } : p)))}
                      />
                      可视化（面板外的像素压暗）
                    </label>
                  </details>
                {/each}
              {:else if section === "grading"}
                {#each WHEEL_SECTIONS as [key, label] (key)}
                  {@const w = wheel(key)}
                  <CameraRawSlider label={`${label}·色相`} value={w.hue ?? 0} min={0} max={360}
                    track={wheelTrack(w)}
                    oninput={(v) => setWheel(key, { hue: v })}
                    onreset={() => setWheel(key, { hue: 0 })} />
                  <CameraRawSlider label={`${label}·饱和`} value={w.saturation ?? 0} min={0} max={100}
                    oninput={(v) => setWheel(key, { saturation: v })}
                    onreset={() => setWheel(key, { saturation: 0 })} />
                  <CameraRawSlider label={`${label}·明度`} value={w.luminance ?? 0} min={-100} max={100}
                    oninput={(v) => setWheel(key, { luminance: v })}
                    onreset={() => setWheel(key, { luminance: 0 })} />
                {/each}
                <CameraRawSlider label="混合" value={cr.grading?.blending ?? 50} min={0} max={100} reset={50}
                  help="三个色调轮的重叠程度。"
                  oninput={(v) => setCR({ grading: { ...(cr.grading ?? {}), blending: v } })}
                  onreset={() => setCR({ grading: { ...(cr.grading ?? {}), blending: 50 } })} />
                <CameraRawSlider label="平衡" value={cr.grading?.balance ?? 0} min={-100} max={100}
                  help="负值偏阴影，正值偏高光。"
                  oninput={(v) => setCR({ grading: { ...(cr.grading ?? {}), balance: v } })}
                  onreset={() => setCR({ grading: { ...(cr.grading ?? {}), balance: 0 } })} />
              {:else if section === "effects"}
                <CameraRawSlider label="质感" value={cr.texture ?? 0} min={-100} max={100}
                  oninput={(v) => setCR({ texture: v })} onreset={() => setCR({ texture: 0 })} />
                <CameraRawSlider label="清晰度" value={cr.clarity ?? 0} min={-100} max={100}
                  oninput={(v) => setCR({ clarity: v })} onreset={() => setCR({ clarity: 0 })} />
                <CameraRawSlider label="去雾" value={cr.dehaze ?? 0} min={-100} max={100}
                  oninput={(v) => setCR({ dehaze: v })} onreset={() => setCR({ dehaze: 0 })} />
                <CameraRawSlider label="光晕" value={cr.glow ?? 0} min={0} max={100}
                  oninput={(v) => setCR({ glow: v })} onreset={() => setCR({ glow: 0 })} />
                <div class="row">
                  <span class="row-label">光晕样式</span>
                  <select value={styleGlow} onchange={(e) => setCR({
                    glowStyle: e.currentTarget.value === "bloom" ? 1 : e.currentTarget.value === "halation" ? 2 : 0,
                  })}>
                    <option value="diffusion">扩散</option>
                    <option value="bloom">泛光</option>
                    <option value="halation">光晕（红边）</option>
                  </select>
                </div>
                <CameraRawSlider label="光晕范围" value={cr.glowRange ?? 0} min={-100} max={100}
                  oninput={(v) => setCR({ glowRange: v })} onreset={() => setCR({ glowRange: 0 })} />
                <CameraRawSlider label="光晕扩散" value={cr.glowSpread ?? 0} min={-100} max={100}
                  oninput={(v) => setCR({ glowSpread: v })} onreset={() => setCR({ glowSpread: 0 })} />
                <CameraRawSlider label="光晕暖度" value={cr.glowWarmth ?? 0} min={-100} max={100}
                  oninput={(v) => setCR({ glowWarmth: v })} onreset={() => setCR({ glowWarmth: 0 })} />
                <CameraRawSlider label="暗角" value={cr.vignetteAmount ?? 0} min={-100} max={100}
                  oninput={(v) => setCR({ vignetteAmount: v })} onreset={() => setCR({ vignetteAmount: 0 })} />
                <div class="row">
                  <span class="row-label">暗角样式</span>
                  <select
                    value={String(cr.vignetteStyle ?? 0)}
                    onchange={(e) => setCR({ vignetteStyle: +e.currentTarget.value })}
                  >
                    <option value="0">高光优先</option>
                    <option value="1">颜色优先</option>
                    <option value="2">绘画叠加</option>
                  </select>
                </div>
                <CameraRawSlider label="中点" value={cr.vignetteMidpoint ?? 50} min={0} max={100} reset={50}
                  oninput={(v) => setCR({ vignetteMidpoint: v })} onreset={() => setCR({ vignetteMidpoint: 50 })} />
                <CameraRawSlider label="圆度" value={cr.vignetteRoundness ?? 0} min={-100} max={100}
                  oninput={(v) => setCR({ vignetteRoundness: v })} onreset={() => setCR({ vignetteRoundness: 0 })} />
                <CameraRawSlider label="羽化" value={cr.vignetteFeather ?? 50} min={0} max={100} reset={50}
                  oninput={(v) => setCR({ vignetteFeather: v })} onreset={() => setCR({ vignetteFeather: 50 })} />
                <CameraRawSlider label="高光保护" value={cr.vignetteHighlights ?? 0} min={0} max={100}
                  oninput={(v) => setCR({ vignetteHighlights: v })} onreset={() => setCR({ vignetteHighlights: 0 })} />
                <CameraRawSlider label="颗粒" value={cr.grainAmount ?? 0} min={0} max={100}
                  oninput={(v) => setCR({ grainAmount: v })} onreset={() => setCR({ grainAmount: 0 })} />
                <CameraRawSlider label="颗粒大小" value={cr.grainSize ?? 25} min={0} max={100} reset={25}
                  oninput={(v) => setCR({ grainSize: v })} onreset={() => setCR({ grainSize: 25 })} />
                <CameraRawSlider label="粗糙度" value={cr.grainRoughness ?? 50} min={0} max={100} reset={50}
                  oninput={(v) => setCR({ grainRoughness: v })} onreset={() => setCR({ grainRoughness: 50 })} />
              {:else if section === "detail"}
                {@const d = cr.detail ?? {}}
                <CameraRawSlider label="锐化数量" value={num(d, "sharpenAmount", 0)} min={0} max={150}
                  oninput={(v) => setDetail({ sharpenAmount: v })} onreset={() => setDetail({ sharpenAmount: 0 })} />
                <CameraRawSlider label="锐化半径" value={num(d, "sharpenRadius", 10)} min={0} max={100} reset={10}
                  oninput={(v) => setDetail({ sharpenRadius: v })} onreset={() => setDetail({ sharpenRadius: 10 })} />
                <CameraRawSlider label="锐化细节" value={num(d, "sharpenDetail", 25)} min={0} max={100} reset={25}
                  oninput={(v) => setDetail({ sharpenDetail: v })} onreset={() => setDetail({ sharpenDetail: 25 })} />
                <CameraRawSlider label="锐化蒙版" value={num(d, "sharpenMasking", 0)} min={0} max={100}
                  help="按住 Option 预览蒙版（白=锐化，黑=保护）。"
                  onalt={(_, c) => updateFilterSettings({ sharpenMask: c === 1 })}
                  oninput={(v) => setDetail({ sharpenMasking: v })}
                  onreset={() => setDetail({ sharpenMasking: 0 })} />
                <CameraRawSlider label="亮度降噪" value={num(d, "noiseLuminance", 0)} min={0} max={100}
                  oninput={(v) => setDetail({ noiseLuminance: v })} onreset={() => setDetail({ noiseLuminance: 0 })} />
                <CameraRawSlider label="亮度细节" value={num(d, "noiseLuminanceDetail", 50)} min={0} max={100} reset={50}
                  oninput={(v) => setDetail({ noiseLuminanceDetail: v })} onreset={() => setDetail({ noiseLuminanceDetail: 50 })} />
                <CameraRawSlider label="亮度对比" value={num(d, "noiseLuminanceContrast", 0)} min={0} max={100}
                  oninput={(v) => setDetail({ noiseLuminanceContrast: v })} onreset={() => setDetail({ noiseLuminanceContrast: 0 })} />
                <CameraRawSlider label="颜色降噪" value={num(d, "noiseColor", 0)} min={0} max={100}
                  oninput={(v) => setDetail({ noiseColor: v })} onreset={() => setDetail({ noiseColor: 0 })} />
                <CameraRawSlider label="颜色细节" value={num(d, "noiseColorDetail", 50)} min={0} max={100} reset={50}
                  oninput={(v) => setDetail({ noiseColorDetail: v })} onreset={() => setDetail({ noiseColorDetail: 50 })} />
                <CameraRawSlider label="颜色平滑" value={num(d, "noiseColorSmoothness", 50)} min={0} max={100} reset={50}
                  oninput={(v) => setDetail({ noiseColorSmoothness: v })} onreset={() => setDetail({ noiseColorSmoothness: 50 })} />
              {:else if section === "optics"}
                {@const o = cr.optics ?? {}}
                <label class="check">
                  <input type="checkbox" checked={bool(o, "removeChromaticAberration", false)}
                    onchange={(e) => setOptics({ removeChromaticAberration: e.currentTarget.checked })} />
                  移除色差
                </label>
                <label class="check">
                  <input type="checkbox" checked={bool(o, "enableLensProfile", false)}
                    onchange={(e) => setOptics({ enableLensProfile: e.currentTarget.checked })} />
                  启用镜头配置文件
                </label>
                <CameraRawSlider label="配置扭曲" value={num(o, "profileDistortion", 100)} min={0} max={100} reset={100}
                  oninput={(v) => setOptics({ profileDistortion: v })} onreset={() => setOptics({ profileDistortion: 100 })} />
                <CameraRawSlider label="配置暗角" value={num(o, "profileVignetting", 100)} min={0} max={100} reset={100}
                  oninput={(v) => setOptics({ profileVignetting: v })} onreset={() => setOptics({ profileVignetting: 100 })} />
                <CameraRawSlider label="扭曲" value={num(o, "distortion", 0)} min={-100} max={100}
                  oninput={(v) => setOptics({ distortion: v })} onreset={() => setOptics({ distortion: 0 })} />
                <div class="row">
                  <span class="row-label">紫边</span>
                  <button
                    class:armed={$armedEyedropper === "defringe"}
                    title="去边吸管：点画布上的紫/绿边"
                    onclick={() => armedEyedropper.set($armedEyedropper === "defringe" ? null : "defringe")}
                  >吸管</button>
                </div>
                <CameraRawSlider label="紫边量" value={num(o, "purpleAmount", 0)} min={0} max={100}
                  oninput={(v) => setOptics({ purpleAmount: v })} onreset={() => setOptics({ purpleAmount: 0 })} />
                <CameraRawSlider label="紫边低" value={num(o, "purpleHueLow", 270)} min={0} max={360} reset={270}
                  oninput={(v) => setOptics({ purpleHueLow: v })} onreset={() => setOptics({ purpleHueLow: 270 })} />
                <CameraRawSlider label="紫边高" value={num(o, "purpleHueHigh", 310)} min={0} max={360} reset={310}
                  oninput={(v) => setOptics({ purpleHueHigh: v })} onreset={() => setOptics({ purpleHueHigh: 310 })} />
                <CameraRawSlider label="绿边量" value={num(o, "greenAmount", 0)} min={0} max={100}
                  oninput={(v) => setOptics({ greenAmount: v })} onreset={() => setOptics({ greenAmount: 0 })} />
                <CameraRawSlider label="绿边低" value={num(o, "greenHueLow", 60)} min={0} max={360} reset={60}
                  oninput={(v) => setOptics({ greenHueLow: v })} onreset={() => setOptics({ greenHueLow: 60 })} />
                <CameraRawSlider label="绿边高" value={num(o, "greenHueHigh", 120)} min={0} max={360} reset={120}
                  oninput={(v) => setOptics({ greenHueHigh: v })} onreset={() => setOptics({ greenHueHigh: 120 })} />
                <CameraRawSlider label="镜头暗角" value={num(o, "vignetteAmount", 0)} min={-100} max={100}
                  help="校正镜头暗角（正提亮，负压暗）。"
                  oninput={(v) => setOptics({ vignetteAmount: v })} onreset={() => setOptics({ vignetteAmount: 0 })} />
                <CameraRawSlider label="暗角中点" value={num(o, "vignetteMidpoint", 50)} min={0} max={100} reset={50}
                  oninput={(v) => setOptics({ vignetteMidpoint: v })} onreset={() => setOptics({ vignetteMidpoint: 50 })} />
              {:else if section === "geometry"}
                {@const g = cr.geometry ?? {}}
                <div class="row">
                  <span class="row-label">Upright</span>
                  <select value={String(g.upright ?? "Off")} onchange={(e) => setGeometry({ upright: e.currentTarget.value })}>
                    <option value="Off">关闭</option>
                    <option value="Guided">引导线</option>
                  </select>
                </div>
                <div class="row">
                  <span class="row-label">投影</span>
                  <select value={String(g.projection ?? "Perspective")} onchange={(e) => setGeometry({ projection: e.currentTarget.value })}>
                    <option value="Perspective">透视</option>
                    <option value="Rectilinear"> rectilinear</option>
                  </select>
                </div>
                <CameraRawSlider label="垂直" value={num(g, "vertical", 0)} min={-100} max={100}
                  oninput={(v) => setGeometry({ vertical: v })} onreset={() => setGeometry({ vertical: 0 })} />
                <CameraRawSlider label="水平" value={num(g, "horizontal", 0)} min={-100} max={100}
                  oninput={(v) => setGeometry({ horizontal: v })} onreset={() => setGeometry({ horizontal: 0 })} />
                <CameraRawSlider label="旋转" value={num(g, "rotate", 0)} min={-45} max={45}
                  oninput={(v) => setGeometry({ rotate: v })} onreset={() => setGeometry({ rotate: 0 })} />
                <CameraRawSlider label="长宽比" value={num(g, "aspect", 0)} min={-100} max={100}
                  oninput={(v) => setGeometry({ aspect: v })} onreset={() => setGeometry({ aspect: 0 })} />
                <CameraRawSlider label="缩放" value={num(g, "scale", 0)} min={-100} max={100}
                  oninput={(v) => setGeometry({ scale: v })} onreset={() => setGeometry({ scale: 0 })} />
                <CameraRawSlider label="X 偏移" value={num(g, "offsetX", 0)} min={-100} max={100}
                  oninput={(v) => setGeometry({ offsetX: v })} onreset={() => setGeometry({ offsetX: 0 })} />
                <CameraRawSlider label="Y 偏移" value={num(g, "offsetY", 0)} min={-100} max={100}
                  oninput={(v) => setGeometry({ offsetY: v })} onreset={() => setGeometry({ offsetY: 0 })} />
                <label class="check">
                  <input type="checkbox" checked={bool(g, "constrainCrop", false)}
                    onchange={(e) => setGeometry({ constrainCrop: e.currentTarget.checked })} />
                  约束裁切
                </label>
                <p class="hint">引导线绘制随画布工具接线（后续批次）。</p>
              {:else if section === "calibration"}
                {@const c = cr.calibration ?? {}}
                <div class="row">
                  <span class="row-label">进程版本</span>
                  <select value={String(num(c, "process", 6))} onchange={(e) => setCalibration({ process: +e.currentTarget.value })}>
                    {#each [1, 2, 3, 4, 5, 6] as v (v)}
                      <option value={v}>Version {v}</option>
                    {/each}
                  </select>
                </div>
                <CameraRawSlider label="阴影染色" value={num(c, "shadowTint", 0)} min={-100} max={100}
                  oninput={(v) => setCalibration({ shadowTint: v })} onreset={() => setCalibration({ shadowTint: 0 })} />
                <CameraRawSlider label="红·色相" value={num(c, "redHue", 0)} min={-100} max={100}
                  oninput={(v) => setCalibration({ redHue: v })} onreset={() => setCalibration({ redHue: 0 })} />
                <CameraRawSlider label="红·饱和" value={num(c, "redSaturation", 0)} min={-100} max={100}
                  oninput={(v) => setCalibration({ redSaturation: v })} onreset={() => setCalibration({ redSaturation: 0 })} />
                <CameraRawSlider label="绿·色相" value={num(c, "greenHue", 0)} min={-100} max={100}
                  oninput={(v) => setCalibration({ greenHue: v })} onreset={() => setCalibration({ greenHue: 0 })} />
                <CameraRawSlider label="绿·饱和" value={num(c, "greenSaturation", 0)} min={-100} max={100}
                  oninput={(v) => setCalibration({ greenSaturation: v })} onreset={() => setCalibration({ greenSaturation: 0 })} />
                <CameraRawSlider label="蓝·色相" value={num(c, "blueHue", 0)} min={-100} max={100}
                  oninput={(v) => setCalibration({ blueHue: v })} onreset={() => setCalibration({ blueHue: 0 })} />
                <CameraRawSlider label="蓝·饱和" value={num(c, "blueSaturation", 0)} min={-100} max={100}
                  oninput={(v) => setCalibration({ blueSaturation: v })} onreset={() => setCalibration({ blueSaturation: 0 })} />
              {/if}
            </div>
          {/if}
        </div>
      {/each}
    </div>

    <footer class="panel-footer">
      <span class="hint">预览 ≤2048px · 确定按全尺寸提交</span>
      <button onclick={() => void cancelFilter()}>取消</button>
      <button class="primary" onclick={() => void commitFilter()}>确定</button>
    </footer>
  </div>
{/if}

<style>
  .panel {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--bg-panel);
  }

  .panel-header {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px 6px;
  }

  .panel-header h2 {
    font-size: 13px;
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

  .scope {
    padding: 0 12px 8px;
    border-bottom: 1px solid var(--border);
  }

  .scope canvas {
    display: block;
    width: 100%;
    height: 84px;
    background: var(--bg-canvas);
    border: 1px solid var(--border);
    border-radius: 4px;
  }

  .scope-actions {
    display: flex;
    gap: 4px;
    margin-top: 6px;
  }

  .scope-actions button {
    font-size: 11px;
    padding: 2px 8px;
  }

  .sections {
    flex: 1;
    overflow-y: auto;
    padding: 4px 12px;
  }

  .section {
    border-bottom: 1px solid var(--border);
    padding: 2px 0;
  }

  .section-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .disclose {
    flex: 1;
    text-align: left;
    background: transparent;
    border: none;
    font-size: 12px;
    font-weight: 600;
    padding: 6px 2px;
  }

  .eye {
    background: transparent;
    border: none;
    font-size: 13px;
    padding: 0 4px;
  }

  .eye.off {
    opacity: 0.4;
  }

  .controls {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 2px 2px 10px;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
  }

  .row-label {
    min-width: 76px;
    color: var(--text-dim);
    font-size: 12px;
  }

  .row select {
    flex: 1;
  }

  .tabs {
    display: flex;
    gap: 4px;
  }

  .tabs button {
    font-size: 11px;
    padding: 2px 8px;
  }

  .check {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
  }

  .point-color {
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 4px 6px;
  }

  .point-color summary {
    font-size: 11px;
    cursor: pointer;
    color: var(--text-dim);
  }

  .hint {
    color: var(--text-dim);
    font-size: 11px;
    margin: 0;
  }

  .panel-footer {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
    border-top: 1px solid var(--border);
  }

  .panel-footer .hint {
    flex: 1;
  }

  .primary {
    font-weight: 600;
  }

  button.armed {
    outline: 2px solid #48c;
  }
</style>
