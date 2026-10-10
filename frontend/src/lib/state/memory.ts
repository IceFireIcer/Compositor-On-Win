import { writable } from "svelte/store";

/**
 * 内存压力（票 52 / V10）：Go 侧定期采样机器内存，档位变化时推 `memory:pressure`。
 * 这里把它变成一个 store + 一个注册口：**能逐出纹理的缓存自己注册**。
 *
 * 今天画布显示的是 Go 合成好的 PNG（一个 <img>，按 rev 换 URL），没有 GPU 纹理缓存可
 * 逐出；GPU 路径（票 11）接上画布时，只需要 `registerPressureHandler(cache.onMemoryPressure)`。
 * 所以这个注册口是那条路要用的接缝，不是摆设 —— 它有测试覆盖。
 */

/** 与 Go 侧 `memory.Level` 同名："" = 没压力。 */
export type MemoryPressureLevel = "" | "moderate" | "critical";

export const memoryPressure = writable<MemoryPressureLevel>("");

const handlers = new Set<(level: MemoryPressureLevel) => void>();

/** 注册一个压力处理器（返回注销函数）。同一个函数重复注册只生效一次。 */
export function registerPressureHandler(
  handler: (level: MemoryPressureLevel) => void,
): () => void {
  handlers.add(handler);
  return () => {
    handlers.delete(handler);
  };
}

/** 处理一条压力通知（测试直接调它，不必造 Wails 事件）。返回归一化后的档位。 */
export function applyMemoryPressure(raw: unknown): MemoryPressureLevel {
  const level: MemoryPressureLevel = raw === "moderate" || raw === "critical" ? raw : "";
  memoryPressure.set(level);
  for (const handler of handlers) handler(level);
  return level;
}

/**
 * 订阅 "memory:pressure"。没有 Wails 运行时（vite dev / vitest）时是空订阅 ——
 * 与 filters.ts / update.ts / external.ts 同款写法（globalThis 而不是 window）。
 */
export function watchMemoryPressure(): () => void {
  const runtime = (
    globalThis as unknown as {
      runtime?: { EventsOn: (name: string, fn: (...args: unknown[]) => void) => () => void };
    }
  ).runtime;
  if (!runtime) return () => undefined;
  return runtime.EventsOn("memory:pressure", (payload: unknown) => {
    applyMemoryPressure(payload);
  });
}
