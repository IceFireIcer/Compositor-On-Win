import { get, writable } from "svelte/store";
import { ResolveExternalChange } from "../../../wailsjs/go/bridge/Service";
import { applyDocumentSnapshot } from "./document";
import { applySnapshot, workspace, type Snapshot } from "./workspace";

/**
 * 外部变更的前端半边（票 07 的基础设施 + 票 51 的端到端）。
 *
 * 代理或别的应用改了磁盘上的 .comp，Go 侧已经读好并**原位重载**（清撤销、保留标签），
 * 这里只把新状态贴到前端 store 上。
 *
 * 关键：**不碰视口与选区**。重载保留缩放/平移/选区在实现上就是"谁都不去动
 * viewport / currentSelection" —— 前端不调用 fit/reset，也不重建选区。撤销栈在 Go 侧
 * 被清空（重载等同重新打开文件），脏文档则先问用户。
 */

/** Go 侧 "external:changed" 的负载。 */
export interface ExternalChangedEvent {
  tabId: string;
  path: string;
  /** true = 有未保存修改，等用户决定（此时 document 为空）。 */
  pending: boolean;
  dirty: boolean;
  snapshot?: Snapshot;
  document?: string;
}

/** 待用户决定的外部变更（脏文档 + 磁盘被外部改了）。 */
export const pendingExternalChange = writable<{ tabId: string; path: string } | null>(null);

function errorMessage(err: unknown): string {
  if (err instanceof Error) return err.message;
  return typeof err === "string" ? err : String(err);
}

/**
 * 处理一条已解析的事件。测试直接调它，不必造 Wails 事件。
 * 干净的文档：贴新快照 + 新文档信封（画布靠 (docId, rev) 换新，Go 侧重载时 rev 会前进）。
 * 脏文档：只记下待决定，一个像素都不动。
 */
export function applyExternalChange(event: ExternalChangedEvent): void {
  if (event.snapshot) applySnapshot(event.snapshot);
  if (event.pending) {
    pendingExternalChange.set({ tabId: event.tabId, path: event.path });
    return;
  }
  // 只贴"当前正在看的那个标签"的文档：前端只维护活动标签的文档镜像，后台标签重载
  // 只更新标签列表（切过去时前端会自己重新拉）。Go 侧也不会为后台标签带文档过来，
  // 这里再挡一道，免得将来 Go 侧改了行为把当前画布覆盖掉。
  if (event.document && get(workspace).activeId === event.tabId) {
    applyDocumentSnapshot(event.document);
  }
}

/** 用户对外部变更的回答：accept=true 载入磁盘版本，false 保留本地修改。 */
export async function resolveExternalChange(accept: boolean): Promise<void> {
  const pending = get(pendingExternalChange);
  if (!pending) return;
  pendingExternalChange.set(null);
  try {
    const reply = await ResolveExternalChange(pending.tabId, accept);
    // 只有"用户正在看的那个标签"才贴文档：前端只有一份文档镜像，而后台标签的外部改动
    // 也会走到这里（待决定状态是全局一个槽）。Go 侧同样只在活动标签时回文档，这里再挡一道。
    if (accept && get(workspace).activeId === pending.tabId) {
      applyDocumentSnapshot(reply);
    }
  } catch (err) {
    // A transient package-read or document-busy failure remains actionable:
    // restore the exact prompt that was optimistically hidden before the RPC.
    // A stale answer is different: the backend already resolved that pending
    // state, so resurfacing it would ask the user to decide twice.
    if (!errorMessage(err).includes("EXTERNAL_CHANGE_NOT_PENDING")) {
      pendingExternalChange.set(pending);
    }
    console.warn("处理外部变更失败", err);
  }
}

/** 解析事件负载；坏负载直接忽略（事件来自 Go，坏了也不该让前端崩）。 */
export function parseExternalEvent(payload: unknown): ExternalChangedEvent | null {
  if (typeof payload !== "string") return null;
  try {
    const parsed = JSON.parse(payload) as ExternalChangedEvent;
    if (!parsed || typeof parsed.tabId !== "string") return null;
    return parsed;
  } catch {
    return null;
  }
}

/**
 * 订阅 "external:changed"。没有 Wails 运行时（vite dev / vitest）时是空订阅 ——
 * 与 filters.ts、update.ts 同款写法（globalThis 而不是 window）。
 */
export function watchExternalChanges(): () => void {
  const runtime = (
    globalThis as unknown as {
      runtime?: { EventsOn: (name: string, fn: (...args: unknown[]) => void) => () => void };
    }
  ).runtime;
  if (!runtime) return () => undefined;
  return runtime.EventsOn("external:changed", (payload: unknown) => {
    const event = parseExternalEvent(payload);
    if (event) applyExternalChange(event);
  });
}
