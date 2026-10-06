/**
 * Device-independent brush latency pipeline: the state machine between the
 * pointer stream and the WGSL coverage kernel (shaders/brush.ts).
 *
 * The pointer never waits for the GPU. The pipeline keeps no backlog — every
 * accepted event is fully processed through the enqueue before strokePoint
 * returns, so consecutive events cannot trail the cursor — and it owns the
 * permanent/provisional-tail double buffer's *discipline* (the pixels live in
 * the BrushSurface behind it, usually the WebGPU compositor):
 *
 *  - Each accepted point bakes the previous provisional tail into the
 *    permanent buffer and lays the straight run to the pointer as the new
 *    tail — the device-independent reduction of BrushStroke.appendContinuous
 *    (BrushStroke.swift:300-317) with the Catmull-Rom piece replaced by the
 *    chord; the input-spline layer above supplies sub-segments if it wants
 *    the curve, the buffer discipline is identical.
 *  - The tail is replaced wholesale, never accumulated into — the kernel's
 *    preview is a pure function of permanent + the current tail, so replacing
 *    a tail cannot leave old pixels behind (brush-performance.md).
 *  - strokeEnd folds the last tail into permanent in one final dispatch and
 *    commits: no state exists between the last preview and the committed
 *    output, and the committed output equals that preview exactly (the
 *    density cap commutes with the fold: min(min(P,20)+T,20) = min(P+T,20)).
 *    Commit then fires the onCommit listeners — the strokeSurface semantics:
 *    effects/preview rebuild from the new surface, synchronously, like the
 *    original's mouse-up snapshot handoff.
 *
 * Timestamps: pointer events carry a monotonic timestamp; events that arrive
 * out of order (timestamp older than the last accepted one) are dropped and
 * counted — a late event never re-enters the stream to chase the tail.
 *
 * The 4K echo benchmark (parity row V09, brush-performance.md) samples each
 * accepted event's event → dab → enqueue wall time; the p50 must stay under
 * one 16 ms frame on a 4000×4000 document. These are CPU-side latencies up to
 * the enqueue — real GPU timing (timestamp queries) lands with ticket 52.
 */

/** One deposited dab — the pipeline's input currency (Go render.Stroke's dab). */
export interface Dab {
  x: number;
  y: number;
  /** Tip radius in document pixels for this dab (pressure may vary it). */
  radius: number;
  /** Paint strength in [0,1]; 1 for a plain stroke. */
  alpha: number;
}

/**
 * One continuous-tip segment the coverage kernel consumes — the float4
 * (x0, y0, x1, y1) of the WGSL `segments` storage buffer.
 */
export interface Segment {
  ax: number;
  ay: number;
  bx: number;
  by: number;
}

/**
 * The buffer protocol the pipeline drives. The WebGPU compositor implements
 * it with the kernel in shaders/brush.ts; tests substitute a model surface.
 * A render is one dispatch: bake `settled` into the permanent buffer, rebuild
 * the preview from permanent + `tail`. `settled` carries only segments new
 * since the last render (the permanent buffer accumulates), while `tail` is
 * the whole provisional tail (the preview is rebuilt from it, never patched).
 */
export interface BrushSurface {
  render(update: { settled: readonly Segment[]; tail: readonly Segment[] }): void;
  /**
   * One-step commit at stroke end: the last tail joins the permanent buffer
   * and the preview becomes the stroke's committed output. Atomic to the
   * caller — there is no intermediate state to flash.
   */
  commit(): void;
}

/** One sampled pointer event's latency breakdown, in milliseconds. */
export interface EchoSample {
  /** Pointer-event decode and guards. */
  eventMs: number;
  /** Dab → segment/tail buffer update (the model step). */
  dabMs: number;
  /** The coverage dispatch enqueue (surface.render). */
  enqueueMs: number;
  /** Total event → enqueue latency. */
  totalMs: number;
}

export interface EchoStats {
  count: number;
  p50: number;
  p95: number;
  worst: number;
  /** Samples at or under the frame budget. */
  withinBudget: number;
  budgetMs: number;
}

/** One 60 fps frame — the 4K echo budget (parity row V09). */
export const BRUSH_ECHO_BUDGET_MS = 16;
/** Ring size of the echo sampler, aligned with lib/gpu's 120-frame protocol. */
export const ECHO_SAMPLE_RING = 120;

/** Ring-buffer sampler for the event → dab → enqueue latency protocol. */
export class EchoBenchmark {
  private samples: EchoSample[] = [];

  get all(): readonly EchoSample[] {
    return this.samples;
  }

  record(sample: EchoSample): void {
    if (this.samples.length >= ECHO_SAMPLE_RING) this.samples.shift();
    this.samples.push(sample);
  }

  /** p50/p95 by nearest rank over the ring's totals. */
  stats(): EchoStats {
    const totals = this.samples.map((s) => s.totalMs).sort((a, b) => a - b);
    const rank = (q: number) => totals[Math.ceil(q * totals.length) - 1];
    return {
      count: totals.length,
      p50: totals.length > 0 ? rank(0.5) : 0,
      p95: totals.length > 0 ? rank(0.95) : 0,
      worst: totals.length > 0 ? totals[totals.length - 1] : 0,
      withinBudget: this.samples.filter((s) => s.totalMs <= BRUSH_ECHO_BUDGET_MS).length,
      budgetMs: BRUSH_ECHO_BUDGET_MS,
    };
  }
}

export interface StrokeCommit {
  strokeId: number;
  /** Every dab of the stroke, deposition order. */
  dabs: readonly Dab[];
  /** Every segment of the stroke, deposition order — the committed coverage. */
  segments: readonly Segment[];
}

export type StrokeBeginResult = "began" | "already-stroking" | "dropped-invalid";
export type StrokePointResult = "accepted" | "duplicate" | "dropped-late" | "dropped-invalid" | "idle";

/**
 * The latency state machine. Pure TypeScript — it owns segment lists and
 * timestamps, never pixels; hand it a BrushSurface to drive the real buffers.
 */
export class BrushLatencyPipeline {
  private phase: "idle" | "stroking" = "idle";
  private currentId = 0;
  private lastTimestamp = 0;
  private settledSegments: Segment[] = [];
  private tailSegments: Segment[] = [];
  private strokeDabs: Dab[] = [];
  private lateDrops = 0;
  private invalidDrops = 0;
  readonly benchmark = new EchoBenchmark();

  constructor(
    /** The buffers to drive; omitted when the caller reads the segment lists directly. */
    private readonly surface?: BrushSurface,
    /** strokeSurface semantics: commit listeners (effects/preview rebuild). */
    private readonly onCommit: ReadonlyArray<(commit: StrokeCommit) => void> = [],
    /** Monotonic clock for the echo sampler; injectable for tests. */
    private readonly now: () => number = () => performance.now(),
  ) {}

  get active(): boolean {
    return this.phase === "stroking";
  }

  /** 1-based identifier of the current (or last) stroke; commit payloads carry it. */
  get strokeId(): number {
    return this.currentId;
  }

  /** Events that arrived out of order and were dropped, lifetime count. */
  get droppedLate(): number {
    return this.lateDrops;
  }

  /** Events with non-finite coordinates/timestamps, lifetime count. */
  get droppedInvalid(): number {
    return this.invalidDrops;
  }

  /** Segments baked into the permanent buffer — the kernel's counts.z. */
  get settled(): readonly Segment[] {
    return this.settledSegments;
  }

  /** The provisional tail — the kernel's counts.z..counts.w range. */
  get tail(): readonly Segment[] {
    return this.tailSegments;
  }

  /** The full segment list of the current dispatch: settled + tail, in order. */
  get segments(): readonly Segment[] {
    return [...this.settledSegments, ...this.tailSegments];
  }

  get dabs(): readonly Dab[] {
    return this.strokeDabs;
  }

  strokeBegin(dab: Dab, timestamp: number): StrokeBeginResult {
    if (this.phase === "stroking") return "already-stroking";
    if (!usable(dab) || !Number.isFinite(timestamp)) {
      this.invalidDrops += 1;
      return "dropped-invalid";
    }
    this.phase = "stroking";
    this.currentId += 1;
    this.lastTimestamp = timestamp;
    this.settledSegments = [];
    this.tailSegments = [];
    this.strokeDabs = [dab];
    // The initial click: one degenerate segment at the dab — the kernel's
    // "initial click" branch in segmentDensity (length < 1e-6) — baked into
    // the permanent buffer by the first dispatch (BrushStroke.swift:310).
    const click: Segment = { ax: dab.x, ay: dab.y, bx: dab.x, by: dab.y };
    this.settledSegments.push(click);
    this.surface?.render({ settled: [click], tail: [] });
    return "began";
  }

  strokePoint(dab: Dab, timestamp: number): StrokePointResult {
    const started = this.now();
    if (this.phase !== "stroking") return "idle";
    if (!usable(dab) || !Number.isFinite(timestamp)) {
      this.invalidDrops += 1;
      return "dropped-invalid";
    }
    if (timestamp < this.lastTimestamp) {
      this.lateDrops += 1;
      return "dropped-late";
    }
    const previous = this.strokeDabs[this.strokeDabs.length - 1];
    if (previous === undefined) return "idle";
    // A repeated point would deposit the initial-click density twice
    // (BrushStroke.swift:279 drops it the same way).
    if (previous.x === dab.x && previous.y === dab.y) return "duplicate";

    const decoded = this.now();
    // Promote the provisional tail to the permanent buffer and lay the straight
    // run to this point as the new tail (appendContinuous's settled/tail split).
    const promoted = this.tailSegments;
    if (promoted.length > 0) this.settledSegments.push(...promoted);
    this.tailSegments = [{ ax: previous.x, ay: previous.y, bx: dab.x, by: dab.y }];
    this.strokeDabs.push(dab);
    this.lastTimestamp = timestamp;
    const enqueuedModel = this.now();

    this.surface?.render({ settled: promoted, tail: this.tailSegments });
    const done = this.now();
    this.benchmark.record({
      eventMs: decoded - started,
      dabMs: enqueuedModel - decoded,
      enqueueMs: done - enqueuedModel,
      totalMs: done - started,
    });
    return "accepted";
  }

  strokeEnd(): StrokeCommit | null {
    if (this.phase !== "stroking") return null;
    // flushContinuous (BrushStroke.swift:321-326): the last provisional run is
    // baked by one final dispatch with an empty tail; a bare click (no tail)
    // skips straight to commit, as the n < 2 guard does there.
    const promoted = this.tailSegments;
    if (promoted.length > 0) {
      this.settledSegments.push(...promoted);
      this.tailSegments = [];
      this.surface?.render({ settled: promoted, tail: [] });
    }
    // The one-step promote: the surface freezes the preview as the stroke's
    // committed output — atomic, nothing between it and the last preview.
    this.surface?.commit();
    this.phase = "idle";
    const commit: StrokeCommit = {
      strokeId: this.currentId,
      dabs: [...this.strokeDabs],
      segments: [...this.settledSegments],
    };
    for (const listener of this.onCommit) listener(commit);
    return commit;
  }
}

/** NaN and the infinities are rejected, as in BrushStroke.swift:275. */
function usable(dab: Dab): boolean {
  return Number.isFinite(dab.x) && Number.isFinite(dab.y) && Number.isFinite(dab.radius) && Number.isFinite(dab.alpha);
}
