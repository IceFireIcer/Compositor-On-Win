import { beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";

vi.mock("../../../wailsjs/go/bridge/Workspace", () => ({
  Snapshot: vi.fn(),
}));
vi.mock("../../../wailsjs/go/bridge/Service", () => ({
  RawBeginDevelop: vi.fn(),
  RawCancelDevelop: vi.fn(),
  RawDevelopPreview: vi.fn(),
  RawFinishDevelop: vi.fn(),
}));
vi.mock("./workspace", async (importOriginal) => ({
  ...(await importOriginal<typeof import("./workspace")>()),
  applySnapshot: vi.fn(),
}));
vi.mock("./document", () => ({
  loadDocument: vi.fn(),
}));

import {
  RawBeginDevelop,
  RawCancelDevelop,
  RawDevelopPreview,
  RawFinishDevelop,
} from "../../../wailsjs/go/bridge/Service";
import { Snapshot as GetSnapshot } from "../../../wailsjs/go/bridge/Workspace";
import {
  closeRawDevelop,
  developSettingsPayload,
  isAsShot,
  queueRawDevelop,
  rawDevelop,
  resetRawDevelopSettings,
  updateRawDevelopPreview,
  type RawDevelopSettings,
} from "./rawdevelop";

const mockedBegin = vi.mocked(RawBeginDevelop);
const mockedCancel = vi.mocked(RawCancelDevelop);
const mockedPreview = vi.mocked(RawDevelopPreview);
const mockedFinish = vi.mocked(RawFinishDevelop);
const mockedSnapshot = vi.mocked(GetSnapshot);

/** 相机支持降噪与高光恢复、没有镜头配置文件（LibRaw 的实际情况）。 */
function supported(): RawDevelopSettings {
  return {
    exposure: 0.4,
    temperature: 7200,
    tint: 12,
    boost: 0.8,
    noiseReduction: 0.35,
    lensCorrection: true,
    highlightRecovery: true,
    asShotTemperature: 5400,
    asShotTint: -6,
    asShotNoiseReduction: 0.1,
    asShotLensCorrection: false,
    asShotHighlightRecovery: false,
    supportsNoiseReduction: true,
    supportsLensCorrection: false,
    supportsHighlightRecovery: true,
  };
}

function beginReply(asShot: RawDevelopSettings): string {
  return JSON.stringify({ name: "样片", fullWidth: 6000, fullHeight: 4000, asShot });
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("RAW 显影设置（票 64）", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    rawDevelop.set(null);
  });

  it("能力位为 false 的参数不进请求（置灰的控件不悄悄生效）", () => {
    const payload = developSettingsPayload(supported());
    expect(payload.noiseReduction).toBe(0.35);
    expect(payload.highlightRecovery).toBe(true);
    // LibRaw 没有镜头配置文件：即使设置里残留 true 也不发出去。
    expect(payload.lensCorrection).toBe(false);
    // 原设置不被改动。
    expect(supported().lensCorrection).toBe(true);
  });

  it("相机完全不支持时三个参数都不生效，其余照发", () => {
    const none = {
      ...supported(),
      supportsNoiseReduction: false,
      supportsHighlightRecovery: false,
    };
    const payload = developSettingsPayload(none);
    expect(payload.noiseReduction).toBe(0);
    expect(payload.lensCorrection).toBe(false);
    expect(payload.highlightRecovery).toBe(false);
    expect(payload.exposure).toBe(0.4);
    expect(payload.temperature).toBe(7200);
    expect(payload.tint).toBe(12);
    expect(payload.boost).toBe(0.8);
  });

  it("Reset 回到 asShot*（三个新参数也一样），且不改动入参", () => {
    const before = supported();
    const after = resetRawDevelopSettings(before);
    expect(after.exposure).toBe(0);
    expect(after.boost).toBe(1);
    expect(after.temperature).toBe(before.asShotTemperature);
    expect(after.tint).toBe(before.asShotTint);
    expect(after.noiseReduction).toBe(before.asShotNoiseReduction);
    expect(after.lensCorrection).toBe(before.asShotLensCorrection);
    expect(after.highlightRecovery).toBe(before.asShotHighlightRecovery);
    // 入参不动（面板里是整体替换）。
    expect(before.noiseReduction).toBe(0.35);
    expect(before.exposure).toBe(0.4);
  });

  it("isAsShot 覆盖三个新参数：Reset 在 asShot 时禁用", () => {
    const shot = resetRawDevelopSettings(supported());
    expect(isAsShot(shot)).toBe(true);
    for (const changed of [
      { ...shot, noiseReduction: shot.noiseReduction + 0.1 },
      { ...shot, lensCorrection: !shot.lensCorrection },
      { ...shot, highlightRecovery: !shot.highlightRecovery },
      { ...shot, exposure: 1 },
      { ...shot, temperature: shot.temperature + 100 },
    ]) {
      expect(isAsShot(changed)).toBe(false);
    }
  });
});

describe("RAW 显影的预览与导入（票 64）", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    rawDevelop.set(null);
    mockedPreview.mockResolvedValue(JSON.stringify({ url: "/pixel/a", width: 800, height: 533 }));
  });

  it("预览与最终导入用同一份设置（含能力位过滤）", async () => {
    const asShot = supported();
    mockedBegin.mockResolvedValue(beginReply(asShot));
    mockedFinish.mockResolvedValue("");
    mockedSnapshot.mockResolvedValue("{}" as never);
    await queueRawDevelop(["C:/raw/a.cr2"]);
    const settings = get(rawDevelop)!.asShot;
    // queueRawDevelop 已经用同一份 asShot 发过一帧预览；导入必须发同一份。
    await vi.waitFor(() => expect(mockedPreview).toHaveBeenCalledTimes(1));
    await updateRawDevelopPreview(settings);
    await import("./rawdevelop").then((m) => m.finishRawDevelop(settings));

    expect(mockedFinish).toHaveBeenCalledTimes(1);
    const previewed = JSON.parse(mockedPreview.mock.calls[0][0]);
    const explicit = JSON.parse(mockedPreview.mock.calls[1][0]);
    const imported = JSON.parse(mockedFinish.mock.calls[0][1]);
    expect(explicit).toEqual(imported);
    expect(previewed).toEqual(imported);
    expect(previewed.lensCorrection).toBe(false);
    expect(previewed.supportsLensCorrection).toBe(false);
    expect(mockedFinish.mock.calls[0][0]).toBe("C:/raw/a.cr2");
  });

  it("打不开的文件不把队列卡死：跳过它继续开下一个", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const asShot = supported();
    mockedBegin.mockRejectedValueOnce(new Error("坏文件")).mockResolvedValueOnce(beginReply(asShot));
    mockedCancel.mockResolvedValue(undefined);

    await queueRawDevelop(["C:/raw/bad.cr2", "C:/raw/good.cr2"]);

    expect(mockedBegin.mock.calls.map((c) => c[0])).toEqual(["C:/raw/bad.cr2", "C:/raw/good.cr2"]);
    expect(get(rawDevelop)?.path).toBe("C:/raw/good.cr2");
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
    closeRawDevelop();
  });

  it("导入把 busy 延续到图层落地，失败则留在面板上", async () => {
    const asShot = supported();
    mockedBegin.mockResolvedValue(beginReply(asShot));
    mockedSnapshot.mockResolvedValue("{}" as never);
    const pending = deferred<string>();
    mockedFinish.mockReturnValue(pending.promise);

    await queueRawDevelop(["C:/raw/a.nef"]);
    const m = await import("./rawdevelop");
    const importing = m.finishRawDevelop(get(rawDevelop)!.asShot);
    await Promise.resolve();
    // 图层还没落地：busy 仍为 true，面板不关闭。
    expect(get(rawDevelop)?.busy).toBe(true);

    pending.reject(new Error("提交失败"));
    await importing;
    expect(get(rawDevelop)?.busy).toBe(false);
    expect(get(rawDevelop)?.error).toBe("提交失败");
    closeRawDevelop();
  });
});