/**
 * LRU texture cache with a byte budget and memory-pressure response.
 *
 * The GPU compositor keeps layer/mask bitmaps as GPU textures; large
 * documents cannot hold every texture, so the cache evicts least-recently
 * used entries oldest-first once the byte budget is exceeded. Memory
 * pressure callbacks (performance.memory pressure events, or the host
 * signaling low memory) trim to half the budget or drop everything.
 *
 * Pure logic — the texture itself is an opaque value; `onDestroy` lets the
 * caller release GPU memory (device.destroy texture) on eviction.
 */
export type MemoryPressureLevel = "moderate" | "critical";

export class TextureCache<T> {
  private entries = new Map<string, { value: T; bytes: number }>();
  private totalBytes = 0;

  constructor(
    private limitBytes: number,
    private onDestroy?: (value: T) => void,
  ) {}

  get size(): number {
    return this.entries.size;
  }

  get bytes(): number {
    return this.totalBytes;
  }

  has(key: string): boolean {
    return this.entries.has(key);
  }

  /** Returns the cached value and marks it most recently used. */
  get(key: string): T | undefined {
    const entry = this.entries.get(key);
    if (!entry) return undefined;
    // Map iteration order is insertion order: re-insert to move to the tail
    // (most recently used).
    this.entries.delete(key);
    this.entries.set(key, entry);
    return entry.value;
  }

  /** Inserts or replaces a value; evicts LRU entries while over budget. */
  put(key: string, value: T, bytes: number): void {
    if (bytes > this.limitBytes) {
      // A value that alone exceeds the budget cannot coexist with anything:
      // cache nothing rather than thrashing every other entry out.
      return;
    }
    this.delete(key);
    this.entries.set(key, { value, bytes });
    this.totalBytes += bytes;
    this.evictWhileOverBudget();
  }

  delete(key: string): boolean {
    const entry = this.entries.get(key);
    if (!entry) return false;
    this.entries.delete(key);
    this.totalBytes -= entry.bytes;
    this.onDestroy?.(entry.value);
    return true;
  }

  clear(): void {
    for (const entry of this.entries.values()) this.onDestroy?.(entry.value);
    this.entries.clear();
    this.totalBytes = 0;
  }

  /** The host or window reported memory pressure; drop what we can spare. */
  onMemoryPressure(level: MemoryPressureLevel): void {
    if (level === "critical") {
      this.clear();
      return;
    }
    this.evictTo(this.limitBytes / 2);
  }

  private evictWhileOverBudget(): void {
    this.evictTo(this.limitBytes);
  }

  /** Evicts least-recently-used entries (Map head) until within budget. */
  private evictTo(budget: number): void {
    while (this.totalBytes > budget && this.entries.size > 0) {
      const oldest = this.entries.keys().next();
      if (oldest.done) break;
      this.delete(oldest.value);
    }
  }
}
