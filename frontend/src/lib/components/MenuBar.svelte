<script lang="ts">
  import {
    ADJUSTMENT_LAYER_KINDS,
    FILTER_KINDS,
    ADJUSTMENT_KINDS,
    beginFilter,
    filterSession,
  } from "../state/filters";
  import { document, layerOps } from "../state/document";
  import { importImages, saveProjectAs } from "../state/workspace";
  import { copyMerged, exportPNG, openExportAs, openExportJPEG } from "../state/export";
  import { geometrySheet } from "../state/geometry";
  import {
    clearAllGuides,
    clearSelectionPixels,
    flipCanvas,
    flipLayer,
    layerMenuState,
    openGeometry,
    openProjectSafe,
    openShortcutsSheet,
    pendingConversions,
    runInvert,
    runSelect,
    runShortcut,
    saveProjectSafe,
    toggleLockGuides,
    toggleTransformControls,
    toggleView,
  } from "../shortcuts/actions";
  import {
    clearRecentProjects,
    openPath,
    recentProjects,
    refreshRecentProjects,
  } from "../state/shell";
  import { checkForUpdates } from "../state/update";
  import { gamutWarning, proofColors, proofError, proofLookups } from "../state/cmykProof";
  import { depthMenuItems, layerDepth, runDepthCommand } from "../state/layerDepth";
  import { CMYKProfiles, LoadCMYKProfile, SetCMYKProfile } from "../../../wailsjs/go/bridge/Service";
  import { CMYKProofLookups } from "../../../wailsjs/go/bridge/Service";
  import { chordFor } from "../shortcuts/settings";
  import { chordLabel } from "../shortcuts/chords";
  import { registerCanvasModalGuard } from "../shortcuts/dispatch";
  import { textSession } from "../state/text";
  import { currentSelection } from "../state/selection";
  import { loadLayerSelection, openSelectionEdit, selectionEdit, type SelectionEditKind } from "../state/selectionEdit";
  import {
    gridSettingsOpen,
    lockGuides,
    showGrid,
    showGuides,
    showRulers,
    showsNavigator,
    showsPixelGrid,
    snapEnabled,
    snapToggles,
  } from "../state/view";
  import { showsTransformControls } from "../state/transform";
  import { historyNames, redoMenuLabel, undoMenuLabel } from "../state/historyNames";
  import { SEARCH_COMMANDS_LABEL, menuModel, type MenuModel } from "../commands/menuModel";
  import { toggleCommandPalette } from "../commands/session";
  // 票 55 / 56 的菜单入口：菜单栏是这两票唯一共用的文件，由 Lead 统一接线。
  import { canvasOnly, canvasOnlyAllowed, toggleCanvasOnly } from "../state/canvasOnly";
  import { rotateCanvas } from "../state/canvasRotate";
  import { lastFilter, lastFilterLabel, repeatAllowed } from "../state/lastFilter";

  /**
   * The menu bar (CompositorApp.swift's menus): 选择 / 文件 / 编辑 / 图像 /
   * 滤镜. 编辑 is ticket 47's addition — 撤销 / 重做 / 键盘快捷键…, the last
   * opening the remapping sheet (票 47 只有菜单入口，快捷键表单本身不再占一条
   * 组合键). 图像 carries the destructive adjustment entries and 反相/反相蒙版;
   * 滤镜 the ten FilterKind items.
   *
   * Hints now come from the shortcut table (`hintFor` → chordFor + chordLabel),
   * so a remap in 键盘快捷键… is reflected here immediately. Items whose command
   * lives in the action registry route through `runShortcut`, so hint and
   * behavior cannot drift apart.
   */

  // 菜单的形状在 ../commands/menuModel.ts（命令搜索面板搜的是同一份）。
  type Menu = MenuModel;

  let open = $state<string | null>(null);
  const hasLayer = $derived(!!$document.activeLayerID);
  const hasPixelLayer = $derived(
    !!$document.activeLayerID &&
      $document.layers.some((layer) => layer.id === $document.activeLayerID && !layer.isGroup),
  );
  const dialogOpen = $derived($filterSession !== null || $selectionEdit !== null || $gridSettingsOpen);
  const hasSelection = $derived($currentSelection?.active === true);
  const hasMask = $derived(!!$document.layers.find((layer) => layer.id === $document.activeLayerID)?.maskFile);
  // 图层菜单的标题/可用性来自被测试钉住的纯函数（state/layerOps.ts），
  // 所以菜单、快捷键、Go 三处对"⌘E 现在合并什么"的看法一致。
  const layerMenu = $derived(layerMenuState($document));
  // 文字会话：剪贴板命令让位给行内文本框（菜单路径也要挡）。
  const editingText = $derived($textSession !== null);
  // 纯画布的可进入性：与 F 的动作共用 canvasOnlyAllowed 一份规则（票 55）。
  const canCanvasOnly = $derived(canvasOnlyAllowed(!!$document.docId, editingText));
  let cmykProfiles = $state<Array<{ name: string; chosen: boolean }>>([]);
  async function refreshProfiles(): Promise<void> {
    try {
      cmykProfiles = JSON.parse(await CMYKProfiles()) as Array<{ name: string; chosen: boolean }>;
    } catch {
      cmykProfiles = [];
    }
  }

  // 校样是视图变换（票 72）：查找表按**当前**配置生成，没有配置就留在菜单报错，
  // 不发明一张表。两个开关共用同一份表。
  async function loadProofLookups(): Promise<boolean> {
    try {
      proofLookups.set(JSON.parse(await CMYKProofLookups()));
      proofError.set("");
      return true;
    } catch (err) {
      proofLookups.set(null);
      proofError.set(err instanceof Error ? err.message : String(err));
      return false;
    }
  }

  async function toggleProof(which: "proof" | "gamut"): Promise<void> {
    const turningOff = which === "proof" ? $proofColors : $gamutWarning;
    if (turningOff) {
      if (which === "proof") proofColors.set(false);
      else gamutWarning.set(false);
      return;
    }
    if (!$proofLookups && !(await loadProofLookups())) return;
    if (which === "proof") proofColors.set(true);
    else gamutWarning.set(true);
  }

  /**
   * 配置变了就得重新取表。只把表清空是不够的：开关还亮着，而画布的 setProof 在
   * lookup 为 null 时提前返回，会继续用**上一张**3D 纹理 —— 菜单勾了新配置、画布
   * 却还按旧的校样。取不到新表就把两个开关一起关掉，宁可直通显示。
   */
  async function reproof(): Promise<void> {
    const wasOn = $proofColors || $gamutWarning;
    proofLookups.set(null);
    if (!wasOn) return;
    if (!(await loadProofLookups())) {
      proofColors.set(false);
      gamutWarning.set(false);
    }
  }
  // View 菜单的五个开关状态（勾）+"清除参考线"的可用性。动作与快捷键共用
  // toggleView，所以这里的勾永远反映真实状态，不会和画布/快捷键分叉。
  //
  // 必须用 `$store` 前缀逐个读：旧的 `viewToggleState()` 内部是 `get(store)` 与
  // 普通类字段（`guidesState.guides.length`），Svelte 5 不会把它们登记成
  // $derived 的依赖 —— 结果是一份"挂载时算一次、之后永不更新"的冻结核对表：
  // 画布上明明拖出了参考线，"清除参考线"仍然是灰的（真机验收实测）。
  //
  // hasGuides 读**文档真源**（Go 快照里的 `guides`）而不是渲染用的
  // `visibleGuides`：后者是"含拖动中那条"的视图列表，将来任何一条漏掉
  // refreshVisibleGuides 的新改动都会让菜单重新冻住 —— 正是这一轮修掉的
  // 那类 bug。
  const viewState = $derived({
    grid: $showGrid,
    pixelGrid: $showsPixelGrid,
    guides: $showGuides,
    rulers: $showRulers,
    snap: $snapEnabled,
    snapTo: $snapToggles,
    lockGuides: $lockGuides,
    transformControls: $showsTransformControls,
    hasGuides: Array.isArray($document.guides) && $document.guides.length > 0,
  });

  // 最近项目（票 49）：文件菜单里逐条列出，最后一条是"清除最近记录"。
  // 列表由 Go 过滤（移动/删除过的项目不出现），开菜单时重取一次。
  const recentFiles = $derived($recentProjects);
  $effect(() => {
    if (open === "文件") void refreshRecentProjects();
    if (open === "视图") void refreshProfiles();
  });

  /** 文件名（菜单里只显示文件名，完整路径放 title/hint）。 */
  function baseName(path: string): string {
    const parts = path.split(/[\\/]/);
    return parts[parts.length - 1] || path;
  }

  /** 打开最近项目：进了标签就够，要转换报告就把表单顶起来。 */
  async function openRecentPath(path: string): Promise<void> {
    const result = await openPath(path);
    if (result.kind === "pending") {
      pendingConversions.set(result.conversions);
    } else if (result.kind === "failed") {
      window.alert(`打开失败\n\n${result.error}`);
    }
    await refreshRecentProjects();
  }

  /** Current chord label for a definition id — the menu's live hint. */
  function hintFor(id: string): string {
    return chordLabel(chordFor(id));
  }

  // 反相/反相蒙版: 反相 rides ⌘I (single definition), the mask variant has no
  // key of its own in the original, so it stays hint-less.
  const ADJUSTMENT_HINT_IDS: Record<string, string> = {
    "adjust:Curves": "Menus:Curves",
    "adjust:Levels": "Menus:Levels",
    "adjust:Hue/Saturation": "Menus:Hue/Saturation",
  };

  function close(): void {
    open = null;
  }

  async function runInvertMenu(kind: "invert" | "invertMask"): Promise<void> {
    await runInvert(kind);
    close();
  }

  async function runSelectMenu(kind: "subject" | "object"): Promise<void> {
    await runSelect(kind);
    close();
  }

  function editSelection(kind: SelectionEditKind): void {
    close();
    openSelectionEdit(kind);
  }

  async function loadSelection(kind: "pixels" | "mask"): Promise<void> {
    close();
    try { await loadLayerSelection(kind); }
    catch (error) { window.alert(`载入选区失败：${error instanceof Error ? error.message : String(error)}`); }
  }

  /** Image adjustments are layers, not destructive pixel filters. Create the
   * layer first, then open the same editor in metadata-only mode against the
   * newly active layer so cancel leaves the new layer at its identity defaults.
   */
  async function runAdjustmentMenu(
    kind: string,
    label: string,
    provisional = true,
  ): Promise<void> {
    close();
    const rawKind = kind.startsWith("adjust:") ? kind.slice("adjust:".length) : kind;
    if (!(await layerOps.addAdjustment(rawKind, provisional))) {
      window.alert(`无法创建${label}调整图层`);
      return;
    }
    // Invert has no parameters to edit: its adjustment layer is immediately
    // usable and must not leave an unowned provisional creation marker.
    if (!provisional) return;
    const layerID = $document.activeLayerID;
    const layer = layerID ? $document.layers.find((candidate) => candidate.id === layerID) : undefined;
    if (!layerID || !layer?.adjustment) {
      window.alert(`${label}调整图层创建成功，但无法打开编辑器`);
      return;
    }
    await beginFilter(kind, label, false, layerID, layer.adjustment);
  }

  function runAndClose(id: string): void {
    runShortcut(id);
    close();
  }

  // Non-fatal bridge failures log; the flows surface user errors themselves.
  async function safe(p: Promise<void>, what: string): Promise<void> {
    try {
      await p;
    } catch (err) {
      console.warn(what, err);
    }
    close();
  }

  // 导入图像…: the batch lands as one undo step; per-file failures gather
  // into one alert exactly as the original's importError did. RAW items
  // queue into the develop sheet (one at a time) after the commit.
  async function runImportImages(): Promise<void> {
    try {
      const result = await importImages();
      if (result && result.failures.length > 0) {
        window.alert(`导入未能完成\n\n${result.failures.join("\n\n")}`);
      }
    } catch (err) {
      console.warn("导入图像失败", err);
      window.alert(`导入未能完成：${err instanceof Error ? err.message : String(err)}`);
    }
    close();
  }

  const menus = $derived<Menu[]>([
    {
      label: "选择",
      items: [
        { label: "全选", hint: hintFor("Menus:Select All"), disabled: !hasLayer || dialogOpen, run: () => runAndClose("Menus:Select All") },
        { label: "取消选择", hint: hintFor("Menus:Deselect"), disabled: $currentSelection === null || dialogOpen, run: () => runAndClose("Menus:Deselect") },
        { label: "反向选区", hint: hintFor("Menus:Inverse Selection"), disabled: $currentSelection === null || dialogOpen, run: () => runAndClose("Menus:Inverse Selection") },
        { label: "载入图层像素", disabled: !hasLayer || dialogOpen, run: () => void loadSelection("pixels") },
        { label: "载入蒙版黑色区域", disabled: !hasMask || dialogOpen, run: () => void loadSelection("mask") },
        { label: "色彩范围…", disabled: !hasLayer || dialogOpen, run: () => editSelection("colorRange") },
        { label: "扩展选区…", disabled: !hasSelection || dialogOpen, run: () => editSelection("expand") },
        { label: "收缩选区…", disabled: !hasSelection || dialogOpen, run: () => editSelection("contract") },
        { label: "羽化选区…", disabled: !hasSelection || dialogOpen, run: () => editSelection("feather") },
        { label: "变换选区…", disabled: !hasSelection || dialogOpen, run: () => editSelection("transform") },
        {
          label: "选择主体",
          hint: hintFor("Menus:Select Subject"),
          disabled: !hasLayer || dialogOpen,
          run: () => void runSelectMenu("subject"),
        },
        {
          label: "对象选择",
          disabled: !hasLayer || dialogOpen,
          run: () => void runSelectMenu("object"),
        },
      ],
    },
    {
      label: "文件",
      items: [
        {
          label: "新建画布…",
          hint: hintFor("Menus:New Canvas"),
          run: () => runAndClose("Menus:New Canvas"),
        },
        {
          label: "打开项目…",
          hint: hintFor("Menus:Open Project"),
          run: () => {
            close();
            void openProjectSafe();
          },
        },
        ...recentFiles.map((path) => ({
          label: `打开最近：${baseName(path)}`,
          placeholder: path,
          disabled: dialogOpen,
          run: () => {
            close();
            void openRecentPath(path);
          },
        })),
        ...(recentFiles.length > 0
          ? [
              {
                label: "清除最近记录",
                disabled: dialogOpen,
                run: () => {
                  close();
                  void clearRecentProjects();
                },
              },
            ]
          : []),
        {
          label: "保存",
          hint: hintFor("Menus:Save"),
          disabled: dialogOpen,
          run: () => {
            close();
            void saveProjectSafe();
          },
        },
        { label: "导入图像…", disabled: dialogOpen, run: () => void runImportImages() },
        { label: "另存为…", disabled: !hasLayer || dialogOpen, run: () => { close(); void saveProjectAs().catch((error) => window.alert(`另存为失败：${error instanceof Error ? error.message : String(error)}`)); } },
        {
          label: "导出 PNG…",
          hint: hintFor("Menus:Export PNG"),
          disabled: !hasLayer || dialogOpen,
          run: () => void safe(exportPNG(), "导出 PNG 失败"),
        },
        {
          label: "导出 JPEG…",
          hint: hintFor("Menus:Export JPEG"),
          disabled: !hasLayer || dialogOpen,
          run: () => openExportJPEG(),
        },
        // 票 59：统一的导出窗口（PNG/JPEG/TIFF/单页 PDF、等比缩放、质量、预览）。
        // 原版放在同一组的 PNG/JPEG 之后（⌥⇧⌘W → Ctrl+Alt+Shift+W）。
        {
          label: "导出为…",
          hint: hintFor("Menus:Export As"),
          disabled: !hasLayer || dialogOpen,
          run: () => {
            close();
            openExportAs();
          },
        },
        {
          label: "拷贝合并",
          hint: hintFor("Menus:Copy Merged"),
          disabled: !hasLayer || dialogOpen,
          run: () => void safe(copyMerged(), "拷贝合并失败"),
        },
        {
          label: "关闭项目",
          hint: hintFor("Menus:Close Project"),
          run: () => runAndClose("Menus:Close Project"),
        },
      ],
    },
    {
      label: "编辑",
      items: [
        // 标题带操作名（原版 "Undo <操作名>"）：名字由 Go 给，空栈时退回裸标题。
        { label: undoMenuLabel($historyNames), hint: hintFor("Menus:Undo"), run: () => runAndClose("Menus:Undo") },
        { label: redoMenuLabel($historyNames), hint: hintFor("Menus:Redo"), run: () => runAndClose("Menus:Redo") },
        // 文字会话开着时剪贴板归文本框（原版按 first responder 分流）。
        {
          label: "剪切",
          hint: hintFor("Menus:Cut"),
          disabled: !hasLayer || dialogOpen || editingText,
          run: () => runAndClose("Menus:Cut"),
        },
        {
          label: "拷贝",
          hint: hintFor("Menus:Copy"),
          disabled: !hasLayer || dialogOpen || editingText,
          run: () => runAndClose("Menus:Copy"),
        },
        {
          label: "粘贴",
          hint: hintFor("Menus:Paste"),
          disabled: !hasLayer || dialogOpen || editingText,
          run: () => runAndClose("Menus:Paste"),
        },
        {
          label: "通过拷贝的图层",
          hint: hintFor("Menus:Duplicate / Layer via Copy"),
          disabled: !hasLayer || dialogOpen,
          run: () => runAndClose("Menus:Duplicate / Layer via Copy"),
        },
        // 填充 / 清除（票 23）：内核在 render/stampfill.go，RPC 是 FillSelection。
        {
          label: "填充前景色",
          hint: hintFor("Menus:Fill with Foreground"),
          disabled: !hasLayer || dialogOpen,
          run: () => runAndClose("Menus:Fill with Foreground"),
        },
        {
          label: "填充背景色",
          hint: hintFor("Menus:Fill with Background"),
          disabled: !hasLayer || dialogOpen,
          run: () => runAndClose("Menus:Fill with Background"),
        },
        {
          label: "清除选区像素",
          disabled: !hasLayer || !hasSelection || dialogOpen,
          run: () => {
            close();
            void clearSelectionPixels();
          },
        },
        {
          label: "键盘快捷键…",
          disabled: dialogOpen,
          run: () => {
            openShortcutsSheet();
            close();
          },
        },
      ],
    },
    {
      label: "图像",
      items: [
        ...ADJUSTMENT_KINDS.map((k) => {
          const definition = ADJUSTMENT_HINT_IDS[k.id];
          return {
            label: k.label,
            hint: definition ? hintFor(definition) : k.placeholder,
            disabled: !hasLayer || dialogOpen,
            run: () => void runAdjustmentMenu(k.id, k.label.replace(/…$/, "")),
          };
        }),
        {
          label: "反相",
          hint: hintFor("Menus:Invert Pixels / Mask"),
          disabled: !hasLayer || dialogOpen,
          run: () => void runInvertMenu("invert"),
        },
        {
          label: "反相蒙版",
          disabled: !hasLayer || dialogOpen,
          run: () => void runInvertMenu("invertMask"),
        },
        {
          label: "画布大小…",
          hint: hintFor("Menus:Canvas Size"),
          disabled: !hasLayer || dialogOpen,
          run: () => {
            openGeometry("canvasSize");
            close();
          },
        },
        {
          label: "图像大小…",
          hint: hintFor("Menus:Image Size"),
          disabled: !hasLayer || dialogOpen,
          run: () => {
            openGeometry("imageSize");
            close();
          },
        },
        {
          label: "修剪…",
          disabled: !hasLayer || dialogOpen,
          run: () => {
            geometrySheet.set("trim");
            close();
          },
        },
        // 票 56：画布旋转 90°（原版 Image 菜单，无快捷键）。顺序与原版一致：
        // 旋转在翻转之前。
        {
          label: "顺时针旋转 90°",
          disabled: !hasLayer || dialogOpen,
          run: () => {
            close();
            void rotateCanvas("clockwise");
          },
        },
        {
          label: "逆时针旋转 90°",
          disabled: !hasLayer || dialogOpen,
          run: () => {
            close();
            void rotateCanvas("counterclockwise");
          },
        },
        {
          label: "水平翻转画布",
          disabled: !hasLayer || dialogOpen,
          run: () => {
            close();
            void flipCanvas("horizontal");
          },
        },
        {
          label: "垂直翻转画布",
          disabled: !hasLayer || dialogOpen,
          run: () => {
            close();
            void flipCanvas("vertical");
          },
        },
      ],
    },
    {
      label: "图层",
      items: [
        ...depthMenuItems($layerDepth).map((item) => ({
          label: item.label,
          disabled: dialogOpen || !$document.docId,
          run: () => {
            close();
            const tab = $document.docId;
            if (tab) void runDepthCommand(tab, item.command);
          },
        })),
        {
          label: "通过拷贝的图层",
          hint: hintFor("Menus:Duplicate / Layer via Copy"),
          disabled: !hasLayer || dialogOpen,
          run: () => runAndClose("Menus:Duplicate / Layer via Copy"),
        },
        {
          label: "编组",
          hint: hintFor("Menus:Group Layers"),
          disabled: !hasLayer || dialogOpen,
          run: () => runAndClose("Menus:Group Layers"),
        },
        {
          label: "取消编组",
          hint: hintFor("Menus:Ungroup Layers"),
          disabled: !layerMenu.canUngroup || dialogOpen,
          run: () => runAndClose("Menus:Ungroup Layers"),
        },
        {
          label: layerMenu.clippingTitle,
          hint: hintFor("Menus:Toggle Clipping Mask"),
          disabled: !layerMenu.canClip || dialogOpen,
          run: () => runAndClose("Menus:Toggle Clipping Mask"),
        },
        {
          label: layerMenu.mergeTitle,
          hint: hintFor("Menus:Merge Layers"),
          disabled: !layerMenu.canMerge || dialogOpen,
          run: () => runAndClose("Menus:Merge Layers"),
        },
        {
          label: "水平翻转图层",
          disabled: !hasPixelLayer || dialogOpen,
          run: () => {
            close();
            void flipLayer("x");
          },
        },
        {
          label: "垂直翻转图层",
          disabled: !hasPixelLayer || dialogOpen,
          run: () => {
            close();
            void flipLayer("y");
          },
        },
        {
          label: "新建空图层",
          hint: hintFor("Menus:New Blank Layer"),
          disabled: !hasLayer || dialogOpen,
          run: () => runAndClose("Menus:New Blank Layer"),
        },
        {
          label: "上移一层",
          hint: hintFor("Menus:Move Layer Up"),
          disabled: !hasLayer || dialogOpen,
          run: () => runAndClose("Menus:Move Layer Up"),
        },
        {
          label: "下移一层",
          hint: hintFor("Menus:Move Layer Down"),
          disabled: !hasLayer || dialogOpen,
          run: () => runAndClose("Menus:Move Layer Down"),
        },
        {
          label: "删除图层",
          hint: hintFor("Canvas & Layers:Delete selection / layer / effect / lasso point"),
          disabled: !hasLayer || dialogOpen,
          run: () => runAndClose("Canvas & Layers:Delete selection / layer / effect / lasso point"),
        },
        ...ADJUSTMENT_LAYER_KINDS.map((k) => ({
          label: `新建调整图层 · ${k.label.replace(/…$/, "")}`,
          disabled: !hasLayer || dialogOpen,
          run: () => void runAdjustmentMenu(
            k.id,
            k.label.replace(/…$/, ""),
            k.id !== "adjust:Invert",
          ),
        })),
      ],
    },
    {
      label: "帮助",
      items: [
        {
          // C09：检查更新（票 50）。没有配置更新源的构建会说清楚，而不是假装检查。
          label: "检查更新…",
          disabled: dialogOpen,
          run: () => {
            close();
            void checkForUpdates();
          },
        },
      ],
    },
    {
      label: "视图",
      items: [
        // 票 54：命令搜索面板。原版把这一条放在 View 组（CommandGroup(after: .toolbar)，
        // ⌘F）；Windows 上是 Ctrl+F，纯画布模式的 F 留给票 55。
        {
          label: SEARCH_COMMANDS_LABEL,
          hint: hintFor("Menus:Search Commands"),
          run: () => {
            close();
            toggleCommandPalette();
          },
        },
        // 票 55：纯画布（原版 View 组的 Toggle Fullscreen，F；Esc 也能离开）。
        // 已经在纯画布里时永远可点（退出不受"可进入性"限制），对话框开着时让位
        // ——与 F 走分发层时被 canvasModalOpen 静默的行为一致。
        {
          label: "纯画布",
          hint: hintFor("Canvas & Layers:Toggle Fullscreen: the canvas alone on black (Esc also leaves)"),
          checked: $canvasOnly,
          disabled: dialogOpen || (!$canvasOnly && !canCanvasOnly),
          run: () => {
            close();
            toggleCanvasOnly();
          },
        },
        {
          label: "适应窗口",
          hint: hintFor("Menus:Fit Canvas"),
          disabled: !hasLayer,
          run: () => runAndClose("Menus:Fit Canvas"),
        },
        {
          label: "实际像素",
          hint: hintFor("Menus:Actual Pixels"),
          disabled: !hasLayer,
          run: () => runAndClose("Menus:Actual Pixels"),
        },
        {
          label: "放大",
          hint: hintFor("Menus:Zoom In"),
          disabled: !hasLayer,
          run: () => runAndClose("Menus:Zoom In"),
        },
        {
          label: "缩小",
          hint: hintFor("Menus:Zoom Out"),
          disabled: !hasLayer,
          run: () => runAndClose("Menus:Zoom Out"),
        },
        // 票 61：导航器（原版 View ▸ Navigator (300% and above)）。默认关，偏好
        // 持久化在 state/view.ts 的 showsNavigator 里；组件自己判断 300% 与纯画布。
        {
          label: "导航器（300% 及以上）",
          checked: $showsNavigator,
          run: () => {
            showsNavigator.update((value) => !value);
            close();
          },
        },
        // --- 视图开关（票 14 / 47）：原先这五条只有快捷键、没有菜单入口，
        // 用户除了背键位没有别的办法打开参考线/网格/标尺。勾来自上面的
        // viewState（$store 响应式读），与实际状态同源（菜单与快捷键共用 toggleView）。
        {
          label: $proofError ? `校样颜色（${$proofError}）` : "校样颜色",
          checked: $proofColors,
          disabled: dialogOpen,
          run: () => {
            close();
            void toggleProof("proof");
          },
        },
        ...cmykProfiles.map((profile) => ({
          label: profile.chosen ? `✓ ${profile.name}` : profile.name,
          disabled: dialogOpen,
          run: () => {
            close();
            void SetCMYKProfile(profile.name)
              .then(() => reproof())
              .then(() => refreshProfiles());
          },
        })),
        {
          label: "色域警告",
          checked: $gamutWarning,
          disabled: dialogOpen,
          run: () => {
            close();
            void toggleProof("gamut");
          },
        },
        {
          label: "载入 ICC 配置…",
          disabled: dialogOpen,
          run: () => {
            close();
            const input = globalThis.document.createElement("input");
            input.type = "file";
            input.accept = ".icc,.icm";
            input.onchange = () => {
              const file = input.files?.[0];
              const path = file && "path" in file ? String(file.path) : "";
              if (!path) {
                proofError.set("请从桌面版选择一个 ICC 文件");
                return;
              }
              void LoadCMYKProfile(path).then(() => reproof(), (err: unknown) => {
                proofError.set(err instanceof Error ? err.message : String(err));
              });
            };
            input.click();
          },
        },
        {
          label: "显示网格",
          hint: hintFor("Menus:Show Grid"),
          checked: viewState.grid,
          run: () => {
            toggleView("grid");
            close();
          },
        },
        {
          label: "网格设置…",
          disabled: dialogOpen,
          run: () => gridSettingsOpen.set(true),
        },
        {
          label: "显示像素网格",
          checked: viewState.pixelGrid,
          run: () => showsPixelGrid.update((value) => !value),
        },
        {
          label: "显示参考线",
          hint: hintFor("Menus:Show Guides"),
          checked: viewState.guides,
          run: () => {
            toggleView("guides");
            close();
          },
        },
        {
          label: "显示标尺",
          hint: hintFor("Menus:Show Rulers"),
          checked: viewState.rulers,
          run: () => {
            toggleView("rulers");
            close();
          },
        },
        {
          label: "对齐",
          hint: hintFor("Menus:Snap"),
          checked: viewState.snap,
          run: () => {
            toggleView("snap");
            close();
          },
        },
        ...([
          ["guides", "对齐到参考线"],
          ["grid", "对齐到网格"],
          ["layerBounds", "对齐到图层"],
          ["docBounds", "对齐到文档边界"],
        ] as const).map(([target, label]) => ({
          label,
          checked: viewState.snapTo[target],
          run: () => snapToggles.update((current) => ({ ...current, [target]: !current[target] })),
        })),
        {
          label: "锁定参考线",
          hint: hintFor("Menus:Lock Guides"),
          checked: viewState.lockGuides,
          run: () => {
            toggleLockGuides();
            close();
          },
        },
        {
          label: "清除参考线",
          disabled: !viewState.hasGuides,
          run: () => {
            clearAllGuides();
            close();
          },
        },
        // ⌘H 是"控件可见性"开关（原版 ShowTransformControls），不是"开始变换"。
        // 开始变换是 ⌘T，两条语义分开。
        {
          label: "显示变换控件",
          hint: hintFor("Menus:Show Transform Controls"),
          checked: viewState.transformControls,
          run: () => {
            toggleTransformControls();
            close();
          },
        },
        {
          label: "变换图层 / 选区",
          hint: hintFor("Menus:Transform Layer / Selection"),
          disabled: !(hasLayer || hasSelection) || dialogOpen,
          run: () => runAndClose("Menus:Transform Layer / Selection"),
        },
      ],
    },
    {
      label: "滤镜",
      items: [
        // 票 58：重做上一个滤镜（原版 Filter ▸ Last Filter，⌃⌘F → Windows Ctrl+Alt+F）。
        // 标题跟着上次滤镜改名；可用性判据与快捷键动作共用 repeatAllowed 一份规则。
        {
          label: lastFilterLabel($lastFilter),
          hint: hintFor("Menus:Last Filter"),
          disabled: dialogOpen || !repeatAllowed($lastFilter, hasLayer, $filterSession !== null),
          run: () => runAndClose("Menus:Last Filter"),
        },
        ...FILTER_KINDS.map((k) => ({
          label: k.label,
          disabled: !hasLayer || dialogOpen,
          placeholder: k.placeholder,
          run: () => {
            void beginFilter(k.id, k.label.replace(/…$/, ""), false);
            close();
          },
        })),
      ],
    },
  ]);

  function onDocClick(event: MouseEvent): void {
    if (open && !(event.target as HTMLElement).closest(".menu")) open = null;
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === "Escape") close();
  }

  // 展开的下拉与 AppKit 打开中的菜单一样"占键"：画布池在菜单展开时静默，
  // 菜单池（Ctrl+S 一类）照常。
  $effect(() => {
    if (open === null) return;
    return registerCanvasModalGuard(() => open !== null);
  });

  // 把这份菜单发布给命令搜索面板（票 54）：菜单是前端数据，面板搜的就是这里
  // 渲染的那份 —— 动态标题（向下合并 / 合并组）、最近项目、可用性与勾都一致。
  $effect(() => {
    menuModel.set(menus);
  });
</script>

<svelte:document onclick={onDocClick} />
<svelte:window onkeydown={onKeydown} />

<nav class="menubar" aria-label="菜单">
  {#each menus as menu (menu.label)}
    <div class="menu">
      <button
        class:active={open === menu.label}
        onclick={() => (open = open === menu.label ? null : menu.label)}
      >
        {menu.label}
      </button>
      {#if open === menu.label}
        <div class="dropdown" role="menu">
          {#each menu.items as item (item.label)}
            <button
              class="item"
              role="menuitem"
              class:disabled={item.disabled}
              title={item.placeholder}
              disabled={item.disabled}
              onclick={() => {
                close();
                item.run?.();
              }}
            >
              <span class="label">
                {#if item.checked !== undefined}
                  <span class="check" aria-hidden="true">{item.checked ? "✓" : ""}</span>
                {/if}
                {item.label}
              </span>
              {#if item.hint || item.placeholder}
                <span class="hint">{item.hint ?? item.placeholder}</span>
              {/if}
            </button>
          {/each}
        </div>
      {/if}
    </div>
  {/each}
</nav>

<style>
  .menubar {
    display: flex;
    gap: 2px;
    align-items: stretch;
  }

  .menu {
    position: relative;
  }

  .menu > button {
    height: 100%;
    padding: 0 10px;
    background: transparent;
    border: none;
    border-radius: 5px;
    font-size: 12px;
  }

  .menu > button:hover,
  .menu > button.active {
    background: var(--bg-raised);
    color: var(--text-strong);
  }

  .dropdown {
    position: absolute;
    top: 100%;
    left: 0;
    min-width: 244px;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 8px;
    box-shadow: 0 14px 36px rgb(0 0 0 / 34%);
    padding: 5px;
    z-index: 50;
    display: flex;
    flex-direction: column;
  }

  .item {
    display: flex;
    justify-content: space-between;
    gap: 18px;
    background: transparent;
    border: none;
    min-height: 28px;
    padding: 5px 9px;
    border-radius: 5px;
    font-size: 12px;
    text-align: left;
  }

  .item:hover:not(.disabled) {
    background: var(--accent-surface);
    color: var(--text-strong);
  }

  .item.disabled {
    opacity: 0.45;
    cursor: default;
  }

  .item .hint {
    color: var(--text-dim);
    font-size: 11px;
  }

  /* 开关型菜单项的勾：固定宽度，避免勾出现/消失时整列文字左右跳。 */
  .item .label {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }

  .item .check {
    display: inline-block;
    width: 10px;
    color: var(--accent, #3b82f6);
  }
</style>
