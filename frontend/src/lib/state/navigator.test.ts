import { beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";
import {
  NAVIGATOR_LARGEST,
  NAVIGATOR_ZOOM_SHOWN,
  centerView,
  intersectRects,
  navigatorDocumentPoint,
  navigatorImageRect,
  navigatorKeyboardTarget,
  navigatorShown,
  navigatorSize,
  navigatorThumbnailRect,
  navigatorViewportBox,
  visibleDocumentRect,
  type NavigatorGeometry,
} from "./navigator";
import { fit, resize, setZoom, type ViewportState } from "./viewport";
import { showsNavigator } from "./view";

/**
 * 票 61 / 对等矩阵 U08 的纯几何层。真值：reference/Swift/Compositor/Rendering/
 * NavigatorGeometry.swift（52 行）与 CompositorTests/NavigatorTests.swift（133 行）。
 * 每条断言按原版用例逐条对照数值；原版里需要 CoreGraphics 合成的那几条
 * （navigatorImage 的像素与 adjustment 结果）不在这里，理由见票面回报。
 */

/** 原版断言都用 `abs(a - b) < 0.001`，这里等价成 3 位小数。 */
function expectClose(actual: number, expected: number): void {
  expect(Math.abs(actual - expected)).toBeLessThan(0.001);
}

/** 原版 NavigatorTests.viewport()：400 × 300 的文档放进 800 × 600 的视图，1× backing。 */
function fittedViewport(): ViewportState {
  return fit(
    {
      viewWidth: 800,
      viewHeight: 600,
      backingScale: 1,
      zoom: 1,
      panX: 0,
      panY: 0,
      followsFit: true,
    },
    400,
    300,
  );
}

const BOX: NavigatorGeometry = {
  documentSize: { width: 400, height: 200 },
  box: { width: 200, height: 200 },
};

describe("NavigatorGeometry（NavigatorGeometry.swift 逐条移植）", () => {
  it("documentFitsTheBoxCentered：文档等比放进盒子并居中", () => {
    expect(navigatorImageRect(BOX)).toEqual({ x: 0, y: 50, width: 200, height: 100 });
  });

  it("thumbnailRect 按图片比例把文档矩形映到盒子里", () => {
    expect(navigatorThumbnailRect(BOX, { x: 100, y: 50, width: 200, height: 100 })).toEqual({
      x: 50,
      y: 75,
      width: 100,
      height: 50,
    });
  });

  it("documentPoint 把盒子里的点换回文档像素", () => {
    expect(navigatorDocumentPoint(BOX, { x: 100, y: 100 })).toEqual({ x: 200, y: 100 });
  });

  it("图片外的点被夹在文档边缘上", () => {
    expect(navigatorDocumentPoint(BOX, { x: -20, y: 199 })).toEqual({ x: 0, y: 200 });
    // 右侧/下侧越界同样夹住。
    expect(navigatorDocumentPoint(BOX, { x: 999, y: 999 })).toEqual({ x: 400, y: 200 });
  });

  it("空文档或空盒子：imageRect 归零，documentPoint 归零", () => {
    const empty = { documentSize: { width: 0, height: 0 }, box: { width: 10, height: 10 } };
    expect(navigatorImageRect(empty)).toEqual({ x: 0, y: 0, width: 0, height: 0 });
    expect(navigatorDocumentPoint(empty, { x: 5, y: 5 })).toEqual({ x: 0, y: 0 });
    const noBox = { documentSize: { width: 400, height: 200 }, box: { width: 0, height: 0 } };
    expect(navigatorImageRect(noBox)).toEqual({ x: 0, y: 0, width: 0, height: 0 });
  });
});

describe("缩略图尺寸（NavigatorMinimap.size(for:)）", () => {
  it("minimapFitsTheDocument：等比放进最大尺寸并取整", () => {
    expect(NAVIGATOR_LARGEST).toEqual({ width: 126, height: 90 });
    expect(navigatorSize({ width: 4000, height: 3000 })).toEqual({ width: 120, height: 90 });
    expect(navigatorSize({ width: 6000, height: 2000 })).toEqual({ width: 126, height: 42 });
    expect(navigatorSize({ width: 0, height: 0 })).toEqual({ width: 0, height: 0 });
  });

  it("原版阈值：300% 起显示", () => {
    expect(NAVIGATOR_ZOOM_SHOWN).toBe(3);
  });
});

describe("可见文档矩形（CanvasViewport.visibleDocumentRect）", () => {
  it("visibleRectFollowsZoomAndPan：800 × 600 点在 4× 下显示 200 × 150 像素", () => {
    const size = { width: 400, height: 300 };
    const vp = fittedViewport();
    const zoomed = setZoom(vp, 4, vp.viewWidth / 2, vp.viewHeight / 2, size.width, size.height);
    const visible = visibleDocumentRect(zoomed, size.width, size.height);
    expectClose(visible.width, 200);
    expectClose(visible.height, 150);
    expectClose(visible.x + visible.width / 2, 200);
    expectClose(visible.y + visible.height / 2, 150);
  });

  it("画布显示得比文档还多时，可见矩形越出文档边缘", () => {
    // 400 × 300 的文档在 800 × 600 的视图里 fit 到 1.68×，可见 476 × 357 像素。
    const visible = visibleDocumentRect(fittedViewport(), 400, 300);
    expect(visible.x).toBeLessThan(0);
    expect(visible.y).toBeLessThan(0);
    expectClose(visible.width, 800 / 1.68);
    expectClose(visible.height, 600 / 1.68);
  });
});

describe("居中（CanvasViewport.centerView）", () => {
  it("centeringKeepsZoomAndStopsFollowingFit：缩放不变，之后 resize 不再重新 fit", () => {
    const size = { width: 400, height: 300 };
    const vp = fittedViewport();
    const zoom = vp.zoom;
    const centered = centerView(vp, { x: 50, y: 60 }, size.width, size.height);
    const visible = visibleDocumentRect(centered, size.width, size.height);
    expectClose(visible.x + visible.width / 2, 50);
    expectClose(visible.y + visible.height / 2, 60);
    expect(centered.zoom).toBe(zoom);
    expect(centered.followsFit).toBe(false);

    // 之后再 resize 仍停在人放的位置；若还在跟 Fit，这里会重新 fit 到约 2.01×。
    const resized = resize(centered, 1000, 700, 1, size.width, size.height);
    expect(resized.zoom).toBe(zoom);
  });

  it("文档边缘：居中到角落时可见矩形贴住文档边", () => {
    const size = { width: 400, height: 300 };
    const centered = centerView(fittedViewport(), { x: 0, y: 0 }, size.width, size.height);
    const visible = visibleDocumentRect(centered, size.width, size.height);
    expectClose(visible.x + visible.width / 2, 0);
    expectClose(visible.y + visible.height / 2, 0);
  });
});

describe("缩略图 ↔ 文档往返映射", () => {
  it("视口框中心映射回可见矩形的中心（缩放/平移后仍成立）", () => {
    const visible = { x: 50, y: 25, width: 100, height: 50 };
    const box = navigatorThumbnailRect(BOX, visible);
    const back = navigatorDocumentPoint(BOX, {
      x: box.x + box.width / 2,
      y: box.y + box.height / 2,
    });
    expectClose(back.x, visible.x + visible.width / 2);
    expectClose(back.y, visible.y + visible.height / 2);
  });

  it("画布旋转 90°（宽高互换）后，缩略图跟着换比例且映射仍正确", () => {
    const rotated: NavigatorGeometry = {
      documentSize: { width: 200, height: 400 },
      box: { width: 50, height: 100 },
    };
    expect(navigatorImageRect(rotated)).toEqual({ x: 0, y: 0, width: 50, height: 100 });
    const box = navigatorThumbnailRect(rotated, { x: 0, y: 200, width: 200, height: 200 });
    expect(box).toEqual({ x: 0, y: 50, width: 50, height: 50 });
    expect(navigatorDocumentPoint(rotated, { x: 25, y: 75 })).toEqual({ x: 100, y: 300 });
  });

  it("视口框裁到图片范围：显示超过整幅时框住整幅，完全在外时不给框", () => {
    const whole = intersectRects(
      navigatorThumbnailRect(BOX, { x: -100, y: -50, width: 600, height: 300 }),
      navigatorImageRect(BOX),
    );
    expect(whole).toEqual({ x: 0, y: 50, width: 200, height: 100 });
    expect(intersectRects({ x: -10, y: -10, width: 5, height: 5 }, navigatorImageRect(BOX))).toBeNull();
    // 相接但不重叠（零面积）按"没有框"处理，与原版 isEmpty 的跳过一致。
    expect(intersectRects({ x: -10, y: 0, width: 10, height: 10 }, navigatorImageRect(BOX))).toBeNull();
  });

  it("navigatorViewportBox 组合上面两步", () => {
    const visible = { x: 100, y: 50, width: 200, height: 100 };
    expect(navigatorViewportBox(BOX, visible)).toEqual({ x: 50, y: 75, width: 100, height: 50 });
  });
});

describe("显示条件（原版 ContentView.swift:114 + NavigatorMinimap.zoomShown）", () => {
  const base = {
    enabled: true,
    zoom: 3,
    documentWidth: 400,
    documentHeight: 300,
    canvasOnly: false,
  };

  it("默认关；打开后 300% 及以上才显示", () => {
    expect(navigatorShown({ ...base, enabled: false })).toBe(false);
    expect(navigatorShown({ ...base, zoom: 2.99 })).toBe(false);
    expect(navigatorShown({ ...base, zoom: 3 })).toBe(true);
    expect(navigatorShown({ ...base, zoom: 8 })).toBe(true);
  });

  it("纯画布模式收起，没有文档也不显示", () => {
    expect(navigatorShown({ ...base, canvasOnly: true })).toBe(false);
    expect(navigatorShown({ ...base, documentWidth: 0 })).toBe(false);
    expect(navigatorShown({ ...base, documentHeight: 0 })).toBe(false);
  });
});

describe("键盘微调（最小可达性支持）", () => {
  it("方向键按可见区域的一格（1/8）移动视图中心", () => {
    const visible = { x: 100, y: 75, width: 200, height: 150 };
    expect(navigatorKeyboardTarget(visible, 1, 0)).toEqual({ x: 225, y: 150 });
    expect(navigatorKeyboardTarget(visible, -1, 0)).toEqual({ x: 175, y: 150 });
    expect(navigatorKeyboardTarget(visible, 0, 1)).toEqual({ x: 200, y: 168.75 });
    expect(navigatorKeyboardTarget(visible, 0, 0)).toEqual({ x: 200, y: 150 });
  });
});

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

describe("导航器偏好持久化（localStorage 键 navigator，原版 @AppStorage）", () => {
  let storage: Storage;

  beforeEach(() => {
    storage = fakeStorage();
    vi.stubGlobal("localStorage", storage);
    showsNavigator.set(false);
  });

  it("默认关；set / update 都写盘（偏好随菜单开关落盘）", () => {
    expect(get(showsNavigator)).toBe(false);

    showsNavigator.set(true);
    expect(get(showsNavigator)).toBe(true);
    expect(storage.getItem("navigator")).toBe("true");

    showsNavigator.update((v) => !v);
    expect(get(showsNavigator)).toBe(false);
    expect(storage.getItem("navigator")).toBe("false");
  });

  it("存储被禁用或抛错时不抛异常，内存里仍然生效", () => {
    vi.stubGlobal("localStorage", undefined);
    expect(() => showsNavigator.set(true)).not.toThrow();

    vi.stubGlobal("localStorage", {
      ...fakeStorage(),
      getItem: () => {
        throw new Error("blocked");
      },
      setItem: () => {
        throw new Error("blocked");
      },
    } as unknown as Storage);
    expect(() => showsNavigator.set(true)).not.toThrow();
    expect(get(showsNavigator)).toBe(true); // 内存里仍然生效
  });
});
