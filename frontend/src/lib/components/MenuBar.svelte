<script lang="ts">
  import {
    FILTER_KINDS,
    ADJUSTMENT_KINDS,
    beginFilter,
    filterSession,
  } from "../state/filters";
  import { document } from "../state/document";
  import { importImages } from "../state/workspace";
  import { copyMerged, exportPNG, openExportJPEG } from "../state/export";
  import { geometrySheet } from "../state/geometry";
  import {
    layerMenuState,
    openGeometry,
    openProjectSafe,
    openShortcutsSheet,
    pendingConversions,
    runInvert,
    runSelect,
    runShortcut,
    saveProjectSafe,
  } from "../shortcuts/actions";
  import {
    clearRecentProjects,
    openPath,
    recentProjects,
    refreshRecentProjects,
  } from "../state/shell";
  import { checkForUpdates } from "../state/update";
  import { chordFor } from "../shortcuts/settings";
  import { chordLabel } from "../shortcuts/chords";
  import { registerCanvasModalGuard } from "../shortcuts/dispatch";
  import { textSession } from "../state/text";

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

  interface MenuItem {
    label: string;
    hint?: string;
    disabled?: boolean;
    placeholder?: string;
    run?: () => void;
  }
  interface Menu {
    label: string;
    items: MenuItem[];
  }

  let open = $state<string | null>(null);
  const hasLayer = $derived(!!$document.activeLayerID);
  const dialogOpen = $derived($filterSession !== null);
  // 图层菜单的标题/可用性来自被测试钉住的纯函数（state/layerOps.ts），
  // 所以菜单、快捷键、Go 三处对"⌘E 现在合并什么"的看法一致。
  const layerMenu = $derived(layerMenuState($document));
  // 文字会话：剪贴板命令让位给行内文本框（菜单路径也要挡）。
  const editingText = $derived($textSession !== null);

  // 最近项目（票 49）：文件菜单里逐条列出，最后一条是"清除最近记录"。
  // 列表由 Go 过滤（移动/删除过的项目不出现），开菜单时重取一次。
  const recentFiles = $derived($recentProjects);
  $effect(() => {
    if (open === "文件") void refreshRecentProjects();
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
        {
          label: "选择主体",
          hint: hintFor("Menus:Select Subject"),
          disabled: !hasLayer || dialogOpen,
          run: () => void runSelectMenu("subject"),
        },
        {
          label: "对象选择（画布中心）",
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
        { label: "撤销", hint: hintFor("Menus:Undo"), run: () => runAndClose("Menus:Undo") },
        { label: "重做", hint: hintFor("Menus:Redo"), run: () => runAndClose("Menus:Redo") },
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
            run: () => {
              void beginFilter(k.id, k.label.replace(/…$/, ""), true);
              close();
            },
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
      ],
    },
    {
      label: "图层",
      items: [
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
        {
          // 变换控件还没画（票 17）：菜单项先留着显示键位，按下不做事。
          label: "显示变换控件",
          hint: hintFor("Menus:Show Transform Controls"),
          disabled: true,
        },
      ],
    },
    {
      label: "滤镜",
      items: [
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
              <span>{item.label}</span>
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
    font-size: 12px;
  }

  .menu > button:hover,
  .menu > button.active {
    background: var(--bg-canvas);
  }

  .dropdown {
    position: absolute;
    top: 100%;
    left: 0;
    min-width: 220px;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: 0 10px 30px rgb(0 0 0 / 30%);
    padding: 4px;
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
    padding: 5px 10px;
    font-size: 12px;
    text-align: left;
  }

  .item:hover:not(.disabled) {
    background: var(--bg-canvas);
  }

  .item.disabled {
    opacity: 0.45;
    cursor: default;
  }

  .item .hint {
    color: var(--text-dim);
    font-size: 11px;
  }
</style>
