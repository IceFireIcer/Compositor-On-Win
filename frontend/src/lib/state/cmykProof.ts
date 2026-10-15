/**
 * CMYK 软打样的视图状态（票 72 / ADR-0008）。
 *
 * 校样是显示变换，不是文档数据：不进 manifest，撤销也不认识它。查找表由
 * Go 侧按活动 ICC 配置生成，前端只记住开关和最近一次取到的表。
 */
import { writable } from "svelte/store";

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

export function clearProof(): void {
  proofColors.set(false);
  gamutWarning.set(false);
  proofLookups.set(null);
  proofError.set("");
}
