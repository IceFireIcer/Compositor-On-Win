import { writable } from "svelte/store";
import {
  ClearRecentProjects as ClearRecentProjectsRPC,
  OpenPath as OpenPathRPC,
  OpenShellPaths as OpenShellPathsRPC,
  RecentProjects as RecentProjectsRPC,
  SyncWindowChrome as SyncWindowChromeRPC,
} from "../../../wailsjs/go/bridge/Service";
import { Snapshot as GetSnapshot } from "../../../wailsjs/go/bridge/Workspace";
import { applyDocumentSnapshot } from "./document";
import { applySnapshot, type PendingConversion, type Snapshot } from "./workspace";

/**
 * Shell 集成的前端半边（票 49）：文件菜单的"打开最近"、外壳递进来的文件、窗口
 * 标题与任务栏脏标记。逻辑与真源都在 Go（internal/bridge/shell.go），这里只做
 * store 与 Wails 事件订阅。
 */

/** 最近项目路径（最近优先；Go 侧已过滤掉移动/删除过的）。 */
export const recentProjects = writable<string[]>([]);

/** 打开一个路径的结果：进了标签、要一份转换报告，或失败。 */
export type OpenPathResult =
  | { kind: "opened" }
  | { kind: "pending"; conversions: PendingConversion[] }
  | { kind: "failed"; error: string };

/** 刷新最近项目列表（打开/保存/清空后调用）。 */
export async function refreshRecentProjects(): Promise<void> {
  try {
    const reply = JSON.parse(await RecentProjectsRPC()) as { paths?: string[] };
    recentProjects.set(Array.isArray(reply.paths) ? reply.paths : []);
  } catch (err) {
    console.warn("读取最近项目失败", err);
    recentProjects.set([]);
  }
}

/** 清空最近项目（文件菜单的"清除最近记录"）。 */
export async function clearRecentProjects(): Promise<void> {
  try {
    await ClearRecentProjectsRPC();
  } catch (err) {
    console.warn("清除最近项目失败", err);
  }
  await refreshRecentProjects();
}

/** 打开一个路径（最近项目菜单、或外壳递来的单个文件）。 */
export async function openPath(path: string): Promise<OpenPathResult> {
  try {
    const raw = await OpenPathRPC(path);
    const parsed = JSON.parse(raw) as { pending?: boolean; conversions?: PendingConversion[] };
    if (parsed.pending) {
      return { kind: "pending", conversions: parsed.conversions ?? [] };
    }
    applyDocumentSnapshot(raw);
    await refreshWorkspaceSnapshot();
    return { kind: "opened" };
  } catch (err) {
    return { kind: "failed", error: err instanceof Error ? err.message : String(err) };
  }
}

/** 外壳一次递来的一批文件的结果。 */
export interface ShellOpenResult {
  opened: string[];
  failed: { path: string; error: string }[];
  pending: boolean;
  conversions: PendingConversion[];
}

/**
 * 打开外壳排队的全部路径（启动参数 / 第二次启动转发的参数）。前端在启动时与收到
 * "shell:openPaths" 事件时调用。
 */
export async function openShellPaths(): Promise<ShellOpenResult> {
  const empty: ShellOpenResult = { opened: [], failed: [], pending: false, conversions: [] };
  try {
    const raw = await OpenShellPathsRPC();
    const reply = JSON.parse(raw) as {
      snapshot?: Snapshot;
      opened?: string[];
      failed?: { path: string; error: string }[];
      pending?: boolean;
      conversions?: PendingConversion[];
    };
    if (reply.snapshot) {
      applySnapshot(reply.snapshot); // Go 的回复就是打开后的权威快照
    } else {
      await refreshWorkspaceSnapshot();
    }
    return {
      opened: reply.opened ?? [],
      failed: reply.failed ?? [],
      pending: reply.pending === true,
      conversions: reply.conversions ?? [],
    };
  } catch (err) {
    console.warn("打开外壳递来的文件失败", err);
    return empty;
  }
}

/** 同步窗口标题与任务栏脏标记（活动标签的名字/脏标记变化时调用）。 */
export function syncWindowChrome(name: string, dirty: boolean): void {
  void SyncWindowChromeRPC(name, dirty).catch((err) => console.warn("同步窗口标题失败", err));
}

/**
 * 订阅"外壳又递了文件进来"（第二次启动的单实例转发）。没有 Wails 运行时
 * （浏览器里跑 vite dev）时是空订阅 —— 与 filters.ts 的 watchFilterPreviews 同款。
 */
export function watchShellPaths(onOpen: () => void): () => void {
  const wails = window as unknown as {
    runtime?: { EventsOn: (name: string, fn: (...args: unknown[]) => void) => () => void };
  };
  const runtime = wails.runtime;
  if (!runtime) return () => undefined;
  return runtime.EventsOn("shell:openPaths", () => onOpen());
}

/** 重新取一次标签快照（打开文件后标签栏要跟着变）。 */
async function refreshWorkspaceSnapshot(): Promise<void> {
  applySnapshot(await GetSnapshot());
}
