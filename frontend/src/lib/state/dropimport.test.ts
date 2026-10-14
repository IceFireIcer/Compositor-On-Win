import { describe, expect, it } from "vitest";
import {
  DROP_DEDUP_MS,
  DROP_SETTLE_MS,
  LAYER_DRAG_MIME,
  browserImageFiles,
  dropName,
  isImageName,
  prepareDroppedBlobs,
  readDropPayload,
  shouldImportDomDrop,
} from "./dropimport";

describe("拖放导入的前端判定（票 48）", () => {
  it("Wails 刚报过路径的这次拖放，DOM 侧不再重复导入", () => {
    expect(shouldImportDomDrop(null, 1000)).toBe(true);
    expect(shouldImportDomDrop(1000, 1000 + DROP_DEDUP_MS - 1)).toBe(false);
    expect(shouldImportDomDrop(1000, 1000 + DROP_DEDUP_MS + 1)).toBe(true);
  });

  it("等待窗口比去重窗口短：等到 Wails 回调后仍能判出去重", () => {
    expect(DROP_SETTLE_MS).toBeLessThan(DROP_DEDUP_MS);
    // DOM 事件先到（此时 Wails 还没报路径）→ 等一小会儿后路径已经到了 →
    // 这一次 DOM 不再接手。
    const dropAt = 1000;
    expect(shouldImportDomDrop(null, dropAt)).toBe(true); // 早判：还看不出重复
    const wailsAt = dropAt + 40; // 同一次拖放的路径回调
    expect(shouldImportDomDrop(wailsAt, dropAt + DROP_SETTLE_MS)).toBe(false);
    // 路径回调一直没来 → DOM 正常接手。
    expect(shouldImportDomDrop(null, dropAt + DROP_SETTLE_MS)).toBe(true);
  });

  it("从数据转移里读出文件与 uri-list（忽略注释与空行）", () => {
    const files = [{ name: "shot.png" } as File, { name: "doc.pdf" } as File];
    const payload = readDropPayload({
      files: { length: files.length, 0: files[0]!, 1: files[1]! },
      getData: (type) => (type === "text/uri-list" ? "# comment\r\nhttps://x.test/a.png\r\n\r\n" : ""),
    });
    expect(payload.files.map((f) => f.name)).toEqual(["shot.png", "doc.pdf"]);
    expect(payload.uris).toEqual(["https://x.test/a.png"]);
  });

  it("只把浏览器能解码的图像交给 blob 通道", () => {
    const png = { name: "a.png", type: "image/png" } as File;
    const jpegNoMime = { name: "b.JPG", type: "" } as File;
    const tiff = { name: "c.tiff", type: "image/tiff" } as File;
    const psd = { name: "d.psd", type: "" } as File;
    expect(browserImageFiles([png, jpegNoMime, tiff, psd]).map((f) => f.name)).toEqual(["a.png", "b.JPG"]);
    expect(isImageName("x.WebP")).toBe(true);
    expect(isImageName("x.psd")).toBe(false);
  });

  it("WebP 与相机 HEIF 都进名单：WebP 浏览器可解，HEIF 只能报错不能吞掉", () => {
    const webpNoMime = { name: "图.WebP", type: "" } as File;
    const webpMime = { name: "无名", type: "image/webp" } as File;
    const hif = { name: "相机.HIF", type: "" } as File;
    const heifMime = { name: "无名2", type: "image/heif" } as File;
    const heicMime = { name: "无名3", type: "image/heic" } as File;
    const psd = { name: "d.psd", type: "" } as File;
    expect(
      browserImageFiles([webpNoMime, webpMime, hif, heifMime, heicMime, psd]).map((f) => f.name),
    ).toEqual(["图.WebP", "无名", "相机.HIF", "无名2", "无名3"]);
    expect(isImageName("x.heic")).toBe(true);
    expect(isImageName("x.heif")).toBe(true);
    expect(isImageName("x.hif")).toBe(true);
    expect(isImageName("x.psd")).toBe(false);
  });

  it("没有路径的相机 HEIF 进失败清单（带上文件名），不静默丢弃", async () => {
    const hif = { name: "相机.hif", type: "image/heif" } as File;
    const webp = { name: "a.webp", type: "image/webp" } as File;
    const { ready, failed } = await prepareDroppedBlobs([hif, webp], async (blob) =>
      (blob as File).name === "a.webp" ? new Blob(["png"]) : null,
    );
    expect(ready.map((r) => r.name)).toEqual(["a.webp"]);
    expect(failed).toEqual(["相机.hif"]);
  });

  it("名字取自文件名，空名有兜底", () => {
    expect(dropName({ name: "shot.png" }, 0)).toBe("shot.png");
    expect(dropName({ name: "   " }, 2)).toBe("拖入的图像 3");
  });

  it("按顺序栅格化，保留与 Go 上传 token 一一对应的名字", async () => {
    const files = [
      { name: "a.png", type: "image/png" } as File,
      { name: "b.tiff", type: "image/tiff" } as File,
    ];
    const { ready, failed } = await prepareDroppedBlobs(files, async (blob) =>
      (blob as File).name === "a.png" ? new Blob(["png"]) : null,
    );
    expect(ready.map((r) => r.name)).toEqual(["a.png"]);
    expect(failed).toEqual(["b.tiff"]);
  });

  it("图层拖拽用的 MIME 与图层行/标签栏约定一致", () => {
    expect(LAYER_DRAG_MIME).toBe("application/x-compositor-layer");
  });
});
