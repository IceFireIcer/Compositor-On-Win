import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import {
  EXPORT_MAX_SIDE,
  MORE_EXPORT_FORMATS,
  PRIMARY_EXPORT_FORMATS,
  beginExportPreview,
  commitExport,
  endExportPreview,
  exportDialog,
  exportFormatDef,
  exportNotice,
  exportPNG,
  exportPreview,
  exportScalePercent,
  exportSizeForScale,
  linkedExportSize,
  openExportAs,
  openExportJPEG,
  rememberedQuality,
  updateExportPreview,
} from "./export";

/** 最小 localStorage 替身（node 环境没有 window.localStorage）。 */
function fakeStorage(): Storage {
  const map = new Map<string, string>();
  return {
    get length() {
      return map.size;
    },
    clear: () => map.clear(),
    getItem: (k: string) => (map.has(k) ? map.get(k)! : null),
    key: (i: number) => [...map.keys()][i] ?? null,
    removeItem: (k: string) => void map.delete(k),
    setItem: (k: string, v: string) => void map.set(k, String(v)),
  } as Storage;
}

const bridge = {
  BeginExportPreview: vi.fn(),
  EndExportPreview: vi.fn(),
  ExportAsPreview: vi.fn(),
  ExportAs: vi.fn(),
  ExportPNG: vi.fn(),
  ExportJPEG: vi.fn(),
  CopyMerged: vi.fn(),
};

beforeEach(() => {
  vi.useFakeTimers();
  vi.stubGlobal("localStorage", fakeStorage());
  vi.stubGlobal("window", { go: { bridge: { Service: bridge } } });
  vi.resetAllMocks();
  bridge.BeginExportPreview.mockResolvedValue(JSON.stringify({ width: 800, height: 600, resolution: 300 }));
  bridge.EndExportPreview.mockResolvedValue(undefined);
  exportDialog.set(null);
  exportPreview.set(null);
  exportNotice.set(null);
});

afterEach(() => {
  vi.clearAllTimers();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("等比缩放数学（原版 ExportAsSheet.setWidth/setHeight）", () => {
  const original = { width: 800, height: 600 };

  it("改宽：高按比例跟随", () => {
    expect(linkedExportSize(original, "width", 400)).toEqual({ width: 400, height: 300 });
    expect(linkedExportSize(original, "width", 200)).toEqual({ width: 200, height: 150 });
  });

  it("改高：宽按比例跟随", () => {
    expect(linkedExportSize(original, "height", 300)).toEqual({ width: 400, height: 300 });
  });

  it("至少 1 像素，且不超过边长上限", () => {
    expect(linkedExportSize(original, "width", 0)).toEqual({ width: 1, height: 1 });
    const huge = linkedExportSize(original, "width", 999999);
    expect(huge.width).toBe(EXPORT_MAX_SIDE);
    expect(huge.height).toBe(Math.round((600 * EXPORT_MAX_SIDE) / 800));
  });

  it("非数字输入退回原尺寸", () => {
    expect(linkedExportSize(original, "width", Number.NaN)).toEqual(original);
    expect(linkedExportSize(original, "height", Number.POSITIVE_INFINITY)).toEqual(original);
  });

  it("百分比缩放（25/50/75/100/200 菜单）", () => {
    expect(exportSizeForScale(original, 0.5)).toEqual({ width: 400, height: 300 });
    expect(exportSizeForScale(original, 2)).toEqual({ width: 1600, height: 1200 });
    expect(exportSizeForScale(original, Number.NaN)).toEqual(original);
  });

  it("显示当前百分比", () => {
    expect(exportScalePercent(original, { width: 400, height: 300 })).toBe(50);
    expect(exportScalePercent(original, original)).toBe(100);
    expect(exportScalePercent({ width: 0, height: 0 }, { width: 10, height: 10 })).toBe(100);
  });
});

describe("格式集合（首要格式 vs 更多格式）", () => {
  it("JPEG/PNG 为首要格式，TIFF/EXR/PDF 在更多格式区", () => {
    expect(PRIMARY_EXPORT_FORMATS).toEqual(["jpeg", "png"]);
    // 票 85 把 EXR 加进了更多格式区。
    expect(MORE_EXPORT_FORMATS).toEqual(["tiff", "exr", "pdf"]);
    expect(exportFormatDef("tiff").label).toBe("TIFF");
    expect(exportFormatDef("exr").extension).toBe("exr");
    expect(exportFormatDef("pdf").extension).toBe("pdf");
    expect(exportFormatDef("jpeg").extension).toBe("jpg");
  });

  it("只有 PNG/TIFF 保留透明度（棋盘格预览）", () => {
    expect(exportFormatDef("png").keepsAlpha).toBe(true);
    expect(exportFormatDef("tiff").keepsAlpha).toBe(true);
    expect(exportFormatDef("jpeg").keepsAlpha).toBe(false);
    expect(exportFormatDef("pdf").keepsAlpha).toBe(false);
  });
});

describe("打开导出窗口", () => {
  it("导出为… 从 PNG 起步（原版 lastExportFormat 默认 PNG）", () => {
    openExportAs();
    expect(get(exportDialog)).toEqual({ kind: "png" });
  });

  it("导出 JPEG… 在 JPEG 上打开", () => {
    openExportJPEG();
    expect(get(exportDialog)).toEqual({ kind: "jpeg" });
  });

  it("成功导出后记住格式，下次导出为… 沿用", async () => {
    bridge.ExportAs.mockResolvedValue(JSON.stringify({ path: "C:/tmp/a.tiff" }));
    await commitExport({ format: "tiff", width: 100, height: 50, quality: 85, background: "#ffffff" });
    exportDialog.set(null);
    openExportAs();
    expect(get(exportDialog)).toEqual({ kind: "tiff" });
  });
});

describe("导出预览会话", () => {
  it("Begin 记录压平尺寸与分辨率", async () => {
    await beginExportPreview();
    expect(get(exportPreview)).toMatchObject({ width: 800, height: 600, resolution: 300, previewURL: null, size: 0 });
  });

  it("旧预览请求不得覆盖新结果", async () => {
    await beginExportPreview();
    let resolveOld!: (value: string) => void;
    const stale = new Promise<string>((resolve) => {
      resolveOld = resolve;
    });
    bridge.ExportAsPreview
      .mockImplementationOnce(() => stale)
      .mockImplementationOnce(() =>
        Promise.resolve(
          JSON.stringify({
            url: "/pixel/stage/new.png",
            size: 20,
            width: 200,
            height: 150,
            previewWidth: 200,
            previewHeight: 150,
          }),
        ),
      );
    const first = updateExportPreview({ format: "png", width: 400, height: 300, quality: 85, background: "#ffffff" });
    const second = updateExportPreview({ format: "png", width: 200, height: 150, quality: 85, background: "#ffffff" });
    await second;
    // 旧请求随后才落地：必须被丢弃，不能盖掉较新的预览。
    resolveOld(
      JSON.stringify({
        url: "/pixel/stage/old.png",
        size: 99,
        width: 800,
        height: 600,
        previewWidth: 800,
        previewHeight: 600,
      }),
    );
    await first;
    const state = get(exportPreview)!;
    expect(state.previewURL).toContain("/pixel/stage/new.png");
    expect(state.targetWidth).toBe(200);
    expect(state.size).toBe(20);
  });

  it("End 清空会话并释放 Go 侧暂存", async () => {
    await beginExportPreview();
    endExportPreview();
    expect(get(exportPreview)).toBeNull();
    expect(bridge.EndExportPreview).toHaveBeenCalledTimes(1);
  });
});

describe("提交导出", () => {
  it("把设置与记住的文件名发给 ExportAs，并记住保存位置", async () => {
    localStorage.setItem("compositor.export.name", "作品");
    bridge.ExportAs.mockResolvedValue(JSON.stringify({ path: "C:/tmp/作品.tiff" }));
    const ok = await commitExport({ format: "tiff", width: 400, height: 300, quality: 70, background: "#112233" });
    expect(ok).toBe(true);
    const payload = JSON.parse(bridge.ExportAs.mock.calls[0][0] as string);
    expect(payload).toMatchObject({
      format: "tiff",
      width: 400,
      height: 300,
      quality: 70,
      background: "#112233",
      defaultName: "作品.tif",
    });
    expect(localStorage.getItem("compositor.export.name")).toBe("作品");
    expect(get(exportNotice)).toContain("TIFF");
  });

  it("取消（空路径）返回 false 且不提示", async () => {
    bridge.ExportAs.mockResolvedValue(JSON.stringify({ path: "" }));
    expect(await commitExport({ format: "png", width: 10, height: 10, quality: 85, background: "#ffffff" })).toBe(false);
    expect(get(exportNotice)).toBeNull();
  });

  it("JPEG 质量记到 localStorage（原版 jpegExportQuality）", async () => {
    bridge.ExportAs.mockResolvedValue(JSON.stringify({ path: "C:/tmp/a.jpg" }));
    await commitExport({ format: "jpeg", width: 10, height: 10, quality: 42, background: "#ffffff" });
    expect(rememberedQuality()).toBe(42);
    expect(rememberedQuality()).toBeLessThanOrEqual(100);
  });
});

describe("既有快捷入口", () => {
  it("exportPNG 直接落保存对话框并提示", async () => {
    bridge.ExportPNG.mockResolvedValue(JSON.stringify({ path: "C:/tmp/作品.png" }));
    await exportPNG();
    expect(bridge.ExportPNG).toHaveBeenCalledWith("未命名.png");
    expect(get(exportNotice)).toContain("PNG");
  });
});
