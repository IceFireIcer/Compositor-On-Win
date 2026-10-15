import { describe, expect, it } from "vitest";
import { proofModeFor } from "./compositor";

// 票 72：切换或载入 ICC 之后前端会先把表清空再去取新表。这中间如果还让非 0 的
// 模式过去，着色器就会继续采样上一张 3D 纹理 —— 菜单勾了新配置、画布还按旧校样。
describe("proofModeFor", () => {
  const table = new Uint8Array(16);

  it("有表就按请求的模式", () => {
    expect(proofModeFor(1, table, 33)).toBe(1);
    expect(proofModeFor(2, table, 33)).toBe(2);
  });

  it("没有表就直通，不管请求的是什么", () => {
    expect(proofModeFor(1, null, 33)).toBe(0);
    expect(proofModeFor(2, null, 0)).toBe(0);
  });

  it("维度不足也直通（1×1 占位纹理没有意义）", () => {
    expect(proofModeFor(1, table, 0)).toBe(0);
    expect(proofModeFor(1, table, 1)).toBe(0);
  });
});
