/** Serializes the asynchronous bridge against synchronous pointer events.
 * Each gesture retains its own token and failure; later gestures cannot reuse
 * a stale token, and a failed point cancels the stroke rather than committing
 * a partial edit. The backend owns all pixel data. */
export interface StrokeTransport {
  begin(): Promise<string>;
  point(token: string, x: number, y: number): Promise<unknown>;
  end(token: string, cancel: boolean): Promise<unknown>;
}

export interface StrokeProgress {
  token: string;
  sequence: number;
}

export class PointerStroke {
  private tail: Promise<unknown>;
  private token: string | null = null;
  private failure: unknown;
  private closed = false;
  private sequence = 0;
  private listeners = new Set<(progress: StrokeProgress) => void>();

  constructor(private transport: StrokeTransport) {
    this.tail = Promise.resolve().then(() => transport.begin()).then((token) => {
      this.token = token;
      this.publishProgress();
    }).catch((error: unknown) => { this.failure = error; });
  }

  /** Preview requests follow acknowledged pixels, never speculative pointer
   * positions. Observers do not delay the ordered editing RPCs. */
  onProgress(listener: (progress: StrokeProgress) => void): () => void {
    if (this.closed) return () => {};
    this.listeners.add(listener);
    if (this.token !== null) this.notify(listener);
    return () => { this.listeners.delete(listener); };
  }

  private publishProgress(): void {
    this.sequence++;
    if (this.closed || this.token === null) return;
    for (const listener of this.listeners) {
      this.notify(listener);
    }
  }

  private notify(listener: (progress: StrokeProgress) => void): void {
    if (this.token === null) return;
    try { listener({ token: this.token, sequence: this.sequence }); }
    catch { /* Display failures must not cancel acknowledged pixel edits. */ }
  }

  point(x: number, y: number): void {
    if (this.closed) return;
    this.tail = this.tail.then(async () => {
      if (this.failure !== undefined || this.token === null) return;
      try {
        await this.transport.point(this.token, x, y);
        this.publishProgress();
      } catch (error: unknown) { this.failure = error; }
    });
  }

  async finish(cancel = false): Promise<unknown> {
    if (this.closed) return;
    this.closed = true;
    this.listeners.clear();
    await this.tail;
    let result: unknown;
    if (this.token !== null) {
      result = await this.transport.end(this.token, cancel || this.failure !== undefined);
    }
    if (this.failure !== undefined) throw this.failure;
    return result;
  }
}
