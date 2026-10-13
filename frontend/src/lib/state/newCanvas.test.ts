import { describe, expect, it } from "vitest";
import {
  NEW_CANVAS_BACKGROUNDS,
  NEW_CANVAS_MAX_SIDE,
  NEW_CANVAS_UNITS,
  NewCanvasBackground,
  NewCanvasUnit,
  backgroundById,
  nextResolution,
  pixelPreview,
  unitById,
} from "./newCanvas";

// 票 57：逐条照抄 reference/Swift/CompositorTests/NewCanvasUnitTests.swift 的
// 数字与循环顺序；其后是本地化小数与限额的补充用例。
describe("新建画布单位（照抄 NewCanvasUnitTests）", () => {
  it("打印尺寸按 DPI 变成像素", () => {
    expect(NewCanvasUnit.inches.pixels("8.5", 300)).toBe(2550);
    expect(NewCanvasUnit.inches.pixels("11", 300)).toBe(3300);
    expect(NewCanvasUnit.millimeters.pixels("210", 300)).toBe(2480); // A4 的宽
    expect(NewCanvasUnit.centimeters.pixels("2,54", 72)).toBe(72); // 逗号当小数点
    expect(NewCanvasUnit.pixels.pixels("1920", 300)).toBe(1920); // 像素忽略 DPI
    expect(NewCanvasUnit.pixels.pixels("19.5", 72)).toBeNull(); // 像素是整数
    expect(NewCanvasUnit.inches.pixels("200", 300)).toBeNull(); // 60000 像素超过边限
    expect(NewCanvasUnit.inches.pixels("0", 300)).toBeNull();
  });

  it("切换单位写的是同一张画布，pill 按 px → in → cm → mm → px 循环", () => {
    expect(NewCanvasUnit.inches.text(1920, 72)).toBe("26.67");
    expect(NewCanvasUnit.inches.text(2550, 300)).toBe("8.5");
    expect(NewCanvasUnit.millimeters.text(2480, 300)).toBe("209.97");
    for (const unit of NEW_CANVAS_UNITS) {
      if (unit.id === "px") continue;
      expect(unit.pixels(unit.text(2550, 300), 300)).toBe(2550);
    }
    expect(NEW_CANVAS_UNITS.map((unit) => unit.next)).toEqual(["in", "cm", "mm", "px"]);
  });

  it("底色按 transparent → white → black → transparent 循环", () => {
    expect(NEW_CANVAS_BACKGROUNDS.map((background) => background.next)).toEqual([
      "white",
      "black",
      "transparent",
    ]);
  });
});

describe("本地化小数与限额", () => {
  it("逗号当小数点，首尾空白被忽略", () => {
    expect(NewCanvasUnit.centimeters.pixels(" 2,54 ", 72)).toBe(72);
    expect(NewCanvasUnit.inches.pixels(" 8,5 ", 300)).toBe(2550);
  });

  it("非法输入返回 null", () => {
    for (const text of ["", "  ", "abc", "0", "-1", "0,0", "1,2,3"]) {
      expect(NewCanvasUnit.pixels.pixels(text, 72)).toBeNull();
    }
    expect(NewCanvasUnit.pixels.pixels("19.5", 72)).toBeNull();
    expect(NewCanvasUnit.centimeters.pixels("1e9", 300)).toBeNull();
    expect(NewCanvasUnit.millimeters.pixels("999999", 300)).toBeNull();
  });

  it("边限是 DocumentLimits.maxSide：1…30000 像素", () => {
    expect(NEW_CANVAS_MAX_SIDE).toBe(30000);
    expect(NewCanvasUnit.pixels.pixels("1", 72)).toBe(1);
    expect(NewCanvasUnit.pixels.pixels("30000", 72)).toBe(NEW_CANVAS_MAX_SIDE);
    expect(NewCanvasUnit.pixels.pixels("30001", 72)).toBeNull();
    expect(NewCanvasUnit.inches.pixels("100", 300)).toBe(NEW_CANVAS_MAX_SIDE); // 100in @300dpi
    expect(NewCanvasUnit.inches.pixels("100.1", 300)).toBeNull();
  });

  it("像素文本是整数，打印文本最多两位小数", () => {
    expect(NewCanvasUnit.pixels.text(1920, 72)).toBe("1920");
    expect(NewCanvasUnit.pixels.text(1920, 300)).toBe("1920");
    expect(NewCanvasUnit.centimeters.text(2550, 300)).toBe("21.59");
    expect(NewCanvasUnit.millimeters.text(2550, 300)).toBe("215.9");
    expect(NewCanvasUnit.inches.text(1, 9600)).toBe("0");
  });

  it("DPI pill 在 72 与 300 之间来回", () => {
    expect(nextResolution(72)).toBe(300);
    expect(nextResolution(300)).toBe(72);
    expect(nextResolution(150)).toBe(300);
  });

  it("打印单位给出像素预览", () => {
    expect(pixelPreview(1920, 1080, 72)).toBe("72 DPI: 1920 × 1080 像素");
  });

  it("按 id 取单位与底色，默认分别是像素与透明", () => {
    expect(unitById("mm")).toBe(NewCanvasUnit.millimeters);
    expect(unitById("px").name).toBe("像素");
    expect(backgroundById("black")).toBe(NewCanvasBackground.black);
    expect(backgroundById("transparent").title).toBe("透明画布");
  });
});
