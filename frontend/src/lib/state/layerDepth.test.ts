import { describe, expect, it } from "vitest";
import { depthMenuItemsFor } from "./layerDepth";

// 票 69：右键菜单的项按**被点的那一层**算，不能读全局 activeLabel。
// 上游 NativeLayerList 第 7b 项：有深层像素就是「转换为 8 位」，否则「转换为 16 位」；
// HDR 图层两条都给（先「转换为 16 位」，再按深层有无决定的那条）。
describe("depthMenuItemsFor", () => {
  it("offers 16-bit on an 8-bit layer", () => {
    expect(depthMenuItemsFor("", true)).toEqual([{ label: "转换为 16 位", command: "to16" }]);
  });

  it("offers 8-bit on a 16-bit layer", () => {
    expect(depthMenuItemsFor("16-Bit", true)).toEqual([{ label: "转换为 8 位", command: "to8" }]);
  });

  it("offers both on an HDR layer, 16-bit first", () => {
    expect(depthMenuItemsFor("HDR", true)).toEqual([
      { label: "转换为 16 位", command: "hdrTo16" },
      { label: "转换为 8 位", command: "to8" },
    ]);
  });

  it("offers nothing on a folder, adjustment or text layer", () => {
    expect(depthMenuItemsFor("", false)).toEqual([]);
    expect(depthMenuItemsFor("HDR", false)).toEqual([]);
  });
});
