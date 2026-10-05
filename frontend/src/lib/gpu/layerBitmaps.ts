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

export type BitmapFetcher<T> = (layerId: string, signal?: AbortSignal) => Promise<{
  bitmap: T;
  bytes: number;
}>;

export class LayerBitmapStore<T> {
  private cache: TextureCache<LayerBitmap<T>>;
  /** In-flight fetches, keyed by layerId, so concurrent gets coalesce. */
  private pending = new Map<string, Promise<LayerBitmap<T>>>();

  constructor(
    private fetch: BitmapFetcher<T>,
    budgetBytes: number,
  ) {
    this.cache = new TextureCache<LayerBitmap<T>>(budgetBytes);
  }

  peek(layerId: string): LayerBitmap<T> | undefined {
    return this.cache.get(layerId);
  }

  /**
   * Returns the layer's bitmap for the given revision: served from cache
   * when the revision matches; otherwise the stale entry is evicted and
   * only this layer is fetched again. Concurrent callers share one fetch.
   */
  async get(layerId: string, revision: number): Promise<LayerBitmap<T>> {
    const cached = this.cache.get(layerId);
    if (cached && cached.revision === revision) return cached;
    this.cache.delete(layerId); // stale bitmap: release immediately
    const inflight = this.pending.get(layerId);
    if (inflight) return inflight;
    const task = this.fetchAndStore(layerId, revision);
    this.pending.set(layerId, task);
    try {
      return await task;
    } finally {
      this.pending.delete(layerId);
    }
  }

  private async fetchAndStore(layerId: string, revision: number): Promise<LayerBitmap<T>> {
    const { bitmap, bytes } = await this.fetch(layerId);
    const entry: LayerBitmap<T> = { bitmap, revision, bytes };
    // Cache whatever arrived even if a newer revision is already requested:
    // the next get() for the newer revision will fetch (its cache lookup
    // misses by revision), and this bitmap still serves same-revision reads.
    this.cache.put(layerId, entry, bytes);
    return entry;
  }

  /** The layer was deleted or the document closed: release its texture. */
  invalidate(layerId: string): void {
    this.cache.delete(layerId);
  }

  invalidateAll(): void {
    this.cache.clear();
  }

  /** Memory pressure flows through to the texture cache. */
  onMemoryPressure(level: "moderate" | "critical"): void {
    this.cache.onMemoryPressure(level);
  }
}
