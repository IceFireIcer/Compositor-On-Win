<script lang="ts">
  import {
    FILTER_KINDS,
    DITHER_PLACEHOLDER,
    ADJUSTMENT_KINDS,
    beginFilter,
    filterSession,
  } from "../state/filters";
  import { document, applyDocumentSnapshot } from "../state/document";
  import { Undo, Redo, ApplyFilter } from "../../../wailsjs/go/bridge/Service";

  /**
   * The menu bar (CompositorApp.swift's menus): 文件 / 图像 / 滤镜. 图像
   * carries the destructive adjustment entries and 反相/反相蒙版; 滤镜 the
   * ten FilterKind items. Every item opens a live-preview dialog session.
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

  function close(): void {
    open = null;
  }

  // ⌘I semantics: a selected mask inverts the mask, otherwise the pixels.
  // Mask selection is not wired yet, so 反相 targets pixels and 反相蒙版
  // targets the mask (the Go side rejects layers without one).
  async function runInvert(kind: "invert" | "invertMask"): Promise<void> {
    const doc = $document;
    if (!doc.docId || !doc.activeLayerID) return;
    try {
      applyDocumentSnapshot(await ApplyFilter(kind, doc.activeLayerID, "{}", 0, ""));
    } catch (err) {
      console.warn("反相失败", err);
    }
    close();
  }

  async function runUndoRedo(kind: "undo" | "redo"): Promise<void> {
    try {
      const raw = kind === "undo" ? await Undo() : await Redo();
      applyDocumentSnapshot(raw);
    } catch (err) {
      console.warn(kind === "undo" ? "没有可撤销的操作" : "没有可重做的操作");
    }
    close();
  }

  const menus = $derived<Menu[]>([
    {
      label: "文件",
      items: [
        { label: "撤销", hint: "⌘Z", run: () => void runUndoRedo("undo") },
        { label: "重做", hint: "⇧⌘Z", run: () => void runUndoRedo("redo") },
      ],
    },
    {
      label: "图像",
      items: [
        ...ADJUSTMENT_KINDS.map((k) => ({
          label: k.label,
          hint: k.placeholder,
          disabled: !hasLayer || dialogOpen,
          run: () => void beginFilter(k.id, k.label.replace(/…$/, ""), true),
        })),
        {
          label: "反相",
          hint: "⌘I",
          disabled: !hasLayer || dialogOpen,
          run: () => void runInvert("invert"),
        },
        {
          label: "反相蒙版",
          disabled: !hasLayer || dialogOpen,
          run: () => void runInvert("invertMask"),
        },
      ],
    },
    {
      label: "滤镜",
      items: [
        ...FILTER_KINDS.map((k) => ({
          label: k.label,
          disabled: !hasLayer || dialogOpen || k.id === "removeBackground",
          placeholder: k.placeholder,
          run: () => void beginFilter(k.id, k.label.replace(/…$/, ""), false),
        })),
        {
          label: "抖动…",
          disabled: true,
          placeholder: DITHER_PLACEHOLDER,
        },
      ],
    },
  ]);

  function onDocClick(event: MouseEvent): void {
    if (open && !(event.target as HTMLElement).closest(".menu")) open = null;
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === "Escape") close();
  }
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
