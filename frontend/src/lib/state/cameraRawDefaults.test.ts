import { describe, expect, it } from "vitest";
import shared from "../../../../internal/bridge/testdata/camera_raw_defaults.json";
import { CAMERA_RAW_DEFAULTS } from "./cameraRawDefaults";

// 票 80 收尾：面板的默认值与 Go 的默认值在两处各存一份，用共享夹具把它们钉在一起。
// 夹具由 Go 生成（internal/bridge/cameraraw_defaults_test.go），前端读同一份文件。

/** 沿模块的键走到夹具里的同名值。 */
function compare(module: unknown, fixture: unknown, path: string): void {
  if (module === null || typeof module !== "object") {
    expect(module, `${path} 与 Go 的默认值不一致`).toBe(fixture);
    return;
  }
  const fixtureObject = fixture as Record<string, unknown>;
  for (const [key, value] of Object.entries(module as Record<string, unknown>)) {
    expect(fixtureObject, `Go 的默认值里没有 ${path}${key}`).toHaveProperty(key);
    compare(value, fixtureObject[key], `${path}${key}.`);
  }
}

describe("Camera Raw 默认值", () => {
  it("面板列出的每一项都等于 Go 的共享夹具", () => {
    compare(CAMERA_RAW_DEFAULTS, shared, "");
  });

  it("列出的确实是非零默认（不是把零值也抄了一份）", () => {
    const nonZero: number[] = [];
    const walk = (node: unknown): void => {
      if (typeof node === "number") {
        nonZero.push(node);
        return;
      }
      if (node && typeof node === "object") {
        for (const value of Object.values(node)) walk(value);
      }
    };
    walk(CAMERA_RAW_DEFAULTS);
    expect(nonZero.length).toBeGreaterThan(15);
    for (const value of nonZero) expect(value).not.toBe(0);
  });
});
