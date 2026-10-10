import { writable } from "svelte/store";
import {
  ApplyUpdate,
  CheckForUpdates,
  DownloadUpdate,
  UpdateInfo,
} from "../../../wailsjs/go/bridge/Service";

/**
 * 自动更新的前端状态（票 50）。真源在 Go（internal/bridge/update.go）：这里只把
 * "检查 → 下载 → 安装"三件事的**状态**摊平给 UI，网络/格式失败都是状态而不是异常
 * （用户该看到"检查失败"，而不是控制台里一条红字）。
 */

export type UpdateStatus =
  | "idle"
  | "checking"
  | "available"
  | "up-to-date"
  | "unconfigured"
  | "error"
  | "downloading"
  | "ready"
  | "applying";

export interface UpdateState {
  status: UpdateStatus;
  /** 当前应用版本（来自 Go 的构建注入）。 */
  currentVersion: string;
  /** 可更新到的版本。 */
  version?: string;
  notes?: string;
  publishedAt?: string;
  size?: number;
  /** 失败原因（status === "error" 时）。 */
  error?: string;
  /** 下载进度 0–1；总大小未知时 undefined。 */
  progress?: number;
  /** 已校验并落盘的安装包路径（status === "ready" 时）。 */
  path?: string;
  /** 这个构建有没有配置更新源（来自 UpdateInfo，不联网）。 */
  configured?: boolean;
}

export const updateState = writable<UpdateState>({ status: "idle", currentVersion: "" });

/** 更新表单是否打开（菜单"检查更新…"，或启动时静默发现新版）。 */
export const updateSheet = writable(false);

/** 一次"检查更新"的原始回复（Go 的 updateStatusReply）。 */
interface StatusReply {
  status?: string;
  currentVersion?: string;
  version?: string;
  notes?: string;
  publishedAt?: string;
  size?: number;
  error?: string;
}

/**
 * 检查更新。silent=true 时（启动时的静默检查）只有**真的发现有新版**才弹表单：
 * 已是最新、没配更新源、网络不通都安静地留在状态里。
 */
export async function checkForUpdates(options: { silent?: boolean } = {}): Promise<UpdateState> {
  updateState.update((s) => ({ ...s, status: "checking", error: undefined }));
  let reply: StatusReply = {};
  try {
    reply = JSON.parse(await CheckForUpdates()) as StatusReply;
  } catch (err) {
    reply = { status: "error", error: err instanceof Error ? err.message : String(err) };
  }
  const status = (reply.status ?? "error") as UpdateStatus;
  const next: UpdateState = {
    status,
    currentVersion: reply.currentVersion ?? "",
    version: reply.version,
    notes: reply.notes,
    publishedAt: reply.publishedAt,
    size: reply.size,
    error: reply.error,
  };
  updateState.set(next);
  // 手动检查：什么状态都弹（用户问了就该看到答案）；启动时的静默检查只在
  // 真发现有新版时弹，别打扰人。
  if (status === "available" || !options.silent) updateSheet.set(true);
  return next;
}

/**
 * 当前版本与"这个构建有没有更新源"（不联网）。
 *
 * 只写 currentVersion/configured，**不碰 status**：它和 checkForUpdates 是并发启动的
 * （App 挂载时两个一起发），谁先回来不确定；如果这里也写 status，就可能把"正在检查"
 * 覆盖成"未配置"。状态只由检查更新那条路决定。
 */
export async function loadUpdateInfo(): Promise<void> {
  try {
    const info = JSON.parse(await UpdateInfo()) as {
      currentVersion?: string;
      configured?: boolean;
    };
    updateState.update((s) => ({
      ...s,
      currentVersion: info.currentVersion ?? s.currentVersion,
      configured: info.configured,
    }));
  } catch (err) {
    console.warn("读取更新信息失败", err);
  }
}

/** 下载并校验安装器（校验在 Go 侧：sha256 + Ed25519 签名）。 */
export async function downloadUpdate(): Promise<boolean> {
  updateState.update((s) => ({ ...s, status: "downloading", progress: 0, error: undefined }));
  try {
    const reply = JSON.parse(await DownloadUpdate()) as { path?: string; version?: string };
    updateState.update((s) => ({
      ...s,
      status: "ready",
      path: reply.path,
      version: reply.version ?? s.version,
      progress: 1,
    }));
    return true;
  } catch (err) {
    updateState.update((s) => ({
      ...s,
      status: "error",
      error: err instanceof Error ? err.message : String(err),
    }));
    return false;
  }
}

/** 启动安装器并退出应用（安装脚本会等应用退出再静默安装）。 */
export async function applyUpdate(): Promise<boolean> {
  updateState.update((s) => ({ ...s, status: "applying", error: undefined }));
  try {
    await ApplyUpdate();
    return true;
  } catch (err) {
    updateState.update((s) => ({
      ...s,
      status: "error",
      error: err instanceof Error ? err.message : String(err),
    }));
    return false;
  }
}

/**
 * 订阅下载进度（Go 推 "update:progress"）。没有 Wails 运行时（vite dev）时是空订阅
 * —— 与 filters.ts 的 watchFilterPreviews 同款。
 */
export function watchUpdateProgress(): () => void {
  // globalThis 而不是 window：vite dev 的浏览器里有 window.runtime，而 vitest（node
  // 环境）里两者都没有 —— 写 window 会在测试里直接 ReferenceError。
  const wails = globalThis as unknown as {
    runtime?: { EventsOn: (name: string, fn: (...args: unknown[]) => void) => () => void };
  };
  const runtime = wails.runtime;
  if (!runtime) return () => undefined;
  return runtime.EventsOn("update:progress", (done: unknown, total: unknown) => {
    const bytes = typeof done === "number" ? done : 0;
    const size = typeof total === "number" && total > 0 ? total : undefined;
    updateState.update((s) =>
      s.status === "downloading"
        ? { ...s, progress: size === undefined ? undefined : Math.min(1, bytes / size) }
        : s,
    );
  });
}
