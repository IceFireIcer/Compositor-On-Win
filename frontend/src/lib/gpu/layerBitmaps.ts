/**
 * Revision-keyed layer bitmap store (ticket 11 acceptance: 修订号失效机制
 * ——层变更后仅重取该层位图).
 *
 * Layer pixels travel over the asset-server HTTP semantics (large payloads
 * never go through the Wails JSON bridge). Each layer carries a revision;
 * when only layer A changes, only A's bitmap is fetched again — B stays a
 * cache hit. The texture itself is opaque; the fetcher decodes the HTTP
 * response (createImageBitmap in the runtime, plain values in tests).
 */
import { TextureCache } from "./textureCache";

export interface LayerBitmap<T> {
  bitmap: T;
  revision: number;
  /** Estimated decoded size, feeding the texture cache budget. */
  bytes: number;
}

export type BitmapFetcher<T> = (
  layerId: string,
  signal?: AbortSignal,
  revision?: number,
) => Promise<{
  bitmap: T;
  bytes: number;
}>;

interface PendingFetch<T> {
  promise: Promise<LayerBitmap<T>>;
  signal?: AbortSignal;
}

export class LayerBitmapStore<T> {
  private cache: TextureCache<LayerBitmap<T>>;
  /** In-flight fetches, keyed by layer and requested content revision. */
  private pending = new Map<string, Map<number, PendingFetch<T>>>();
  /** Monotonic tokens prevent an older response from replacing a newer one. */
  private generations = new Map<string, number>();
  private latest = new Map<string, { revision: number; generation: number }>();

  constructor(
    private fetch: BitmapFetcher<T>,
    private budgetBytes: number,
    private onDispose?: (bitmap: T) => void,
  ) {
    this.cache = new TextureCache<LayerBitmap<T>>(budgetBytes, (entry) => this.onDispose?.(entry.bitmap));
  }

  canCache(bytes: number): boolean {
    return bytes <= this.budgetBytes;
  }

  peek(layerId: string): LayerBitmap<T> | undefined {
    return this.cache.get(layerId);
  }

  /**
   * Returns the layer's bitmap for the given revision: served from cache
   * when the revision matches; otherwise the stale entry is evicted and
   * only this layer is fetched again. Concurrent callers share one fetch.
   */
  async get(layerId: string, revision: number, signal?: AbortSignal): Promise<LayerBitmap<T>> {
    const cached = this.cache.get(layerId);
    if (cached && cached.revision === revision) return cached;
    if (!cached || cached.revision < revision) {
      this.cache.delete(layerId); // stale bitmap: release immediately
    }

    const byRevision = this.pending.get(layerId);
    const inflight = byRevision?.get(revision);
    if (inflight && !inflight.signal?.aborted) return inflight.promise;
    // A superseded render may have aborted the shared request just before a
    // newer render asks for the same revision. Let that render start a fresh
    // request instead of inheriting an already-aborted promise.
    if (inflight) byRevision?.delete(revision);

    const generation = (this.generations.get(layerId) ?? 0) + 1;
    this.generations.set(layerId, generation);
    const known = this.latest.get(layerId);
    // Revisions are monotonic at the bridge boundary. A late request for an
    // older revision may still resolve for its caller, but it must not become
    // the cache entry after a newer revision was requested.
    if (!known || revision >= known.revision) {
      this.latest.set(layerId, { revision, generation });
    }

    const task = this.fetchAndStore(layerId, revision, generation, signal);
    const pending = { promise: task, signal } satisfies PendingFetch<T>;
    if (byRevision) byRevision.set(revision, pending);
    else this.pending.set(layerId, new Map([[revision, pending]]));
    try {
      return await task;
    } finally {
      const pendingForLayer = this.pending.get(layerId);
      if (pendingForLayer?.get(revision) === pending) {
        pendingForLayer.delete(revision);
        if (pendingForLayer.size === 0) this.pending.delete(layerId);
      }
    }
  }

  private async fetchAndStore(
    layerId: string,
    revision: number,
    generation: number,
    signal?: AbortSignal,
  ): Promise<LayerBitmap<T>> {
    const { bitmap, bytes } = await this.fetch(layerId, signal, revision);
    const entry: LayerBitmap<T> = { bitmap, revision, bytes };
    const latest = this.latest.get(layerId);
    if (latest?.revision === revision && latest.generation === generation) {
      if (!this.cache.put(layerId, entry, bytes)) this.onDispose?.(bitmap);
    } else {
      // A superseded or invalidated request still resolves for its caller;
      // dispose the GPU object because it can no longer enter the cache.
      this.onDispose?.(bitmap);
    }
    return entry;
  }

  /** The layer was deleted or the document closed: release its texture. */
  invalidate(layerId: string): void {
    this.cache.delete(layerId);
    this.pending.delete(layerId);
    this.generations.set(layerId, (this.generations.get(layerId) ?? 0) + 1);
    this.latest.delete(layerId);
  }

  invalidateAll(): void {
    this.cache.clear();
    this.pending.clear();
    this.latest.clear();
    // Keep the token space monotonic: an old request may still resolve after
    // the pending map is cleared, and resetting tokens would let it collide
    // with the first request made after invalidation.
    for (const [layerId, generation] of this.generations) {
      this.generations.set(layerId, generation + 1);
    }
  }

  /** Memory pressure flows through to the texture cache. */
  onMemoryPressure(level: "moderate" | "critical"): void {
    if (level === "critical") {
      // Do not let a response already in flight repopulate a cache that the
      // host explicitly asked us to empty.
      this.invalidateAll();
      return;
    }
    this.cache.onMemoryPressure(level);
  }
}
