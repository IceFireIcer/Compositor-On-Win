import { describe, expect, it } from "vitest";
import type { TextStyle } from "./text";
import {
  dropRuns,
  effectiveColorAt,
  effectiveFontAt,
  reconcileRunsForEdit,
  setRunColor,
  setRunFont,
  uniformColorIn,
  uniformFontIn,
  utf16Length,
} from "./textRuns";

/**
 * 逐 run 语义（票 45）对照原版 TypeTool.swift 的 LayerTextStyle：
 * location/length 是 content 的 UTF-16 码元下标，sorted、不重叠、length > 0；
 * 数组非空，nil/undefined = 全段统一。run 只标记与基础色/字体不同的字母。
 *
 * TS 的 TextStyle 不带 content（内容在 TextSession 上），所以需要文本长度的
 * 断言都显式传 content.length；省略长度时的保守行为另有一组断言。
 */

const RED = { red: 1, green: 0, blue: 0 };
const BLUE = { red: 0, green: 0, blue: 1 };
const BLACK = { red: 0, green: 0, blue: 0 };

function style(patch: Partial<TextStyle> = {}): TextStyle {
  return {
    fontName: "Segoe UI",
    fontSize: 72,
    red: 0,
    green: 0,
    blue: 0,
    alignment: "Left",
    tracking: 0,
    leading: 0,
    ...patch,
  };
}

/** 深度冻结：纯函数（不修改入参）的运行时约束，严格模式下原地赋值会抛错。 */
function frozen<T>(value: T): T {
  if (value && typeof value === "object") {
    for (const key of Object.keys(value as object)) {
      frozen((value as Record<string, unknown>)[key]);
    }
    Object.freeze(value);
  }
  return value;
}

const run = (location: number, length: number, color = RED) => ({ location, length, ...color });
const fontRun = (location: number, length: number, fontName = "Menlo") => ({ location, length, fontName });

describe("utf16Length", () => {
  it("counts UTF-16 code units, not code points (emoji are two)", () => {
    expect(utf16Length("")).toBe(0);
    expect(utf16Length("Hello")).toBe(5);
    expect(utf16Length("😀")).toBe(2);
    expect(utf16Length("a😀b")).toBe(4);
    expect([..."a😀b"].length).toBe(3); // 码点数与码元数确实不同
  });
});

describe("effectiveColorAt / effectiveFontAt", () => {
  it("falls back to the base colour and face outside every run", () => {
    const s = style({ colorRuns: [run(1, 2, RED)], fontRuns: [fontRun(1, 2, "Menlo")] });
    expect(effectiveColorAt(s, 0)).toEqual(BLACK);
    expect(effectiveFontAt(s, 0)).toBe("Segoe UI");
    // 边界：location 含、location+length 不含。
    expect(effectiveColorAt(s, 1)).toEqual(RED);
    expect(effectiveColorAt(s, 2)).toEqual(RED);
    expect(effectiveFontAt(s, 2)).toBe("Menlo");
    expect(effectiveColorAt(s, 3)).toEqual(BLACK);
    expect(effectiveFontAt(s, 3)).toBe("Segoe UI");
  });

  it("addresses surrogate halves by UTF-16 unit (an emoji run is two units)", () => {
    const s = style({ colorRuns: [run(1, 2, BLUE)] });
    expect(effectiveColorAt(s, 1)).toEqual(BLUE); // 高位代理
    expect(effectiveColorAt(s, 2)).toEqual(BLUE); // 低位代理
    expect(effectiveColorAt(s, 3)).toEqual(BLACK); // 'b'
  });

  it("returns the base when there are no runs at all", () => {
    const s = style();
    expect(effectiveColorAt(s, 7)).toEqual(BLACK);
    expect(effectiveFontAt(s, 7)).toBe("Segoe UI");
  });
});

describe("setRunColor", () => {
  it("paints a partial range as one coalesced run, base untouched", () => {
    const before = frozen(style());
    const after = setRunColor(before, { start: 1, end: 3 }, RED, "Hello".length);
    expect(after.colorRuns).toEqual([run(1, 2, RED)]);
    expect(after.red).toBe(0);
    expect(after.green).toBe(0);
    expect(after.blue).toBe(0);
    expect(before).toEqual(style()); // 入参未被修改
  });

  it("merges adjacent equal-colour units into one run", () => {
    // 已有 run {2,1} 与 {3,1}（同色相邻），再涂 0–3：四格同色应合成一个 run。
    const s = style({ colorRuns: [run(2, 1, RED), run(3, 1, RED)] });
    const after = setRunColor(s, { start: 0, end: 3 }, RED, "Hello".length);
    expect(after.colorRuns).toEqual([run(0, 4, RED)]);
  });

  it("keeps the runs outside the painted range", () => {
    const s = style({ colorRuns: [run(1, 1, RED)] });
    const after = setRunColor(s, { start: 3, end: 5 }, BLUE, "Hello".length);
    expect(after.colorRuns).toEqual([run(1, 1, RED), run(3, 2, BLUE)]);
  });

  it("an empty range (or null) sets the base colour and clears runs", () => {
    const s = style({ colorRuns: [run(1, 1, RED), run(4, 1, RED)] });
    const after = setRunColor(s, { start: 2, end: 2 }, BLUE, "Hello".length);
    expect(after).toEqual(style({ colorRuns: undefined, ...BLUE }));
    const fromNull = setRunColor(s, null, BLUE, "Hello".length);
    expect(fromNull).toEqual(style({ colorRuns: undefined, ...BLUE }));
  });

  it("a range covering the whole text sets the base colour and clears runs", () => {
    const s = style({ colorRuns: [run(0, 2, RED), run(3, 2, BLUE)] });
    const after = setRunColor(s, { start: 0, end: 5 }, BLUE, "Hello".length);
    expect(after).toEqual(style({ colorRuns: undefined, ...BLUE }));
  });

  it("collapses to nil when the painted colour equals the base everywhere", () => {
    const s = style({ colorRuns: [run(1, 1, RED)] });
    const after = setRunColor(s, { start: 1, end: 2 }, BLACK, "Hello".length);
    expect(after.colorRuns).toBeUndefined();
  });

  it("clamps the range to the content", () => {
    const s = style();
    // 负数起点与超长终点都被夹到 [0, len]。
    const after = setRunColor(s, { start: -5, end: 99 }, RED, "Hello".length);
    // end 到 5 = 全段 → 规范化为基础色。
    expect(after).toEqual(style({ colorRuns: undefined, ...RED }));
    const partial = setRunColor(s, { start: 1, end: 99 }, RED, "Hello".length);
    expect(partial.colorRuns).toEqual([run(1, 4, RED)]);
  });

  it("without a length, encodes conservatively (never repaints beyond the range)", () => {
    // 省略 content.length 时无法判断「全段」：不规范化，改为把范围标成 run。
    // 渲染结果与规范化后一致（Go 渲染端按 run 绘制）。
    const s = style({ colorRuns: [run(0, 2, RED)] });
    const after = setRunColor(s, { start: 0, end: 3 }, BLUE);
    expect(after.colorRuns).toEqual([run(0, 3, BLUE)]);
    expect(after.red).toBe(0); // 基础色不动，范围外不会被染色
  });

  it("does not mutate its input", () => {
    const s = frozen(style({ colorRuns: [run(1, 2, RED)] }));
    expect(() => setRunColor(s, { start: 3, end: 4 }, BLUE, 5)).not.toThrow();
  });
});

describe("setRunFont", () => {
  it("paints a partial range as one coalesced run, base untouched", () => {
    const after = setRunFont(style(), { start: 1, end: 3 }, "Menlo", "Hello".length);
    expect(after.fontRuns).toEqual([fontRun(1, 2, "Menlo")]);
    expect(after.fontName).toBe("Segoe UI");
  });

  it("merges adjacent equal faces into one run", () => {
    const s = style({ fontRuns: [fontRun(2, 1, "Menlo"), fontRun(3, 1, "Menlo")] });
    const after = setRunFont(s, { start: 0, end: 3 }, "Menlo", "Hello".length);
    expect(after.fontRuns).toEqual([fontRun(0, 4, "Menlo")]);
  });

  it("promotes to the base face when every unit ends up the same", () => {
    // 分两步把 4 个字母都设成 Menlo：第二步后全体同字体 → 提升为基础字体并清空 runs。
    let s = setRunFont(style(), { start: 0, end: 2 }, "Menlo", "abcd".length);
    expect(s.fontRuns).toEqual([fontRun(0, 2, "Menlo")]);
    s = setRunFont(s, { start: 2, end: 4 }, "Menlo", "abcd".length);
    expect(s.fontName).toBe("Menlo");
    expect(s.fontRuns).toBeUndefined();
  });

  it("an empty range or a whole-text range changes the base face and clears runs", () => {
    const s = style({ fontRuns: [fontRun(0, 2, "Menlo")] });
    expect(setRunFont(s, { start: 2, end: 2 }, "Arial", "Hello".length)).toEqual(
      style({ fontName: "Arial", fontRuns: undefined }),
    );
    expect(setRunFont(s, { start: 0, end: 5 }, "Arial", "Hello".length)).toEqual(
      style({ fontName: "Arial", fontRuns: undefined }),
    );
  });

  it("collapses to nil when every unit is back on the base face", () => {
    const s = style({ fontRuns: [fontRun(1, 1, "Menlo")] });
    const after = setRunFont(s, { start: 1, end: 2 }, "Segoe UI", "Hello".length);
    expect(after.fontName).toBe("Segoe UI");
    expect(after.fontRuns).toBeUndefined();
  });

  it("ignores invalid font names silently (no-op, no throw)", () => {
    const s = style({ fontRuns: [fontRun(1, 1, "Menlo")] });
    for (const bad of ["", "x".repeat(201), "Bad\nName", "Bad\rName", "Bad\u2028Name"]) {
      const after = setRunFont(s, { start: 0, end: 5 }, bad, "Hello".length);
      expect(after).toEqual(s);
    }
    expect(() => setRunFont(style(), { start: 0, end: 1 }, "Bad\nName", 5)).not.toThrow();
  });

  it("accepts a 200-character name and rejects 201 (UTF-16 boundary)", () => {
    const ok = "x".repeat(200);
    const after = setRunFont(style(), { start: 0, end: 5 }, ok, 5);
    expect(after.fontName).toBe(ok);
    expect(setRunFont(style(), { start: 0, end: 5 }, "x".repeat(201), 5)).toEqual(style());
  });

  it("does not mutate its input", () => {
    const s = frozen(style({ fontRuns: [fontRun(1, 2, "Menlo")] }));
    expect(() => setRunFont(s, { start: 3, end: 4 }, "Arial", 5)).not.toThrow();
  });
});

describe("uniformColorIn / uniformFontIn", () => {
  it("returns the base value when the range is uniform", () => {
    const s = style();
    expect(uniformColorIn(s, { start: 0, end: 5 }, 5)).toEqual(BLACK);
    expect(uniformFontIn(s, { start: 0, end: 5 }, 5)).toBe("Segoe UI");
  });

  it("returns the run value when the range sits inside one run", () => {
    const s = style({ colorRuns: [run(0, 5, RED)], fontRuns: [fontRun(0, 5, "Menlo")] });
    expect(uniformColorIn(s, { start: 1, end: 3 }, 5)).toEqual(RED);
    expect(uniformFontIn(s, { start: 1, end: 3 }, 5)).toBe("Menlo");
  });

  it("returns null for an empty range", () => {
    expect(uniformColorIn(style(), { start: 2, end: 2 }, 5)).toBeNull();
    expect(uniformFontIn(style(), { start: 2, end: 2 }, 5)).toBeNull();
    expect(uniformColorIn(style(), { start: 3, end: 1 }, 5)).toBeNull();
  });

  it("returns null when the range mixes a run and the base", () => {
    const s = style({ colorRuns: [run(0, 2, RED)], fontRuns: [fontRun(0, 2, "Menlo")] });
    expect(uniformColorIn(s, { start: 0, end: 3 }, 5)).toBeNull();
    expect(uniformFontIn(s, { start: 0, end: 3 }, 5)).toBeNull();
  });

  it("returns null on a gap inside the range whose base differs from the run", () => {
    // run 覆盖 0–1，2 号字母用基础色：范围 0–3 混合 → null（原版同样检查空隙）。
    const s = style({ colorRuns: [run(0, 2, RED)], fontRuns: [fontRun(0, 2, "Menlo")] });
    expect(uniformColorIn(s, { start: 0, end: 3 }, 3)).toBeNull();
    expect(uniformFontIn(s, { start: 0, end: 3 }, 3)).toBeNull();
  });

  it("is uniform when the gap uses the same value as the run", () => {
    // 基础色与 run 同色：空隙不算「混合」。
    const s = style({ red: 1, colorRuns: [run(0, 2, RED)], fontRuns: [fontRun(0, 2, "Segoe UI")] });
    expect(uniformColorIn(s, { start: 0, end: 3 }, 3)).toEqual(RED);
    expect(uniformFontIn(s, { start: 0, end: 3 }, 3)).toBe("Segoe UI");
  });

  it("clamps the range to the content when the length is known", () => {
    const s = style({ colorRuns: [run(0, 2, RED)], fontRuns: [fontRun(0, 2, "Menlo")] });
    expect(uniformColorIn(s, { start: 1, end: 99 }, 2)).toEqual(RED);
    expect(uniformFontIn(s, { start: 1, end: 99 }, 2)).toBe("Menlo");
  });

  it("covers a range spanning two runs of different values as mixed", () => {
    const s = style({ colorRuns: [run(0, 2, RED), run(2, 3, BLUE)] });
    expect(uniformColorIn(s, { start: 1, end: 3 }, 5)).toBeNull();
    expect(uniformColorIn(s, { start: 2, end: 4 }, 5)).toEqual(BLUE);
  });
});

describe("reconcileRunsForEdit", () => {
  it("typing at the end extends the previous letter's colour", () => {
    const s = style({ colorRuns: [run(4, 1, RED)] });
    const after = reconcileRunsForEdit(s, 5, 5, 1, "Hello".length);
    expect(after.colorRuns).toEqual([run(4, 2, RED)]);
  });

  it("typing in the middle inherits the letter before the caret", () => {
    // 在 2 号字母（红）之后插入：新字母红，且与旧 run 合并。
    const s = style({ colorRuns: [run(2, 1, RED)] });
    const after = reconcileRunsForEdit(s, 3, 3, 1, "Hello".length);
    expect(after.colorRuns).toEqual([run(2, 2, RED)]);
  });

  it("inserting after a base-coloured letter inherits the base", () => {
    // run 在末尾（红），在下标 1 插入：新字母取基础色，红色 run 右移一格。
    const s = style({ colorRuns: [run(4, 1, RED)] });
    const after = reconcileRunsForEdit(s, 1, 1, 1, "Hello".length);
    expect(after.colorRuns).toEqual([run(5, 1, RED)]);
  });

  it("at position 0 with a replaced range, the first replaced letter's value wins", () => {
    const s = style({ colorRuns: [run(0, 2, RED)] });
    const after = reconcileRunsForEdit(s, 0, 2, 1, "Hello".length);
    expect(after.colorRuns).toEqual([run(0, 1, RED)]);
  });

  it("a pure insertion at position 0 takes the text's first letter's value", () => {
    const s = style({ colorRuns: [run(0, 1, RED)] });
    const after = reconcileRunsForEdit(s, 0, 0, 1, "Hello".length);
    expect(after.colorRuns).toEqual([run(0, 2, RED)]);
  });

  it("falls back to the base colour when the text is empty", () => {
    // 空文本 + 越界的 runs（损坏数据）：继承基础色，编码后没有差异 → nil。
    const s = style({ colorRuns: [run(0, 1, RED)] });
    const after = reconcileRunsForEdit(s, 0, 0, 1, 0);
    expect(after.colorRuns).toBeUndefined();
  });

  it("removes the deleted units' values and keeps the rest", () => {
    const s = style({ colorRuns: [run(1, 2, RED)] });
    const after = reconcileRunsForEdit(s, 1, 3, 0, "Hello".length);
    expect(after.colorRuns).toBeUndefined(); // 红色字母全被删掉，其余是基础色
    const kept = reconcileRunsForEdit(style({ colorRuns: [run(0, 1, RED), run(3, 2, BLUE)] }), 1, 2, 0, 5);
    expect(kept.colorRuns).toEqual([run(0, 1, RED), run(2, 2, BLUE)]); // 后续 run 左移一格
  });

  it("uses UTF-16 offsets so an emoji stays whole", () => {
    // "a😀b"：run 覆盖 emoji 的两个码元；在 b 前（下标 3）插入继承低代理的值。
    const s = style({ colorRuns: [run(1, 2, BLUE)] });
    const after = reconcileRunsForEdit(s, 3, 3, 1, 4);
    expect(after.colorRuns).toEqual([run(1, 3, BLUE)]);
    // 在 emoji 之后删除 b：run 不动。
    const deleted = reconcileRunsForEdit(s, 3, 4, 0, 4);
    expect(deleted.colorRuns).toEqual([run(1, 2, BLUE)]);
  });

  it("moves font runs the same way and may promote the base face", () => {
    const s = style({ fontRuns: [fontRun(4, 1, "Menlo")] });
    const after = reconcileRunsForEdit(s, 5, 5, 1, "Hello".length);
    expect(after.fontRuns).toEqual([fontRun(4, 2, "Menlo")]);
    // "ab"：a 是 Menlo，b 是基础字体；用同样字体重打 b → 全体同字体 → 提升为基础字体。
    const promoted = reconcileRunsForEdit(style({ fontRuns: [fontRun(0, 1, "Menlo")] }), 1, 2, 1, 2);
    expect(promoted).toEqual(style({ fontName: "Menlo", fontRuns: undefined }));
  });

  it("leaves a uniform style untouched (no runs to follow)", () => {
    const s = style();
    const after = reconcileRunsForEdit(s, 1, 2, 3, 5);
    expect(after.colorRuns).toBeUndefined();
    expect(after.fontRuns).toBeUndefined();
    expect(after).toEqual(s);
  });

  it("only reconciles the array that exists", () => {
    const s = style({ colorRuns: [run(4, 1, RED)] });
    const after = reconcileRunsForEdit(s, 5, 5, 1, 5);
    expect(after.fontRuns).toBeUndefined();
    expect(after.colorRuns).toEqual([run(4, 2, RED)]);
  });

  it("keeps the result valid: runs stay sorted and inside the new length", () => {
    const s = style({ colorRuns: [run(4, 1, RED)] });
    const after = reconcileRunsForEdit(s, 1, 1, 1, 5);
    expect(after.colorRuns).toEqual([run(5, 1, RED)]);
    for (const r of after.colorRuns ?? []) {
      expect(r.location).toBeGreaterThanOrEqual(0);
      expect(r.length).toBeGreaterThan(0);
      expect(r.location + r.length).toBeLessThanOrEqual(6); // 5 − 0 + 1
    }
  });

  it("does not mutate its input", () => {
    const s = frozen(style({ colorRuns: [run(4, 1, RED)], fontRuns: [fontRun(4, 1, "Menlo")] }));
    expect(() => reconcileRunsForEdit(s, 5, 5, 1, 5)).not.toThrow();
  });
});

describe("dropRuns", () => {
  it("clears both run arrays and keeps the base style", () => {
    const s = style({ colorRuns: [run(1, 2, RED)], fontRuns: [fontRun(1, 2, "Menlo")] });
    const after = dropRuns(s);
    expect(after.colorRuns).toBeUndefined();
    expect(after.fontRuns).toBeUndefined();
    expect(after).toEqual(style());
  });

  it("does not mutate its input", () => {
    const s = frozen(style({ colorRuns: [run(1, 2, RED)] }));
    expect(() => dropRuns(s)).not.toThrow();
  });
});
