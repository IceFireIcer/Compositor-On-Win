/**
 * 命令面板的结果（票 54）——把三类条目（菜单栏 / 图层蒙版命令 / 工具与子模式）
 * 与当前查询词、选择合成面板要渲染的东西。
 *
 * 条目是 derived：菜单模型（MenuBar 发布）、文档、工作区、选区任何一处变了，
 * 面板里的标题、可用性、勾都会跟着变 —— 与原版"每次打开重读 NSMenu"的
 * 效果一致，而且面板开着时也不会看到过期状态。纯排序在 ./palette.ts。
 */
import { derived, get } from "svelte/store";
import { document } from "../state/document";
import { addLayerMask } from "../state/layerMask";
import { currentSelection } from "../state/selection";
import {
  activeTool,
  selectTool,
  setBrushMode,
  updateShapeSettings,
  updateToolModes,
} from "../state/tools";
import { workspace } from "../state/workspace";
import { chordLabel } from "../shortcuts/chords";
import { chordFor } from "../shortcuts/settings";
import { buildEntries, type ToolCommands } from "./entries";
import { menuModel } from "./menuModel";
import { rankEntries, type PaletteEntry } from "./palette";
import {
  closeCommandPalette,
  paletteQuery,
  paletteSelection,
} from "./session";

/** 工具条目用的真实命令面：改配过的和弦在这里立刻反映出来。 */
const toolCommands: ToolCommands = {
  selectTool,
  activeTool: () => get(activeTool),
  setToolModes: updateToolModes,
  setBrushMode,
  setShapeKind: (kind) => updateShapeSettings({ kind }),
  shortcutLabel: (definitionID) => chordLabel(chordFor(definitionID)),
};

/** 面板此刻能搜到的全部条目（顺序：菜单 → 蒙版命令 → 工具）。 */
export const paletteEntries = derived(
  [menuModel, document, workspace, currentSelection],
  ([$menuModel, $document, $workspace, $selection]) => {
    const hasDocument = !!$document.docId && $workspace.activeId === $document.docId;
    const activeLayer = hasDocument
      ? $document.layers.find((layer) => layer.id === $document.activeLayerID)
      : undefined;
    // 与图层面板的"从选区…"选项同一判据：选区存在且真的有覆盖。全零选区
    // 不算 —— 否则标题写着"从选区"，实际却建了整幅蒙版（选区取舍在
    // state/layerMask.ts 的 addLayerMask 里用的是同一条规则）。
    const covered = $selection !== null && $selection.mask.some((value) => value > 0);
    return buildEntries({
      menus: $menuModel,
      hasDocument,
      mask: {
        // 原版 canAdd：可编辑图层、单选、活动图层还没有蒙版（多选在前端还不存在，
        // 忙碌状态由 Go 拒绝并 warn）。
        canAdd: !!activeLayer && !activeLayer.maskFile,
        hasSelection: covered,
        addMask: (revealing) => void addLayerMask(revealing),
      },
      commands: toolCommands,
    });
  },
);

/** 命中的条目，最好的在前（原版 CommandPaletteModel.results）。 */
export const paletteResults = derived(
  [paletteEntries, paletteQuery],
  ([$entries, $query]) => rankEntries($entries, $query),
);

/** 当前选中的条目；结果为空或下标越界时 null（原版 selected）。 */
export const paletteSelected = derived(
  [paletteResults, paletteSelection],
  ([$results, $selection]) => $results[$selection] ?? null,
);

/** ↑ / ↓：在结果里环绕移动（原版 move(by:)）。 */
export function movePaletteSelection(step: number): void {
  const count = get(paletteResults).length;
  if (count === 0) {
    paletteSelection.set(0);
    return;
  }
  paletteSelection.update((selection) => (((selection + step) % count) + count) % count);
}

/**
 * 执行一条：禁用项什么都不做（原版只响一声，也不关面板）；否则先关面板、
 * 把焦点还给编辑器，再跑命令 —— 这样命令自己的对话框/会话不会和面板抢焦点，
 * 执行结果与从菜单点同一条完全一致（run 就是菜单项的 run）。
 */
export function runPaletteEntry(entry: PaletteEntry): void {
  if (!entry.isEnabled) return;
  closeCommandPalette();
  entry.run();
}
