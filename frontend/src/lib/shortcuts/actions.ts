/**
 * 快捷键动作表（票 47）——把定义表里的 id 接到现有 store / bridge 绑定上，
 * 与 C 系菜单的命令面（CompositorApp.swift 的菜单）对齐。
 *
 * 约定：
 *  - 只接"已有实现"的命令：没有底层 store / RPC 的条目留空，仍然出现在
 *    快捷键表单里可改键；`installShortcuts` 只消费已注册动作的按键
 *    （未注册的不抢键，理由见 dispatch.ts 的模块注释）。
 *  - 原先散在 App.svelte / MenuBar.svelte 的临时处理（撤销重做、反相、
 *    主体选择、几何表单、导出、新建画布）在这里收敛成一份，菜单与快捷键共用。
 *  - 混合模式循环走 `LayerOp`（进 Go 历史，可撤销），符合 BlendShortcutTests：
 *    ⇧= / ⇧- 在活动图层上环绕 BLEND_MODES 一步，且这一步参与撤销。
 *  - 图层命令族（⌘J/⌘G/⇧⌘G/⌥⌘G/⌘E/⌫/方向键/⇧⌫）与"新建空图层""上移下移"
 *    一样只发 op+payload 给 Go（internal/bridge/layer_commands.go），前端不
 *    复制文档语义。
 *
 * 仍未接线的条目（actions.test.ts 里逐条钉住，改动会提醒）都因为**底层命令
 * 本身还没落地**，而不是懒得接：剪贴板像素/文字通道（票 48 未做）、
 * 变换/裁剪/浮动选区的 Apply/Cancel（票 17/18 只有纯函数，画布还没接）、
 * 视图开关 Grid/Guides/Rulers/Snap/Lock（票 14 只有纯函数，画布不渲染参考线）、
 * 笔刷大小/硬度与工具模式循环（没有对应 store）、隐藏应用（macOS 专属，
 * feature-parity「明确不做」第 4 条）。这些条目仍出现在快捷键表单里可改键，
 * 且未注册动作不抢键（dispatch.ts 的 run() 只在动作已注册时消费）。
 */
import { get, writable } from "svelte/store";
import {
  ApplyFilter,
  Redo,
  SelectObjectAt,
  SelectSubject,
  Undo,
} from "../../../wailsjs/go/bridge/Service";
import {
  BLEND_MODES,
  applyDocumentSnapshot,
  document,
  layerOps,
  runLayerOp,
  type DocLayer,
  type DocumentState,
} from "../state/document";
import { beginFilter } from "../state/filters";
import { copyMerged, exportPNG, openExportJPEG } from "../state/export";
import { geometrySheet } from "../state/geometry";
import {
  canUngroupLayers,
  mergePlan,
  type LayerContext,
} from "../state/layerOps";
import { activeTool } from "../state/tools";
import { changeBrushHardness, changeBrushSize } from "../state/tools";
import { resetPaletteColors, swapPaletteColors } from "../state/palette";
import { currentSelection, selectionPayload, setSelectionFromMaskData } from "../state/selection";
import { deselect, invertSelection, selectAll } from "../state/selectionCommands";
import { selectTool } from "../state/tools";
import { showsTransformControls } from "../state/transformSession";
import { fitViewport, keyboardZoom, zoomToValue } from "../state/viewport";
import {
  activeTab,
  closeTab,
  saveProject,
  workspace,
  type DocTab,
  type PendingConversion,
} from "../state/workspace";
import { requestClose } from "../state/confirm";
import { Modifier, pressModifier, releaseModifier } from "../state/modifiers";
import {
  cancelTextSession,
  commitTextSession,
  textDefaults,
  textSession,
  updateTextStyle,
} from "../state/text";
import {
  installShortcuts,
  registerCanvasModalGuard,
  type ShortcutActions,
} from "./dispatch";
import { shortcutsSheet } from "./settings";

/** 新建画布表单的开关：从 App.svelte 的局部状态提上来，菜单项与 ⌘N 共用。 */
export const newCanvasSheet = writable(false);

/** PSD 转换报告（打开项目时待确认）：从 App.svelte 提上来，动作与表单共用。 */
export const pendingConversions = writable<PendingConversion[] | null>(null);

// ---------------------------------------------------------------------------
// 文档 / 标签上下文
// ---------------------------------------------------------------------------

function currentTab(): DocTab | null {
  return activeTab(get(workspace));
}

/** 有文档才执行（没有标签时画布命令静默，原版的菜单禁用）。 */
function withTab(fn: (tab: DocTab) => void): void {
  const tab = currentTab();
  if (tab) fn(tab);
}

// ---------------------------------------------------------------------------
// 菜单命令（与 MenuBar.svelte 共用一份）
// ---------------------------------------------------------------------------

export async function runUndoRedo(kind: "undo" | "redo"): Promise<void> {
  try {
    const raw = kind === "undo" ? await Undo() : await Redo();
    applyDocumentSnapshot(raw);
  } catch {
    // 没有可撤销/重做的操作（原版此时菜单项禁用）：静默。
  }
}

/** Select Subject / Object Selection：Go 返回的掩码进选择 store。 */
export async function runSelect(kind: "subject" | "object"): Promise<void> {
  try {
    const doc = get(document);
    const raw =
      kind === "subject"
        ? await SelectSubject()
        : await SelectObjectAt(Math.floor(doc.width / 2), Math.floor(doc.height / 2));
    const parsed = JSON.parse(raw) as {
      selection: { x: number; y: number; w: number; h: number; mask: string };
    };
    const binary = atob(parsed.selection.mask);
    const mask = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) mask[i] = binary.charCodeAt(i);
    setSelectionFromMaskData(mask, parsed.selection.w, parsed.selection.h);
  } catch (err) {
    console.warn("主体选择失败", err);
  }
}

/** ⌘I：反相像素或蒙版（Go 侧拒绝没有蒙版的图层）。 */
export async function runInvert(kind: "invert" | "invertMask"): Promise<void> {
  const doc = get(document);
  if (!doc.docId || !doc.activeLayerID) return;
  try {
    applyDocumentSnapshot(await ApplyFilter(kind, doc.activeLayerID, "{}", 0, ""));
  } catch (err) {
    console.warn("反相失败", err);
  }
}

/** 曲线 / 色阶 / 色相饱和度：图像菜单的破坏式调整会话。 */
export function openAdjustment(kind: string, label: string): void {
  void beginFilter(kind, label, true);
}

/** ⌥⌘C / ⌥⌘I：文档几何表单。 */
export function openGeometry(kind: "canvasSize" | "imageSize"): void {
  geometrySheet.set(kind);
}

function selectAllPixels(): void {
  withTab((tab) => currentSelection.set(selectAll(tab.width, tab.height)));
}

function deselectPixels(): void {
  currentSelection.update((s) => deselect(s));
}

function invertPixels(): void {
  currentSelection.update((s) => (s ? invertSelection(s) : null));
}

/** 打开项目（含 PSD 转换报告的中转）：App.svelte 的"打开"按钮共用。 */
export async function openProjectSafe(): Promise<void> {
  try {
    const { openProject } = await import("../state/workspace");
    const result = await openProject();
    if (result !== "opened" && result !== "cancelled" && result.pending) {
      pendingConversions.set(result.conversions);
    }
  } catch (err) {
    console.warn("打开项目失败", err);
  }
}

/**
 * 保存 / 另存为：Go 侧 SaveProjectDialog 每次弹原生对话框（没有"直接写回
 * 上次路径"的 Save），所以两条定义走同一实现 —— 对齐 C05 的 ⌘S / ⇧⌘S。
 */
export async function saveProjectSafe(): Promise<void> {
  try {
    await saveProject();
  } catch (err) {
    console.warn("保存项目失败", err);
  }
}

/** ⌘W：关闭当前标签；脏文档先走既有确认框（与标签页 × 同一流程）。 */
function closeActiveProject(): void {
  const tab = currentTab();
  if (tab) requestClose(tab, async (doc) => {
    await closeTab(doc.id);
  });
}

function fitCanvas(): void {
  withTab((tab) => fitViewport(tab.width, tab.height));
}

function actualPixels(): void {
  withTab((tab) => zoomToValue(1, tab.width, tab.height));
}

function zoomStep(step: number): void {
  withTab((tab) => keyboardZoom(step, tab.width, tab.height));
}

// ---------------------------------------------------------------------------
// 图层命令（走 LayerOp，进 Go 历史）
// ---------------------------------------------------------------------------

/** 活动文档 + 活动图层；没有文档/活动图层时 null。 */
function activeLayer(): { docId: string; id: string; blendMode: string; isGroup: boolean } | null {
  const doc = get(document);
  if (!doc.docId || !doc.activeLayerID) return null;
  const layer = doc.layers.find((l) => l.id === doc.activeLayerID);
  if (!layer) return null;
  return { docId: doc.docId, id: layer.id, blendMode: layer.blendMode, isGroup: layer.isGroup };
}

/**
 * ⇧= / ⇧-：活动图层的混合模式沿 BLEND_MODES 环绕一步。
 * BlendShortcutTests 的断言：列表是 Photoshop 序（Normal 之后是第一个变暗
 * 模式 Darken），往回走过 Normal 绕到最后一个模式，且整步可撤销 —— 所以
 * 必须走 LayerOp 而不是本地改 store。payload 用 Go 的字段名 `mode`
 * （internal/bridge/document.go 的 `Mode string `json:"mode"``）；注意
 * state/document.ts 里 layerOps.setBlendMode 发的是 "blendMode"，与 Go 不
 * 一致（既有拼写问题，不在本票范围）。
 */
async function cycleBlendMode(forward: boolean): Promise<void> {
  const layer = activeLayer();
  if (!layer || layer.isGroup) return; // 编组不携带混合模式（canEditAppearance）
  const found = BLEND_MODES.indexOf(layer.blendMode);
  const index = found < 0 ? 0 : found;
  const next = BLEND_MODES[(index + (forward ? 1 : BLEND_MODES.length - 1)) % BLEND_MODES.length];
  if (next === undefined || next === layer.blendMode) return;
  await runLayerOp("setBlendMode", { id: layer.id, mode: next });
}

/** ⌘[ / ⌘]：活动图层与相邻层换位（Go moveLayer 用扁平索引，to 是换位后下标）。 */
async function moveActiveLayer(offset: number): Promise<void> {
  const doc = get(document);
  if (!doc.docId || !doc.activeLayerID) return;
  const index = doc.layers.findIndex((l) => l.id === doc.activeLayerID);
  const to = index + offset;
  if (index < 0 || to < 0 || to >= doc.layers.length) return;
  await runLayerOp("moveLayer", { id: doc.activeLayerID, to });
}

/** ⇧⌘N：新建空图层（Go addLayer 自带命名与空白位图）。 */
async function newBlankLayer(): Promise<void> {
  if (!currentTab()) return;
  await runLayerOp("addLayer", {});
}

// ---------------------------------------------------------------------------
// 图层命令族（C46–C53）—— ⌘J / ⌥⌘G / ⌘G / ⇧⌘G / ⌘E / ⌫ / 方向键
//
// 命令语义（谁来合并、合并结果叫什么、剪贴到哪一层）全部由 Go 的
// planLayerOp 决定：前端只发 op + payload，文档真源只有一份。可执行性判定
// 同样以 Go 为准（无效时 Go 返回错误，前端 warn 后保持原状）。
// ---------------------------------------------------------------------------

/** 活动图层记录（没有文档/活动图层时 null）。 */
function activeLayerEntry(): DocLayer | null {
  const doc = get(document);
  if (!doc.docId || !doc.activeLayerID) return null;
  return doc.layers.find((l) => l.id === doc.activeLayerID) ?? null;
}

/**
 * ⌘J：复制活动图层。原版在图层上方插入副本并把活动层移到副本上（Go 负责
 * 新 UUID/位图拷贝/命名/槽位），前端不猜结果。
 */
async function duplicateActiveLayer(): Promise<void> {
  const layer = activeLayerEntry();
  if (!layer) return;
  await layerOps.duplicateLayer(layer.id);
}

/** ⌘G：把活动图层收进新文件夹（前端只有单选镜像；多选随画布合流）。 */
async function groupActiveLayer(): Promise<void> {
  const layer = activeLayerEntry();
  if (!layer) return;
  await layerOps.groupLayers([layer.id], layer.id);
}

/** ⇧⌘G：解散活动文件夹（不是文件夹时 Go 拒绝，静默）。 */
async function ungroupActiveLayer(): Promise<void> {
  const layer = activeLayerEntry();
  if (!layer || !layer.isGroup) return;
  await layerOps.ungroupLayer(layer.id);
}

/**
 * ⌥⌘G：切换剪贴蒙版。原版菜单项随状态改名（Create / Release），这里按
 * 活动层当前的 maskSourceID 判断方向 —— 只有一层"显示为剪贴"时才释放。
 */
async function toggleClippingMask(): Promise<void> {
  const layer = activeLayerEntry();
  if (!layer || layer.isGroup) return;
  await layerOps.setClippingMask(layer.id, layer.maskSourceID === null);
}

/** ⌘E：合并（向下 / 组 / 所选随上下文；Go 的 mergePlan 决定）。 */
async function mergeActiveLayers(): Promise<void> {
  const layer = activeLayerEntry();
  if (!layer) return;
  await layerOps.mergeLayers(layer.id);
}

/**
 * ⌫：删除。原版按右键上下文删层/蒙版/特效；面板还没有蒙版/特效行入口，
 * 这里删活动图层（Go deleteLayer 连子树一起删，并接管邻居激活）。
 */
async function deleteActiveLayerOrMask(): Promise<void> {
  const layer = activeLayerEntry();
  if (!layer) return;
  await layerOps.deleteLayer(layer.id);
}

/**
 * 方向键：按文档像素平移活动图层。原版 Nudge 1px / Shift 10px，"移动所选
 * 像素"（有选区时搬像素）随选区浮动层（票 16 的浮层）落地，暂不接管。
 */
async function nudgeActiveLayer(dx: number, dy: number): Promise<void> {
  const layer = activeLayerEntry();
  if (!layer) return;
  await layerOps.nudgeLayer(layer.id, dx, dy);
}

/**
 * ⇧⌫：内容感知填充。Go 的 contentAwareFill 需要选区（PSD 语义：填充选区），
 * 没有选区时原版也是禁用状态 —— 这里静默。
 */
async function contentAwareFill(): Promise<void> {
  const doc = get(document);
  const sel = get(currentSelection);
  if (!doc.docId || !doc.activeLayerID || !sel) return;
  try {
    applyDocumentSnapshot(
      await ApplyFilter(
        "contentAwareFill",
        doc.activeLayerID,
        "{}",
        0,
        selectionPayload(sel.mask, sel.width, sel.height),
      ),
    );
  } catch (err) {
    console.warn("内容感知填充失败", err);
  }
}

/**
 * 数字键设置不透明度 —— 原版 `typeOpacityDigit`
 * （EditorSession+Brush.swift:233-245）：单键 N → 10N%，0 单键 → 100%；
 * 第二键在 0.6 s 内到达则拼成两位数（"3" "5" → 35%），下限 1%。
 * 原版写的是工具不透明度，前端暂无笔刷不透明度 store，这里落到活动图层的
 * 不透明度（Photoshop 在移动/选择工具激活时数字键同样作用于图层）。
 */
let pendingOpacityDigit: { digit: number; time: number } | null = null;
const OPACITY_DIGIT_WINDOW_MS = 600;

async function typeOpacityDigit(digit: number): Promise<void> {
  const layer = activeLayer();
  if (!layer) return;
  const now = Date.now();
  let percent = digit === 0 ? 100 : digit * 10;
  if (pendingOpacityDigit && now - pendingOpacityDigit.time < OPACITY_DIGIT_WINDOW_MS) {
    percent = Math.max(1, pendingOpacityDigit.digit * 10 + digit);
    pendingOpacityDigit = null;
  } else {
    pendingOpacityDigit = { digit, time: now };
  }
  await runLayerOp("setOpacity", { id: layer.id, opacity: percent / 100 });
}

// ---------------------------------------------------------------------------
// 文字命令（Text Editing 组；与原版 InlineTextEditor.keyDown 同语义）
// ---------------------------------------------------------------------------

/** Swift lineHeight：leading 未显式设置时按字号 1.2 倍。 */
function currentLineHeight(): number {
  const style = get(textDefaults);
  return style.leading > 0 ? style.leading : style.fontSize * 1.2;
}

function changeTracking(delta: number): void {
  updateTextStyle({ tracking: get(textDefaults).tracking + delta });
}

/** 行距从当前行高算起（原版 `max(1, lineHeight ∓ step)`）。 */
function changeLeading(delta: number): void {
  updateTextStyle({ leading: Math.max(1, currentLineHeight() + delta) });
}

/** Esc：文字会话开着就取消文字（原版 InlineTextEditor 的 Esc），否则留给画布操作。 */
function cancelCanvasOperation(): void {
  if (get(textSession)) cancelTextSession();
}

// ---------------------------------------------------------------------------
// 动作表
// ---------------------------------------------------------------------------

export const shortcutActions: ShortcutActions = {
  // --- Menus ---
  "Menus:Undo": () => void runUndoRedo("undo"),
  "Menus:Redo": () => void runUndoRedo("redo"),
  "Menus:New Canvas": () => newCanvasSheet.set(true),
  "Menus:Open Project": () => void openProjectSafe(),
  "Menus:Save": () => void saveProjectSafe(),
  "Menus:Save As": () => void saveProjectSafe(),
  "Menus:Export PNG": () => {
    if (currentTab()) void exportPNG().catch((err) => console.warn("导出 PNG 失败", err));
  },
  "Menus:Export JPEG": () => {
    if (currentTab()) void openExportJPEG();
  },
  "Menus:Close Project": () => closeActiveProject(),
  "Menus:Fit Canvas": () => fitCanvas(),
  "Menus:Actual Pixels": () => actualPixels(),
  "Menus:Zoom In": () => zoomStep(1),
  "Menus:Zoom Out": () => zoomStep(-1),
  "Menus:Show Transform Controls": () => showsTransformControls.update((v) => !v),
  "Menus:Copy Merged": () => {
    if (currentTab()) void copyMerged().catch((err) => console.warn("拷贝合并失败", err));
  },
  "Menus:Select All": () => selectAllPixels(),
  "Menus:Deselect": () => deselectPixels(),
  "Menus:Inverse Selection": () => invertPixels(),
  "Menus:Select Subject": () => void runSelect("subject"),
  "Menus:Curves": () => openAdjustment("adjust:Curves", "曲线"),
  "Menus:Levels": () => openAdjustment("adjust:Levels", "色阶"),
  "Menus:Hue/Saturation": () => openAdjustment("adjust:Hue/Saturation", "色相/饱和度"),
  "Menus:Invert Pixels / Mask": () => void runInvert("invert"),
  "Menus:Canvas Size": () => openGeometry("canvasSize"),
  "Menus:Image Size": () => openGeometry("imageSize"),
  "Menus:Move Layer Up": () => void moveActiveLayer(1),
  "Menus:Move Layer Down": () => void moveActiveLayer(-1),
  "Menus:New Blank Layer": () => void newBlankLayer(),
  "Menus:Duplicate / Layer via Copy": () => void duplicateActiveLayer(),
  "Menus:Group Layers": () => void groupActiveLayer(),
  "Menus:Ungroup Layers": () => void ungroupActiveLayer(),
  "Menus:Toggle Clipping Mask": () => void toggleClippingMask(),
  "Menus:Merge Layers": () => void mergeActiveLayers(),
  "Menus:Content-Aware Fill": () => void contentAwareFill(),

  // --- Canvas & Layers：工具字母（原版 EditorCanvas / NativeLayerList 映射） ---
  "Canvas & Layers:Select tool": () => selectTool("idle"),
  "Canvas & Layers:Move / Transform tool": () => selectTool("move"),
  "Canvas & Layers:Hand tool": () => selectTool("hand"),
  "Canvas & Layers:Zoom tool": () => selectTool("zoom"),
  "Canvas & Layers:Brush tool": () => selectTool("brush"),
  // 原版 E = 笔刷 + 擦除模式；涂/擦模式还在画布组件里，这里先选中笔刷。
  "Canvas & Layers:Eraser": () => selectTool("brush"),
  "Canvas & Layers:Spot Healing": () => selectTool("spotHealing"),
  "Canvas & Layers:Clone Stamp": () => selectTool("cloneStamp"),
  "Canvas & Layers:Type tool": () => selectTool("type"),
  "Canvas & Layers:Gradient tool": () => selectTool("gradient"),
  "Canvas & Layers:Shape tool": () => selectTool("shape"),
  "Canvas & Layers:Eyedropper tool": () => selectTool("eyedropper"),
  "Canvas & Layers:Marquee / cycle shape": () => selectTool("marquee"),
  "Canvas & Layers:Magic": () => selectTool("wand"),
  "Canvas & Layers:Lasso / cycle mode": () => selectTool("lasso"),
  "Canvas & Layers:Blur / Smudge / Liquify": () => selectTool("blur"),
  "Canvas & Layers:Crop tool": () => selectTool("crop"),

  // --- Canvas & Layers：色板、混合模式、不透明度、临时抓手、取消 ---
  "Canvas & Layers:Swap foreground/background": () => swapPaletteColors(),
  "Canvas & Layers:Reset colors": () => resetPaletteColors(),
  "Canvas & Layers:Previous blend mode": () => void cycleBlendMode(false),
  "Canvas & Layers:Next blend mode": () => void cycleBlendMode(true),
  "Canvas & Layers:Opacity digit 0 (type two for exact %)": () => void typeOpacityDigit(0),
  "Canvas & Layers:Opacity digit 1 (type two for exact %)": () => void typeOpacityDigit(1),
  "Canvas & Layers:Opacity digit 2 (type two for exact %)": () => void typeOpacityDigit(2),
  "Canvas & Layers:Opacity digit 3 (type two for exact %)": () => void typeOpacityDigit(3),
  "Canvas & Layers:Opacity digit 4 (type two for exact %)": () => void typeOpacityDigit(4),
  "Canvas & Layers:Opacity digit 5 (type two for exact %)": () => void typeOpacityDigit(5),
  "Canvas & Layers:Opacity digit 6 (type two for exact %)": () => void typeOpacityDigit(6),
  "Canvas & Layers:Opacity digit 7 (type two for exact %)": () => void typeOpacityDigit(7),
  "Canvas & Layers:Opacity digit 8 (type two for exact %)": () => void typeOpacityDigit(8),
  "Canvas & Layers:Opacity digit 9 (type two for exact %)": () => void typeOpacityDigit(9),
  // 按住型：keydown 按下、keyup 释放（都幂等），画布自己的 Space 跟踪照常。
  // 同一个动作处理两个方向，靠事件类型分流（dispatch 把 keyup 也送给它）。
  "Canvas & Layers:Temporary Hand tool (hold)": (e) => {
    if (e.type === "keyup") releaseModifier(Modifier.Space);
    else pressModifier(Modifier.Space);
  },
  "Canvas & Layers:Cancel current canvas operation": () => cancelCanvasOperation(),
  // [ / ] 与 Shift-[ / Shift-]：原版 EditorCanvas.keyDown 只对笔刷类工具生效，
  // 步长语义在 tools.ts（changeBrushSize/changeBrushHardness）；当前设置落在
  // healSettings（污点修复画笔 J 会真的带进 Go）；画笔/橡皮的参数仍是 Go 侧
  // 固定值，等笔刷设置 RPC（票 21 尾项）落地后共用同一个 store。
  "Canvas & Layers:Decrease brush size": () => changeBrushSize(get(activeTool), false),
  "Canvas & Layers:Increase brush size": () => changeBrushSize(get(activeTool), true),
  "Canvas & Layers:Decrease brush hardness": () => changeBrushHardness(get(activeTool), false),
  "Canvas & Layers:Increase brush hardness": () => changeBrushHardness(get(activeTool), true),
  "Canvas & Layers:Delete selection / layer / effect / lasso point": () =>
    void deleteActiveLayerOrMask(),
  // 方向键（原版 EditorCanvas.keyDown：方向键 ±1px，Shift ±10px）；画布池的
  // Shift 兜底不适用于方向键（它有自己带 Shift 的定义），所以四条都要接线。
  "Canvas & Layers:Nudge Left 1 px": () => void nudgeActiveLayer(-1, 0),
  "Canvas & Layers:Nudge Right 1 px": () => void nudgeActiveLayer(1, 0),
  "Canvas & Layers:Nudge Up 1 px": () => void nudgeActiveLayer(0, -1),
  "Canvas & Layers:Nudge Down 1 px": () => void nudgeActiveLayer(0, 1),
  "Canvas & Layers:Nudge Left 10 px": () => void nudgeActiveLayer(-10, 0),
  "Canvas & Layers:Nudge Right 10 px": () => void nudgeActiveLayer(10, 0),
  "Canvas & Layers:Nudge Up 10 px": () => void nudgeActiveLayer(0, -10),
  "Canvas & Layers:Nudge Down 10 px": () => void nudgeActiveLayer(0, 10),

  // --- Text Editing（原版 InlineTextEditor.keyDown；命令 dlg 的 Enter/Esc 不在此列） ---
  "Text Editing:Finish editing text": () =>
    void commitTextSession().catch((err) => console.warn("文字提交失败", err)),
  "Text Editing:Decrease tracking": () => changeTracking(-1),
  "Text Editing:Increase tracking": () => changeTracking(1),
  "Text Editing:Decrease tracking by 10": () => changeTracking(-10),
  "Text Editing:Increase tracking by 10": () => changeTracking(10),
  "Text Editing:Decrease leading": () => changeLeading(-1),
  "Text Editing:Increase leading": () => changeLeading(1),
  "Text Editing:Decrease leading by 10": () => changeLeading(-10),
  "Text Editing:Increase leading by 10": () => changeLeading(10),
};

/** 菜单项调用动作：菜单点击没有 KeyboardEvent，动作本身不读它。 */
export function runShortcut(id: string): void {
  const action = shortcutActions[id];
  if (!action) return;
  void action({} as KeyboardEvent);
}

/** 表单打开（菜单项"键盘快捷键…"）。 */
export function openShortcutsSheet(): void {
  shortcutsSheet.set(true);
}

// ---------------------------------------------------------------------------
// 图层菜单的上下文文案（C46–C53）
//
// 菜单项的标题与可用性由**已经移植并被 51 个测试钉住**的纯函数决定
// （state/layerOps.ts 的 mergePlan / canUngroupLayers / toggleClippingMask
// 门），而不是各写一份判断 —— 菜单、快捷键、Go 三处对同一条规则的看法必须
// 一致。这里只做 document 快照 → LayerContext 的适配。
// ---------------------------------------------------------------------------

function layerMenuContext(doc: DocumentState): LayerContext | null {
  if (!doc.docId || doc.layers.length === 0) return null;
  return {
    layers: doc.layers.map((l) => ({
      id: l.id,
      name: l.name,
      isVisible: l.isVisible,
      parentID: l.parentID ?? undefined,
      isGroup: l.isGroup,
      opacity: l.opacity,
      blendMode: l.blendMode,
      maskSourceID: l.maskSourceID ?? undefined,
      adjustment: l.adjustment ?? undefined,
    })),
    activeLayerID: doc.activeLayerID,
    selectedLayerIDs: doc.activeLayerID ? [doc.activeLayerID] : [],
  };
}

/**
 * 图层菜单项的标题与可用性：合并标题随上下文（向下 / 组 / 所选），剪贴蒙版
 * 菜单项在"建立"与"释放"之间改名，与原版 NativeLayerList / ⌥⌘G 的语义一致。
 */
export interface LayerMenuState {
  mergeTitle: string;
  canMerge: boolean;
  canGroup: boolean;
  canUngroup: boolean;
  clippingTitle: string;
  canClip: boolean;
  canDuplicate: boolean;
  canDelete: boolean;
}

export function layerMenuState(doc: DocumentState): LayerMenuState {
  const ctx = layerMenuContext(doc);
  const active =
    doc.activeLayerID === null
      ? null
      : (doc.layers.find((l) => l.id === doc.activeLayerID) ?? null);
  const empty: LayerMenuState = {
    mergeTitle: "向下合并",
    canMerge: false,
    canGroup: false,
    canUngroup: false,
    clippingTitle: "创建剪贴蒙版",
    canClip: false,
    canDuplicate: false,
    canDelete: false,
  };
  if (!ctx || !active) return empty;
  const plan = mergePlan(ctx);
  const clipped = active.maskSourceID !== null;
  // 建剪贴蒙版要求下方有紧邻的同级兄弟（不能是文件夹）；释放则要求已经剪贴。
  const index = doc.layers.findIndex((l) => l.id === active.id);
  const below =
    doc.layers
      .slice(0, Math.max(0, index))
      .reverse()
      .find((l) => l.parentID === active.parentID) ?? null;
  return {
    mergeTitle:
      plan === null
        ? "向下合并"
        : plan.action === "Merge Group"
          ? "合并组"
          : plan.action === "Merge Layers"
            ? "合并图层"
            : "向下合并",
    canMerge: plan !== null,
    canGroup: true,
    canUngroup: canUngroupLayers(ctx),
    clippingTitle: clipped ? "释放剪贴蒙版" : "创建剪贴蒙版",
    canClip: clipped || (below !== null && !below.isGroup),
    canDuplicate: true,
    canDelete: true,
  };
}

/**
 * 安装全套动作 + 窗口监听；返回拆除函数（App.svelte 用）。
 * 额外注册一个画布模态守卫：新建画布表单打开时画布池让位（其余模态
 * ——滤镜、几何、导出、RAW、关闭确认、快捷键表单——在 dispatch 内部查）。
 */
export function installAppShortcuts(): () => void {
  const unregister = registerCanvasModalGuard(() => get(newCanvasSheet));
  const teardown = installShortcuts(shortcutActions);
  return () => {
    unregister();
    teardown();
  };
}
