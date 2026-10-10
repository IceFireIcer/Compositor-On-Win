import { beforeEach, describe, expect, it } from "vitest";
import { get } from "svelte/store";

import {
  applyMemoryPressure,
  memoryPressure,
  registerPressureHandler,
  watchMemoryPressure,
} from "./memory";

describe("内存压力的前端接线（票 52 / V10）", () => {
  beforeEach(() => {
    memoryPressure.set("");
  });

  it("档位归一化：只认 moderate/critical，其余当作没压力", () => {
    expect(applyMemoryPressure("critical")).toBe("critical");
    expect(get(memoryPressure)).toBe("critical");
    expect(applyMemoryPressure("moderate")).toBe("moderate");
    // Go 侧恢复时会推空串；未知负载也不该让缓存乱逐出。
    expect(applyMemoryPressure("")).toBe("");
    expect(applyMemoryPressure(undefined)).toBe("");
    expect(applyMemoryPressure("catastrophic")).toBe("");
  });

  it("注册的处理器按顺序收到档位，注销后不再收到", () => {
    const seen: string[] = [];
    const a = registerPressureHandler((level) => seen.push(`a:${level}`));
    const b = registerPressureHandler((level) => seen.push(`b:${level}`));

    applyMemoryPressure("moderate");
    expect(seen).toEqual(["a:moderate", "b:moderate"]);

    a();
    applyMemoryPressure("critical");
    expect(seen).toEqual(["a:moderate", "b:moderate", "b:critical"]);
    b();
    applyMemoryPressure("");
    expect(seen).toEqual(["a:moderate", "b:moderate", "b:critical"]);
  });

  it("重复注册同一个处理器只生效一次（Set 语义）", () => {
    let calls = 0;
    const handler = () => {
      calls += 1;
    };
    const off1 = registerPressureHandler(handler);
    const off2 = registerPressureHandler(handler);
    applyMemoryPressure("critical");
    expect(calls).toBe(1);
    off1();
    off2();
  });

  it("没有 Wails 运行时时订阅是空操作", () => {
    const stop = watchMemoryPressure();
    expect(typeof stop).toBe("function");
    stop();
  });

  it("有运行时时把事件转成档位并派发给处理器", () => {
    const handlers = new Map<string, (...args: unknown[]) => void>();
    (globalThis as unknown as { runtime: unknown }).runtime = {
      EventsOn: (name: string, fn: (...args: unknown[]) => void) => {
        handlers.set(name, fn);
        return () => handlers.delete(name);
      },
    };
    const seen: string[] = [];
    const off = registerPressureHandler((level) => seen.push(level));
    try {
      const stop = watchMemoryPressure();
      const emit = handlers.get("memory:pressure");
      expect(emit).toBeTypeOf("function");
      emit!("critical");
      expect(seen).toEqual(["critical"]);
      expect(get(memoryPressure)).toBe("critical");
      stop();
    } finally {
      off();
      delete (globalThis as unknown as { runtime?: unknown }).runtime;
    }
  });
});
