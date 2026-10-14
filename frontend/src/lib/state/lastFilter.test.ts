import { beforeEach, describe, expect, it } from "vitest";
import { get } from "svelte/store";
import { defaultFilterSettings } from "./filters";
import {
  clearLastFilter,
  lastFilter,
  lastFilterLabel,
  recordCommittedFilter,
  repeatAllowed,
  repeatsAsLastFilter,
} from "./lastFilter";

/**
 * 票 58 的记录与规则：只记住成功提交且可重复的滤镜，菜单与动作共用一份
 * 可用性判据。原版锚点：Filters.swift:28-29（repeatsAsLastFilter）、
 * :737-741（canRepeatLastFilter）、:866（提交后记下 kind）。
 */

function session(kind: string, label = "高斯模糊") {
  return { kind, label, destructive: true, settings: defaultFilterSettings(kind) };
}

beforeEach(() => {
  clearLastFilter();
});

describe("只记住可重复的滤镜", () => {
  it("Filter 菜单自己的滤镜都算可重复", () => {
    for (const kind of ["gaussianBlur", "motionBlur", "addNoise", "vignette", "bloomGlow",
      "tonalContrast", "lensCorrection", "cameraRaw", "dither", "removeBackground"]) {
      expect(repeatsAsLastFilter(kind), kind).toBe(true);
    }
  });

  it("内容感知填充与图像调整不算（原版 repeatsAsLastFilter）", () => {
    expect(repeatsAsLastFilter("contentAwareFill")).toBe(false);
    expect(repeatsAsLastFilter("adjust:Curves")).toBe(false);
    expect(repeatsAsLastFilter("adjust:Black & White")).toBe(false);
  });

  it("提交成功才记录，且记下 kind/标签/参数本身", () => {
    expect(get(lastFilter)).toBeNull();
    const settings = { ...defaultFilterSettings("gaussianBlur"), radius: 12.5 };
    recordCommittedFilter({ kind: "gaussianBlur", label: "高斯模糊", destructive: true, settings });
    expect(get(lastFilter)).toMatchObject({ kind: "gaussianBlur", label: "高斯模糊", destructive: true });
    expect(get(lastFilter)?.settings.radius).toBe(12.5);
    // 参数是同一份（含渐进模糊定位这类位置参数），重做时原样带上。
    expect(get(lastFilter)?.settings).toBe(settings);
  });

  it("不可重复的提交不会覆盖已有的上次滤镜", () => {
    recordCommittedFilter(session("motionBlur", "运动模糊"));
    recordCommittedFilter(session("adjust:Curves", "曲线"));
    recordCommittedFilter(session("contentAwareFill", "内容感知填充"));
    expect(get(lastFilter)?.kind).toBe("motionBlur");
  });
});

describe("可用性判据与菜单标题", () => {
  it("没有上次滤镜 / 没有可编辑图层 / 会话开着时都不可用", () => {
    const record = { kind: "dither", label: "抖动", destructive: true, settings: defaultFilterSettings("dither") };
    expect(repeatAllowed(null, true, false)).toBe(false);
    expect(repeatAllowed(record, false, false)).toBe(false);
    expect(repeatAllowed(record, true, true)).toBe(false);
    expect(repeatAllowed(record, true, false)).toBe(true);
  });

  it("标题跟着上次滤镜改名，没有时是裸标题（原版 Last Filter: <name>）", () => {
    expect(lastFilterLabel(null)).toBe("重做上一个滤镜");
    expect(lastFilterLabel({
      kind: "vignette", label: "晕影", destructive: true, settings: defaultFilterSettings("vignette"),
    })).toBe("重做上一个滤镜：晕影");
  });
});
