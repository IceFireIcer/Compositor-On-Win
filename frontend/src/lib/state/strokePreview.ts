import type { StrokeProgress } from "./pointerStroke";

interface PreviewFrame extends StrokeProgress { generation: number }

/** Coalesces the backend's acknowledged stroke pixels into one HTTP fetch at
 * a time. A tab change or cancelled gesture invalidates both queued and
 * in-flight frames; every allocated blob URL has exactly one owner. */
export class StrokePreview {
  private generation = 0;
  private pending: PreviewFrame | null = null;
  private running = false;
  private controller: AbortController | null = null;
  private url: string | null = null;

  constructor(
    private display: (url: string | null) => void,
    private fetcher: typeof fetch = (...args) => globalThis.fetch(...args),
    private createURL: (blob: Blob) => string = URL.createObjectURL.bind(URL),
    private revokeURL: (url: string) => void = URL.revokeObjectURL.bind(URL),
  ) {}

  update(progress: StrokeProgress): void {
    this.pending = { ...progress, generation: this.generation };
    if (!this.running) void this.drain();
  }

  clear(): void {
    this.generation++;
    this.pending = null;
    this.controller?.abort();
    this.controller = null;
    this.display(null);
    if (this.url !== null) this.revokeURL(this.url);
    this.url = null;
  }

  private async drain(): Promise<void> {
    this.running = true;
    try {
      while (this.pending) {
        const frame = this.pending;
        this.pending = null;
        const controller = new AbortController();
        this.controller = controller;
        try {
          const response = await this.fetcher(
            `/pixel/stroke/${encodeURIComponent(frame.token)}.png?sequence=${frame.sequence}`,
            { cache: "no-store", signal: controller.signal },
          );
          // End retires the token. Keep the last valid frame until the
          // document revision replaces it instead of flashing an empty image.
          if (!response.ok) continue;
          const blob = await response.blob();
          if (frame.generation !== this.generation || controller.signal.aborted) continue;
          const url = this.createURL(blob);
          const previous = this.url;
          this.url = url;
          this.display(url);
          if (previous !== null) this.revokeURL(previous);
        } catch {
          // Preview transport is optional; committing the stroke still uses
          // the ordered bridge and reports its own errors to the user.
        } finally {
          if (this.controller === controller) this.controller = null;
        }
      }
    } finally {
      this.running = false;
    }
  }
}
