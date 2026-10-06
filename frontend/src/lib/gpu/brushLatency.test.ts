import { describe, expect, it, vi } from "vitest";
import {
  BrushLatencyPipeline,
  BRUSH_ECHO_BUDGET_MS,
  ECHO_SAMPLE_RING,
  type BrushSurface,
  type Dab,
  type Segment,
  type StrokeCommit,
} from "./brushLatency";
import {
  BRUSH_UNIFORMS_SIZE,
  BRUSH_WGSL,
  brushUniforms,
  depositionSpacing,
  GAUSS_LEGENDRE_NODES,
  GAUSS_LEGENDRE_WEIGHTS,
  spacingFraction,
} from "./shaders/brush";

function dab(x: number, y: number): Dab {
  return { x, y, radius: 4, alpha: 1 };
}

function dabFrom([ax, ay]: [number, number], [bx, by]: [number, number]): Segment {
  return { ax, ay, bx, by };
}

const A: [number, number] = [2, 16];
const B: [number, number] = [16, 16];
const C: [number, number] = [29, 16];

describe("shaders/brush.ts — WGSL coverage kernel source", () => {
  it("carries the Gauss-Legendre nodes/weights exactly as the TS mirror", () => {
    const nodes = BRUSH_WGSL.match(/const gaussNodes = array<f32, 4>\(([^)]*)\)/);
    const weights = BRUSH_WGSL.match(/const gaussWeights = array<f32, 4>\(([^)]*)\)/);
    expect(nodes).not.toBeNull();
    expect(weights).not.toBeNull();
    expect(nodes![1].split(",").map((v) => Number(v.trim()))).toEqual(GAUSS_LEGENDRE_NODES);
    expect(weights![1].split(",").map((v) => Number(v.trim()))).toEqual(GAUSS_LEGENDRE_WEIGHTS);
  });

  it("keeps the MSL buffer indices as bindings 0-3", () => {
    expect(BRUSH_WGSL).toContain("@group(0) @binding(0) var<storage, read_write> permanent: array<f32>;");
    expect(BRUSH_WGSL).toContain("@group(0) @binding(1) var previewTex: texture_storage_2d<r8unorm, write>;");
    expect(BRUSH_WGSL).toContain("@group(0) @binding(2) var<uniform> u: BrushUniforms;");
    expect(BRUSH_WGSL).toContain("@group(0) @binding(3) var<storage> segments: array<vec4f>;");
  });

  it("ports the soft-tip falloff (k = 2.5) and the hard rim's antialias ramp", () => {
    expect(BRUSH_WGSL).toContain("exp(-2.5 * t * t)");
    expect(BRUSH_WGSL).toContain("(exp(-2.5 * t * t) - exp(-2.5)) / (1.0 - exp(-2.5))");
    expect(BRUSH_WGSL).toContain("clamp((radius - distance) / u.canvas.z + 0.5, 0.0, 1.0)");
  });

  it("ports the density integral: floor, quadrature sum, deposition-spacing divide", () => {
    expect(BRUSH_WGSL).toContain("-log(max(1.0 - brushCoverage(distanceSquared, u), 0.001))");
    expect(BRUSH_WGSL).toContain("integral * halfLength / u.canvas.w");
    expect(BRUSH_WGSL).toContain("tipDensity(dot(p - segment.xy, p - segment.xy), u)"); // initial click
  });

  it("ports the double-buffer math: permanent bakes settled density, preview adds the tail", () => {
    expect(BRUSH_WGSL).toContain("permanent[index] = min(value, 20.0);");
    expect(BRUSH_WGSL).toContain("1.0 - exp(-min(value + tail, 20.0))");
    expect(BRUSH_WGSL).toContain("max(permanent[index], brushCoverage(settled, u))");
    expect(BRUSH_WGSL).toContain("max(value, brushCoverage(tail, u))");
  });

  it("dispatches one 16x16 workgroup per tile, entry point continuousBrush", () => {
    expect(BRUSH_WGSL).toContain("@compute @workgroup_size(16, 16)");
    expect(BRUSH_WGSL).toContain("fn continuousBrush(@builtin(global_invocation_id) pixel: vec3u)");
  });

  it("keeps the 80-byte uniform layout", () => {
    expect(BRUSH_UNIFORMS_SIZE).toBe(80);
  });
});

describe("shaders/brush.ts — deposition spacing", () => {
  it("mirrors BrushStroke.spacingFraction: 2.5% soft, 1.5% hard", () => {
    expect(spacingFraction(0)).toBe(0.025);
    expect(spacingFraction(0.999)).toBe(0.025);
    expect(spacingFraction(1)).toBe(0.015);
  });

  it("floors the spacing at 0.25 document pixels", () => {
    expect(depositionSpacing(10, 0)).toBe(0.25); // 10 × 0.025 < floor
    expect(depositionSpacing(40, 0)).toBe(1); // 40 × 0.025
    expect(depositionSpacing(800, 1)).toBe(12); // 800 × 0.015
  });
});

describe("shaders/brush.ts — uniform packing", () => {
  it("packs radius as diameter / 2 and the antialias width as the smaller mapping axis", () => {
    const u = brushUniforms({
      mapping: [2, 0, 0, 1],
      originX: 256,
      originY: 512,
      tileWidth: 256,
      tileHeight: 256,
      diameter: 800,
      hardness: 0,
      canvasWidth: 4000,
      canvasHeight: 4000,
      settledCount: 3,
      totalCount: 4,
    });
    expect(u.geometry).toEqual([256, 512, 400, 0]);
    expect(u.canvas[2]).toBe(1); // min(hypot(2,0), hypot(0,1))
    expect(u.canvas[3]).toBe(20); // max(0.25, 800 × 0.025)
    expect(u.counts).toEqual([256, 256, 3, 4]);
  });

  it("floors the antialias width at 0.001 so extreme scales never divide by zero", () => {
    const u = brushUniforms({
      mapping: [0.0001, 0, 0, 0.0001],
      originX: 0,
      originY: 0,
      tileWidth: 16,
      tileHeight: 16,
      diameter: 2,
      hardness: 0,
      canvasWidth: 10,
      canvasHeight: 10,
      settledCount: 0,
      totalCount: 0,
    });
    expect(u.canvas[2]).toBe(0.001);
  });
});

/**
 * A model coverage surface: the pipeline's buffer discipline, checked against
 * real pixel math. The per-segment profile is a linear tent (a stand-in for
 * the kernel's Gaussian falloff — its exact shape is the golden-baseline
 * harness's job); the *buffer* rules are the kernel's own: permanent =
 * min(permanent + settled density, 20) at each dispatch, preview = the
 * 8-bit quantization of 1 − exp(−min(permanent + tail, 20)), tail replaced
 * wholesale, commit promoting the preview in one step.
 */
class ModelSurface implements BrushSurface {
  readonly side = 32;
  readonly radius = 4;
  readonly permanentDensity: Float32Array;
  readonly preview: Uint8Array;
  committed: Uint8Array | null = null;
  renders = 0;
  bakes: Segment[][] = [];
  private baked: Segment[] = [];
  private tail: Segment[] = [];

  constructor() {
    this.permanentDensity = new Float32Array(this.side * this.side);
    this.preview = new Uint8Array(this.side * this.side);
  }

  render(update: { settled: readonly Segment[]; tail: readonly Segment[] }): void {
    this.renders += 1;
    this.bakes.push([...update.settled]);
    this.baked.push(...update.settled);
    this.tail = [...update.tail];
    const cap = 20;
    for (let y = 0; y < this.side; y++) {
      for (let x = 0; x < this.side; x++) {
        const i = y * this.side + x;
        const px = x + 0.5;
        const py = y + 0.5;
        // The kernel's bake: only newly settled segments join the permanent buffer.
        let settledDensity = 0;
        for (const s of update.settled) settledDensity += this.profile(px, py, s);
        this.permanentDensity[i] = Math.min(this.permanentDensity[i] + settledDensity, cap);
        let tailDensity = 0;
        for (const s of this.tail) tailDensity += this.profile(px, py, s);
        this.preview[i] = quantize(1 - Math.exp(-Math.min(this.permanentDensity[i] + tailDensity, cap)));
      }
    }
  }

  commit(): void {
    this.committed = this.preview.slice();
  }

  /** The permanent buffer shown as 8-bit coverage, without any tail. */
  permanentCoverage(x: number, y: number): number {
    return quantize(1 - Math.exp(-Math.min(this.permanentDensity[y * this.side + x], 20)));
  }

  previewCoverage(x: number, y: number): number {
    return this.preview[y * this.side + x];
  }

  /** Tent profile: max(0, 1 − distance/radius) to the segment. */
  private profile(px: number, py: number, s: Segment): number {
    const vx = s.bx - s.ax;
    const vy = s.by - s.ay;
    const lengthSquared = vx * vx + vy * vy;
    const t = lengthSquared > 0 ? Math.min(1, Math.max(0, ((px - s.ax) * vx + (py - s.ay) * vy) / lengthSquared)) : 0;
    const dx = px - (s.ax + t * vx);
    const dy = py - (s.ay + t * vy);
    return Math.max(0, 1 - Math.hypot(dx, dy) / this.radius);
  }
}

function quantize(v: number): number {
  return Math.round(v * 255);
}

describe("BrushLatencyPipeline — state machine", () => {
  it("begins a stroke with the initial click settled as a degenerate segment", () => {
    const pipeline = new BrushLatencyPipeline();
    expect(pipeline.strokeBegin(dab(A[0], A[1]), 100)).toBe("began");
    expect(pipeline.active).toBe(true);
    expect(pipeline.strokeId).toBe(1);
    expect(pipeline.settled).toEqual([dabFrom(A, A)]);
    expect(pipeline.tail).toEqual([]);
    expect(pipeline.segments).toEqual([dabFrom(A, A)]);
    expect(pipeline.dabs).toEqual([dab(A[0], A[1])]);
  });

  it("refuses a second begin while a stroke is live", () => {
    const pipeline = new BrushLatencyPipeline();
    pipeline.strokeBegin(dab(A[0], A[1]), 100);
    expect(pipeline.strokeBegin(dab(B[0], B[1]), 110)).toBe("already-stroking");
    expect(pipeline.strokeId).toBe(1);
    expect(pipeline.dabs).toHaveLength(1);
  });

  it("promotes the previous tail to settled and lays a new tail per point", () => {
    const pipeline = new BrushLatencyPipeline();
    pipeline.strokeBegin(dab(A[0], A[1]), 100);
    // appendContinuous with two samples: nothing settled yet, the run is the tail.
    expect(pipeline.strokePoint(dab(B[0], B[1]), 110)).toBe("accepted");
    expect(pipeline.settled).toEqual([dabFrom(A, A)]);
    expect(pipeline.tail).toEqual([dabFrom(A, B)]);
    // The third sample bakes the previous tail and re-lays the provisional run.
    expect(pipeline.strokePoint(dab(C[0], C[1]), 120)).toBe("accepted");
    expect(pipeline.settled).toEqual([dabFrom(A, A), dabFrom(A, B)]);
    expect(pipeline.tail).toEqual([dabFrom(B, C)]);
    // The kernel's counts: counts.z = settled length, counts.w = total length.
    expect(pipeline.segments).toEqual([dabFrom(A, A), dabFrom(A, B), dabFrom(B, C)]);
    expect(pipeline.dabs).toHaveLength(3);
  });

  it("drops repeated points before they double-deposit the click density", () => {
    const pipeline = new BrushLatencyPipeline();
    pipeline.strokeBegin(dab(A[0], A[1]), 100);
    pipeline.strokePoint(dab(B[0], B[1]), 110);
    expect(pipeline.strokePoint(dab(B[0], B[1]), 120)).toBe("duplicate");
    expect(pipeline.dabs).toHaveLength(2);
    expect(pipeline.tail).toEqual([dabFrom(A, B)]);
    expect(pipeline.benchmark.all).toHaveLength(1);
  });

  it("drops late events and counts them instead of chasing the tail", () => {
    const pipeline = new BrushLatencyPipeline();
    pipeline.strokeBegin(dab(A[0], A[1]), 100);
    pipeline.strokePoint(dab(B[0], B[1]), 120);
    expect(pipeline.strokePoint(dab(C[0], C[1]), 115)).toBe("dropped-late");
    expect(pipeline.droppedLate).toBe(1);
    expect(pipeline.tail).toEqual([dabFrom(A, B)]);
    expect(pipeline.dabs).toHaveLength(2);
    // Equal timestamps are coalesced-same-clock events, not late ones.
    expect(pipeline.strokePoint(dab(C[0], C[1]), 120)).toBe("accepted");
    expect(pipeline.droppedLate).toBe(1);
  });

  it("drops non-finite dabs and counts them", () => {
    const pipeline = new BrushLatencyPipeline();
    expect(pipeline.strokeBegin(dab(Number.NaN, 0), 100)).toBe("dropped-invalid");
    expect(pipeline.active).toBe(false);
    pipeline.strokeBegin(dab(A[0], A[1]), 100);
    expect(pipeline.strokePoint({ x: 1, y: 2, radius: Number.POSITIVE_INFINITY, alpha: 1 }, 110)).toBe(
      "dropped-invalid",
    );
    expect(pipeline.strokePoint(dab(B[0], B[1]), Number.NaN)).toBe("dropped-invalid");
    expect(pipeline.droppedInvalid).toBe(3);
    expect(pipeline.dabs).toHaveLength(1);
  });

  it("ignores points while idle", () => {
    const pipeline = new BrushLatencyPipeline();
    expect(pipeline.strokePoint(dab(A[0], A[1]), 100)).toBe("idle");
  });

  it("commits on strokeEnd: the tail folds into settled and the stroke retires", () => {
    const pipeline = new BrushLatencyPipeline();
    pipeline.strokeBegin(dab(A[0], A[1]), 100);
    pipeline.strokePoint(dab(B[0], B[1]), 110);
    pipeline.strokePoint(dab(C[0], C[1]), 120);
    const commit = pipeline.strokeEnd();
    expect(commit).not.toBeNull();
    expect(commit!.strokeId).toBe(1);
    expect(commit!.dabs).toHaveLength(3);
    expect(commit!.segments).toEqual([dabFrom(A, A), dabFrom(A, B), dabFrom(B, C)]);
    expect(pipeline.active).toBe(false);
    expect(pipeline.tail).toEqual([]);
    expect(pipeline.settled).toEqual(commit!.segments);
    expect(pipeline.strokeEnd()).toBeNull();
  });

  it("commits a bare click without a tail render", () => {
    const surface = { render: vi.fn(), commit: vi.fn() };
    const pipeline = new BrushLatencyPipeline(surface);
    pipeline.strokeBegin(dab(A[0], A[1]), 100);
    const commit = pipeline.strokeEnd();
    expect(commit!.segments).toEqual([dabFrom(A, A)]);
    // BrushStroke.flushContinuous's n < 2 guard: no final dispatch, straight to commit.
    expect(surface.render).toHaveBeenCalledTimes(1);
    expect(surface.commit).toHaveBeenCalledTimes(1);
  });

  it("starts a fresh segment list for the next stroke and bumps the stroke id", () => {
    const pipeline = new BrushLatencyPipeline();
    pipeline.strokeBegin(dab(A[0], A[1]), 100);
    pipeline.strokeEnd();
    pipeline.strokeBegin(dab(B[0], B[1]), 200);
    expect(pipeline.strokeId).toBe(2);
    expect(pipeline.settled).toEqual([dabFrom(B, B)]);
    expect(pipeline.tail).toEqual([]);
    const commit = pipeline.strokeEnd();
    expect(commit!.strokeId).toBe(2);
    expect(commit!.segments).toEqual([dabFrom(B, B)]);
  });
});

describe("BrushLatencyPipeline — strokeSurface semantics", () => {
  it("fires every commit listener once, in order, with the commit payload", () => {
    const calls: string[] = [];
    const commits: StrokeCommit[] = [];
    const pipeline = new BrushLatencyPipeline(undefined, [
      (c) => {
        calls.push("effects");
        commits.push(c);
      },
      (c) => {
        calls.push("preview");
        commits.push(c);
      },
    ]);
    pipeline.strokeBegin(dab(A[0], A[1]), 100);
    pipeline.strokePoint(dab(B[0], B[1]), 110);
    pipeline.strokeEnd();
    expect(calls).toEqual(["effects", "preview"]);
    expect(commits[0]).toBe(commits[1]);
    expect(commits[0].segments).toEqual([dabFrom(A, A), dabFrom(A, B)]);
    // No commit for dropped events or idle ends.
    pipeline.strokeBegin(dab(A[0], A[1]), 200);
    pipeline.strokePoint(dab(B[0], B[1]), 150);
    pipeline.strokeEnd();
    pipeline.strokeEnd();
    expect(calls).toEqual(["effects", "preview", "effects", "preview"]);
    expect(commits).toHaveLength(4);
  });

  it("drives the surface once per event and once per commit", () => {
    const surface = { render: vi.fn(), commit: vi.fn() };
    const pipeline = new BrushLatencyPipeline(surface);
    pipeline.strokeBegin(dab(A[0], A[1]), 100);
    pipeline.strokePoint(dab(B[0], B[1]), 110);
    pipeline.strokePoint(dab(C[0], C[1]), 120);
    pipeline.strokeEnd();
    // begin + two points + the final tail fold.
    expect(surface.render).toHaveBeenCalledTimes(4);
    expect(surface.render).toHaveBeenNthCalledWith(1, { settled: [dabFrom(A, A)], tail: [] });
    expect(surface.render).toHaveBeenNthCalledWith(2, { settled: [], tail: [dabFrom(A, B)] });
    expect(surface.render).toHaveBeenNthCalledWith(3, { settled: [dabFrom(A, B)], tail: [dabFrom(B, C)] });
    expect(surface.render).toHaveBeenNthCalledWith(4, { settled: [dabFrom(B, C)], tail: [] });
    expect(surface.commit).toHaveBeenCalledTimes(1);
    // Dropped events never touch the surface.
    pipeline.strokeBegin(dab(A[0], A[1]), 200);
    pipeline.strokePoint(dab(B[0], B[1]), 190);
    expect(surface.render).toHaveBeenCalledTimes(5);
  });
});

describe("BrushLatencyPipeline — double-buffer model", () => {
  function strokedSurface(): { surface: ModelSurface; pipeline: BrushLatencyPipeline } {
    const surface = new ModelSurface();
    const pipeline = new BrushLatencyPipeline(surface);
    pipeline.strokeBegin(dab(A[0], A[1]), 100);
    pipeline.strokePoint(dab(B[0], B[1]), 110);
    return { surface, pipeline };
  }

  it("keeps the tail out of the permanent buffer while the stroke is live", () => {
    const { surface } = strokedSurface();
    // Pixel (9,16) sits under the provisional tail A→B, past the settled click.
    const probe = { x: 9, y: 16 };
    expect(surface.previewCoverage(probe.x, probe.y)).toBeGreaterThan(0);
    expect(surface.permanentCoverage(probe.x, probe.y)).toBe(0);
  });

  it("promotes the tail without flicker: the promoted region reads exactly as the preview did", () => {
    const { surface, pipeline } = strokedSurface();
    const probe = { x: 9, y: 16 };
    const before = surface.previewCoverage(probe.x, probe.y);
    pipeline.strokePoint(dab(C[0], C[1]), 120);
    // The old tail is now permanent paint — byte-identical to the preview it
    // produced, never double-counted, never lost.
    expect(surface.permanentCoverage(probe.x, probe.y)).toBe(before);
    expect(surface.previewCoverage(probe.x, probe.y)).toBe(before);
    // And the new tail is likewise still outside the permanent buffer.
    expect(surface.permanentCoverage(24, 16)).toBe(0);
    expect(surface.previewCoverage(24, 16)).toBeGreaterThan(0);
  });

  it("replaces the tail wholesale: no residue where an older tail used to be", () => {
    const { surface, pipeline } = strokedSurface();
    pipeline.strokePoint(dab(C[0], C[1]), 120);
    pipeline.strokePoint(dab(2, 2), 130);
    // The tail jumped to the top-left corner; a pixel under the old B→C run but
    // clear of the new C→D tail must fall back to exactly the permanent paint.
    expect(surface.permanentCoverage(20, 19)).toBeGreaterThan(0);
    const permanentOnly = surface.permanentCoverage(20, 19);
    expect(surface.previewCoverage(20, 19)).toBe(permanentOnly);
  });

  it("commits in one step and the committed output equals the last preview", () => {
    const { surface, pipeline } = strokedSurface();
    pipeline.strokePoint(dab(C[0], C[1]), 120);
    const lastPreview = surface.preview.slice();
    expect(surface.committed).toBeNull();
    pipeline.strokeEnd();
    expect(surface.committed).not.toBeNull();
    expect(surface.committed!).toEqual(lastPreview);
    // The tail's region is permanent paint now, matching the preview byte-for-byte.
    expect(surface.permanentCoverage(24, 16)).toBe(lastPreview[16 * surface.side + 24]);
  });

  it("records exactly one bake per promoted segment", () => {
    const { surface, pipeline } = strokedSurface();
    pipeline.strokePoint(dab(C[0], C[1]), 120);
    pipeline.strokeEnd();
    expect(surface.renders).toBe(4);
    expect(surface.bakes).toEqual([
      [dabFrom(A, A)], // begin: the initial click
      [], // first point: nothing to promote
      [dabFrom(A, B)], // second point: the first run settles
      [dabFrom(B, C)], // strokeEnd: the last run settles
    ]);
  });
});

describe("EchoBenchmark — 4K echo protocol", () => {
  function pipelineWithClock(times: number[]): BrushLatencyPipeline {
    let next = 0;
    return new BrushLatencyPipeline(undefined, [], () => times[next++]!);
  }

  it("samples one event → dab → enqueue breakdown per accepted point", () => {
    // Two accepted points: [t0 guard-entry, t1 decode, t2 model, t3 enqueue];
    // values chosen so every difference is exact in float64.
    const pipeline = pipelineWithClock([0, 0.5, 1, 2, 10, 10.5, 11.5, 13]);
    pipeline.strokeBegin(dab(A[0], A[1]), 100);
    expect(pipeline.strokePoint(dab(B[0], B[1]), 110)).toBe("accepted");
    expect(pipeline.strokePoint(dab(C[0], C[1]), 120)).toBe("accepted");
    const samples = pipeline.benchmark.all;
    expect(samples).toHaveLength(2);
    expect(samples[0]).toEqual({ eventMs: 0.5, dabMs: 0.5, enqueueMs: 1, totalMs: 2 });
    expect(samples[1]).toEqual({ eventMs: 0.5, dabMs: 1, enqueueMs: 1.5, totalMs: 3 });
  });

  it("does not sample dropped, duplicate, or idle events", () => {
    const pipeline = pipelineWithClock([0, 0.1, 0.2, 0.3]);
    pipeline.strokeBegin(dab(A[0], A[1]), 100);
    pipeline.strokePoint(dab(B[0], B[1]), 120);
    pipeline.strokePoint(dab(B[0], B[1]), 121); // duplicate
    pipeline.strokePoint(dab(C[0], C[1]), 110); // late
    expect(pipeline.benchmark.all).toHaveLength(1);
  });

  it("keeps a 120-sample ring, aligned with the lib/gpu sampling protocol", () => {
    const times: number[] = [];
    for (let i = 0; i < ECHO_SAMPLE_RING + 10; i++) times.push(0, 0, 0, 1);
    const pipeline = pipelineWithClock(times);
    pipeline.strokeBegin(dab(A[0], A[1]), 100);
    for (let i = 0; i < ECHO_SAMPLE_RING + 10; i++) {
      pipeline.strokePoint(dab(3 + i, 16), 200 + i);
    }
    expect(pipeline.benchmark.all).toHaveLength(ECHO_SAMPLE_RING);
    expect(pipeline.benchmark.stats().count).toBe(ECHO_SAMPLE_RING);
  });

  it("reports p50/p95 by nearest rank against the 16 ms frame budget", () => {
    const totals = [2, 5, 9, 20];
    const times: number[] = [];
    for (const total of totals) times.push(0, 0, 0, total);
    const pipeline = pipelineWithClock(times);
    pipeline.strokeBegin(dab(A[0], A[1]), 100);
    for (let i = 0; i < totals.length; i++) pipeline.strokePoint(dab(3 + i, 16), 200 + i);
    const stats = pipeline.benchmark.stats();
    expect(stats.p50).toBe(5); // ceil(0.5 × 4)th
    expect(stats.p95).toBe(20); // ceil(0.95 × 4)th
    expect(stats.worst).toBe(20);
    expect(stats.withinBudget).toBe(3);
    expect(stats.budgetMs).toBe(BRUSH_ECHO_BUDGET_MS);
    expect(stats.budgetMs).toBe(16);
  });

  it("reports zeros before any sample", () => {
    const pipeline = pipelineWithClock([]);
    const stats = pipeline.benchmark.stats();
    expect(stats).toEqual({ count: 0, p50: 0, p95: 0, worst: 0, withinBudget: 0, budgetMs: 16 });
  });
});
