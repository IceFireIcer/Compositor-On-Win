import { writable } from "svelte/store";
import { Snapshot as GetSnapshot } from "../../../wailsjs/go/bridge/Workspace";
import {
  RawBeginDevelop,
  RawCancelDevelop,
  RawDevelopPreview,
  RawFinishDevelop,
} from "../../../wailsjs/go/bridge/Service";
import { applySnapshot } from "./workspace";
import { loadDocument } from "./document";

/**
 * RAW develop sheet state (ticket 41 / 64): a half-size LibRaw session opens
 * once per file; every slider move re-develops the held frame (debounced by
 * the sheet component) and the import re-opens at full size — the original
 * RawImporter.Queue's cached-filter semantics.
 *
 * 票 64 起设置里还有 Noise Reduction / Lens Correction / Highlight Recovery：
 * 起点是相机自己的读数（asShot*），Reset 回到它；supports* 为 false 的项在
 * 面板里置灰并给出原因（LibRaw 没有镜头配置文件，镜头校正恒为 false）。
 */

export interface RawDevelopSettings {
  exposure: number;
  temperature: number;
  tint: number;
  boost: number;
  noiseReduction: number;
  lensCorrection: boolean;
  highlightRecovery: boolean;
  asShotTemperature: number;
  asShotTint: number;
  asShotNoiseReduction: number;
  asShotLensCorrection: boolean;
  asShotHighlightRecovery: boolean;
  supportsNoiseReduction: boolean;
  supportsLensCorrection: boolean;
  supportsHighlightRecovery: boolean;
}

export interface RawDevelopState {
  path: string;
  name: string;
  fullWidth: number;
  fullHeight: number;
  asShot: RawDevelopSettings;
  jpegURL: string | null;
  busy: boolean;
  error: string | null;
}

export const rawDevelop = writable<RawDevelopState | null>(null);

/**
 * resetRawDevelopSettings 是原版 RawDevelopSettings.reset()：三个新参数回到
 * 相机自己的起点，其余回到中性。面板的 Reset 与 Go 侧 DevelopSettings.Reset
 * 必须给出同一份结果（预览与最终导入用同一份设置）。
 */
export function resetRawDevelopSettings(settings: RawDevelopSettings): RawDevelopSettings {
  return {
    ...settings,
    exposure: 0,
    boost: 1,
    temperature: settings.asShotTemperature,
    tint: settings.asShotTint,
    noiseReduction: settings.asShotNoiseReduction,
    lensCorrection: settings.asShotLensCorrection,
    highlightRecovery: settings.asShotHighlightRecovery,
  };
}

/**
 * isAsShot 是原版 isAsShot：Reset 按钮在已经是 asShot 时禁用。
 */
export function isAsShot(settings: RawDevelopSettings): boolean {
  return (
    settings.exposure === 0 &&
    settings.boost === 1 &&
    settings.temperature === settings.asShotTemperature &&
    settings.tint === settings.asShotTint &&
    settings.noiseReduction === settings.asShotNoiseReduction &&
    settings.lensCorrection === settings.asShotLensCorrection &&
    settings.highlightRecovery === settings.asShotHighlightRecovery
  );
}

/**
 * developSettingsPayload 是发给 Go 的那一份设置：相机不支持的参数不进请求
 * （原版 render() 只在 supports* 为真时把值写到 CIRAWFilter 上），这样置灰的
 * 控件即使残留了数值也不会悄悄生效。预览与最终导入都走它。
 */
export function developSettingsPayload(settings: RawDevelopSettings): RawDevelopSettings {
  return {
    ...settings,
    noiseReduction: settings.supportsNoiseReduction ? settings.noiseReduction : 0,
    lensCorrection: settings.supportsLensCorrection ? settings.lensCorrection : false,
    highlightRecovery: settings.supportsHighlightRecovery ? settings.highlightRecovery : false,
  };
}

// RAW items queue: the sheet runs one file at a time, the next opens as
// the current one closes (import or cancel) — the original processed
// pending imports the same way.
let rawQueue: string[] = [];

/** Opens the develop sheet for each path in turn. */
export async function queueRawDevelop(paths: string[]): Promise<void> {
  rawQueue = paths.slice();
  await openNextRawDevelop();
}

/**
 * 打开队列里的下一个。打开失败（文件损坏、不是 RAW）不能把队列卡死：丢掉这
 * 一项继续往下开，错误记在 console 里（用户会在画布上看不到那个文件）。
 */
async function openNextRawDevelop(): Promise<void> {
  while (rawQueue.length > 0) {
    const next = rawQueue.shift()!;
    try {
      await beginRawDevelop(next);
      return;
    } catch (err) {
      // 半尺寸会话可能已经建好（rb_open 成功但读相机数据失败），交给
      // RawCancelDevelop 释放，再试下一个。
      void RawCancelDevelop();
      console.warn(`RAW 显影打不开 ${next}`, err);
    }
  }
}

export async function beginRawDevelop(path: string): Promise<void> {
  const reply = JSON.parse(await RawBeginDevelop(path)) as {
    name: string;
    fullWidth: number;
    fullHeight: number;
    asShot: RawDevelopSettings;
  };
  rawDevelop.set({
    path,
    name: reply.name,
    fullWidth: reply.fullWidth,
    fullHeight: reply.fullHeight,
    asShot: reply.asShot,
    jpegURL: null,
    busy: false,
    error: null,
  });
  void updateRawDevelopPreview(reply.asShot);
}

let previewSeq = 0;

/** Re-develop the held half-size frame; the newest call wins. */
export async function updateRawDevelopPreview(settings: RawDevelopSettings): Promise<void> {
  const seq = ++previewSeq;
  rawDevelop.update((s) => (s ? { ...s, busy: true } : s));
  try {
    const reply = JSON.parse(
      await RawDevelopPreview(JSON.stringify(developSettingsPayload(settings))),
    ) as {
      url: string;
      width: number;
      height: number;
    };
    if (seq !== previewSeq) return;
    // The preview JPEG is staged on the HTTP pixel plane (§3.3); the query
    // is a cache buster for the WebView, not part of the token.
    rawDevelop.update((s) => (s ? { ...s, jpegURL: `${reply.url}?v=${seq}`, busy: false, error: null } : s));
  } catch (err) {
    if (seq !== previewSeq) return;
    rawDevelop.update((s) =>
      s ? { ...s, busy: false, error: err instanceof Error ? err.message : String(err) } : s,
    );
  }
}

/**
 * Import: full-size develop + layer commit (or a new document). busy 从按下
 * 导入一直延续到图层真正落地（RawFinishDevelop 提交完才返回），面板在这期间
 * 保持「导入中」并禁用控件。
 */
export async function finishRawDevelop(settings: RawDevelopSettings): Promise<void> {
  const state = getRaw();
  if (!state) return;
  rawDevelop.update((s) => (s ? { ...s, busy: true } : s));
  try {
    await RawFinishDevelop(state.path, JSON.stringify(developSettingsPayload(settings)));
    // The tab strip may have changed (first import creates the document).
    applySnapshot(await GetSnapshot());
    await loadDocument();
    closeRawDevelop();
  } catch (err) {
    rawDevelop.update((s) =>
      s ? { ...s, busy: false, error: err instanceof Error ? err.message : String(err) } : s,
    );
  }
}

export function closeRawDevelop(): void {
  previewSeq++;
  rawDevelop.set(null);
  void RawCancelDevelop();
  void openNextRawDevelop();
}

function getRaw(): RawDevelopState | null {
  let value: RawDevelopState | null = null;
  rawDevelop.subscribe((v) => (value = v))();
  return value;
}
