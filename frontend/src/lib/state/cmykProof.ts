/**
 * CMYK 软打样的视图状态（票 72 / ADR-0008）。
 *
 * 校样是显示变换，不是文档数据：不进 manifest，撤销也不认识它。查找表由
 * Go 侧按活动 ICC 配置生成，前端只记住开关和最近一次取到的表。
 */
import { get, writable } from "svelte/store";
import { CMYKProofLookups } from "../../../wailsjs/go/bridge/Service";

export const proofColors = writable(false);
export const gamutWarning = writable(false);

export interface ProofLookups {
  dimension: number;
  profile: string;
  /** Base64 little-endian float32, 33^3 RGBA texels. */
  proof: string;
  warning: string;
  marked: string;
}

export const proofLookups = writable<ProofLookups | null>(null);
export const proofError = writable("");

/**
 * 按**当前** ICC 配置取一次查找表。没有配置就留在 proofError 里报错，不发明一张表
 * （校样是视图变换，票 72 / ADR-0008）。两个开关共用同一份表。
 */
export async function loadProofLookups(): Promise<boolean> {
  try {
    proofLookups.set(JSON.parse(await CMYKProofLookups()) as ProofLookups);
    proofError.set("");
    return true;
  } catch (err) {
    proofLookups.set(null);
    proofError.set(err instanceof Error ? err.message : String(err));
    return false;
  }
}

/**
 * 切换一个校样开关。这里而不是菜单里，是因为快捷键也要调它 —— 两处各写一份就会分叉。
 */
export async function toggleProof(which: "proof" | "gamut"): Promise<void> {
  const turningOff = which === "proof" ? get(proofColors) : get(gamutWarning);
  if (turningOff) {
    if (which === "proof") proofColors.set(false);
    else gamutWarning.set(false);
    return;
  }
  if (get(proofLookups) === null && !(await loadProofLookups())) return;
  if (which === "proof") proofColors.set(true);
  else gamutWarning.set(true);
}

export function clearProof(): void {
  proofColors.set(false);
  gamutWarning.set(false);
  proofLookups.set(null);
  proofError.set("");
}
