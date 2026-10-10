import { beforeEach, describe, expect, it, vi } from "vitest";
import { get } from "svelte/store";

vi.mock("../../../wailsjs/go/bridge/Service", () => ({
  ApplyUpdate: vi.fn(),
  CheckForUpdates: vi.fn(),
  DownloadUpdate: vi.fn(),
  UpdateInfo: vi.fn(),
}));

import {
  ApplyUpdate,
  CheckForUpdates,
  DownloadUpdate,
  UpdateInfo,
} from "../../../wailsjs/go/bridge/Service";
import {
  applyUpdate,
  checkForUpdates,
  downloadUpdate,
  loadUpdateInfo,
  updateSheet,
  updateState,
  watchUpdateProgress,
} from "./update";

const mockedCheck = vi.mocked(CheckForUpdates);
const mockedDownload = vi.mocked(DownloadUpdate);
const mockedApply = vi.mocked(ApplyUpdate);
const mockedInfo = vi.mocked(UpdateInfo);

describe("自动更新的前端状态（票 50）", () => {
  beforeEach(() => {
    updateState.set({ status: "idle", currentVersion: "" });
    updateSheet.set(false);
    mockedCheck.mockReset();
    mockedDownload.mockReset();
    mockedApply.mockReset();
    mockedInfo.mockReset();
  });

  it("有新版本：状态 available 并弹表单（静默检查也弹）", async () => {
    mockedCheck.mockResolvedValue(
      JSON.stringify({
        status: "available",
        currentVersion: "0.1.0",
        version: "0.2.0",
        notes: "修了几个 bug",
        size: 12_345_678,
      }),
    );
    const state = await checkForUpdates({ silent: true });
    expect(state.status).toBe("available");
    expect(state.version).toBe("0.2.0");
    expect(get(updateState).notes).toBe("修了几个 bug");
    expect(get(updateSheet)).toBe(true);
  });

  it("已是最新：静默检查不弹，手动检查才弹", async () => {
    mockedCheck.mockResolvedValue(JSON.stringify({ status: "up-to-date", currentVersion: "0.1.0" }));
    await checkForUpdates({ silent: true });
    expect(get(updateState).status).toBe("up-to-date");
    expect(get(updateSheet)).toBe(false);

    await checkForUpdates();
    expect(get(updateSheet)).toBe(true);
  });

  it("没配更新源 / 网络失败：都是状态，不抛异常", async () => {
    mockedCheck.mockResolvedValue(
      JSON.stringify({ status: "unconfigured", currentVersion: "0.1.0", error: "没有 feed 地址" }),
    );
    await checkForUpdates();
    expect(get(updateState).status).toBe("unconfigured");
    expect(get(updateState).error).toBe("没有 feed 地址");

    mockedCheck.mockRejectedValue(new Error("no bridge"));
    await checkForUpdates();
    expect(get(updateState).status).toBe("error");
    expect(get(updateState).error).toContain("no bridge");
  });

  it("下载成功进入 ready 并记下路径；失败进入 error", async () => {
    mockedDownload.mockResolvedValue(
      JSON.stringify({ path: "C:\\Temp\\Compositor-0.2.0-setup.exe", version: "0.2.0" }),
    );
    await expect(downloadUpdate()).resolves.toBe(true);
    expect(get(updateState).status).toBe("ready");
    expect(get(updateState).path).toContain("Compositor-0.2.0-setup.exe");
    expect(get(updateState).progress).toBe(1);

    mockedDownload.mockRejectedValue(new Error("安装器 sha256 不符（下载可能被篡改）"));
    await expect(downloadUpdate()).resolves.toBe(false);
    expect(get(updateState).status).toBe("error");
    expect(get(updateState).error).toContain("sha256");
  });

  it("应用更新：成功进入 applying，失败进入 error", async () => {
    mockedApply.mockResolvedValue(JSON.stringify({ launched: true }));
    await expect(applyUpdate()).resolves.toBe(true);
    expect(get(updateState).status).toBe("applying");

    mockedApply.mockRejectedValue(new Error("还没有下载好的更新包"));
    await expect(applyUpdate()).resolves.toBe(false);
    expect(get(updateState).error).toContain("还没有下载好的更新包");
  });

  it("更新信息：只写版本与配置状态，不覆盖检查更新的状态", async () => {
    mockedInfo.mockResolvedValue(JSON.stringify({ currentVersion: "0.1.0", configured: false }));
    await loadUpdateInfo();
    expect(get(updateState).currentVersion).toBe("0.1.0");
    expect(get(updateState).configured).toBe(false);
    // 关键：并发启动时 loadUpdateInfo 不许把"正在检查"改写成别的状态。
    expect(get(updateState).status).toBe("idle");

    updateState.set({ status: "checking", currentVersion: "0.1.0" });
    mockedInfo.mockResolvedValue(JSON.stringify({ currentVersion: "0.1.0", configured: true }));
    await loadUpdateInfo();
    expect(get(updateState).status).toBe("checking");
    expect(get(updateState).configured).toBe(true);
  });

  it("没有 Wails 运行时时进度订阅是空订阅（vite dev 不崩）", () => {
    const stop = watchUpdateProgress();
    expect(typeof stop).toBe("function");
    stop();
  });

  it("有运行时时进度事件只在下载中更新状态", () => {
    const handlers = new Map<string, (...args: unknown[]) => void>();
    (globalThis as unknown as { runtime: unknown }).runtime = {
      EventsOn: (name: string, fn: (...args: unknown[]) => void) => {
        handlers.set(name, fn);
        return () => handlers.delete(name);
      },
    };
    try {
      const stop = watchUpdateProgress();
      const emit = handlers.get("update:progress");
      expect(emit).toBeTypeOf("function");

      // 不在下载中：事件被忽略。
      updateState.set({ status: "available", currentVersion: "0.1.0" });
      emit!(50, 100);
      expect(get(updateState).progress).toBeUndefined();

      // 下载中：记录比例。
      updateState.set({ status: "downloading", currentVersion: "0.1.0", progress: 0 });
      emit!(25, 100);
      expect(get(updateState).progress).toBeCloseTo(0.25);
      // 总大小未知时不显示进度，也不除零。
      emit!(25, 0);
      expect(get(updateState).progress).toBeUndefined();
      stop();
    } finally {
      delete (globalThis as unknown as { runtime?: unknown }).runtime;
    }
  });
});
