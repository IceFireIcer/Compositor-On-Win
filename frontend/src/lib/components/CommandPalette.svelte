<script lang="ts">
  import { get } from "svelte/store";
  import {
    closeCommandPalette,
    commandPaletteOpen,
    paletteQuery,
    paletteSelection,
    setPaletteQuery,
  } from "../commands/session";
  import { registerCanvasModalGuard } from "../shortcuts/dispatch";
  import {
    movePaletteSelection,
    paletteResults,
    paletteSelected,
    runPaletteEntry,
  } from "../commands/store";

  /**
   * 命令搜索面板（票 54）——原版 CommandPaletteView（只读真值：
   * reference/Swift/Compositor/UI/CommandPaletteView.swift）：
   * 搜索框在上、结果列表填满面板，↑/↓ 选择（两端环绕）、回车执行、
   * Esc 关闭；勾显示开关状态，右侧显示当前快捷键，禁用的行灰显且点不动。
   *
   * 面板挂在 App 根上，由 $commandPaletteOpen 控制（与其它表单同一套写法）。
   */

  let input = $state<HTMLInputElement | null>(null);
  let list = $state<HTMLDivElement | null>(null);

  // 原版在面板成为 key window 之后（出现后一拍）才请求焦点：早了会打不进字
  // （CommandPaletteView.swift:47-49）。这里挂载即聚焦，效果相同。
  $effect(() => {
    input?.focus();
  });

  // 面板开着时画布池让位：焦点落在面板空白处（tabindex=-1 的 .palette）时，
  // 普通字母不该穿透去切工具 —— 原版面板是 key window，画布 keyDown 不响应。
  $effect(() => {
    if (!$commandPaletteOpen) return;
    return registerCanvasModalGuard(() => get(commandPaletteOpen));
  });

  // 选中的行始终可见（原版 ScrollViewReader.scrollTo(selected)）。
  $effect(() => {
    const id = $paletteSelected?.id;
    if (!id || !list) return;
    list
      .querySelector<HTMLElement>('[data-entry-id="' + CSS.escape(id) + '"]')
      ?.scrollIntoView({ block: "nearest" });
  });

  function onInput(event: Event & { currentTarget: HTMLInputElement }): void {
    setPaletteQuery(event.currentTarget.value);
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === "Escape") {
      event.preventDefault();
      closeCommandPalette();
      return;
    }
    if (event.key === "ArrowDown") {
      event.preventDefault();
      movePaletteSelection(1);
      return;
    }
    if (event.key === "ArrowUp") {
      event.preventDefault();
      movePaletteSelection(-1);
      return;
    }
    if (event.key === "Enter") {
      // 行是 <button>：焦点在行上时回车由它自己的 click 触发，别再跑一遍。
      if (event.target instanceof HTMLButtonElement) return;
      event.preventDefault();
      const selected = get(paletteSelected);
      if (selected) runPaletteEntry(selected);
      return;
    }
    // 面板开着时 Ctrl+F 是"再按一次收起"（原版 toggle 的语义）。焦点在搜索框里
    // 时全局分发让位给输入，所以这条必须由面板自己处理；焦点在某一行（button）上
    // 时全局分发**会**接住 Ctrl+F，必须 stopPropagation，否则它再 toggle 一次又
    // 把面板打开（净效果是面板不关、查询被清空）。
    if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "f") {
      event.preventDefault();
      event.stopPropagation();
      closeCommandPalette();
    }
  }

  /** 点面板外面 = 原版"面板失去焦点就收起"（点编辑器即关）。 */
  function onBackdropMousedown(event: MouseEvent): void {
    if (event.target === event.currentTarget) closeCommandPalette();
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="backdrop" onmousedown={onBackdropMousedown}>
  <!-- 按键挂在面板上而不是只挂在搜索框上：Tab 到某一行之后 Esc/↑/↓ 仍然有效
       （原版面板是 key window，Esc 在面板里处处有效）。 -->
  <div
    class="palette"
    role="dialog"
    aria-modal="true"
    aria-label="搜索命令"
    tabindex="-1"
    onkeydown={onKeydown}
  >
    <input
      class="search"
      bind:this={input}
      value={$paletteQuery}
      oninput={onInput}
      placeholder="搜索命令和工具"
      aria-label="搜索命令和工具"
      aria-controls="command-palette-results"
      autocomplete="off"
      spellcheck="false"
    />
    <div class="results" id="command-palette-results" role="listbox" aria-label="命令" bind:this={list}>
      {#each $paletteResults as entry, index (entry.id)}
        <button
          class="row"
          class:chosen={index === $paletteSelection}
          class:disabled={!entry.isEnabled}
          role="option"
          aria-selected={index === $paletteSelection}
          data-entry-id={entry.id}
          disabled={!entry.isEnabled}
          onclick={() => runPaletteEntry(entry)}
        >
          <!-- 勾只占位给有勾的行，其它行从边缘开始（原版 CommandPaletteView.row）。 -->
          {#if entry.isOn}
            <span class="check" aria-hidden="true">✓</span>
          {/if}
          <span class="title">{entry.title}</span>
          <span class="spacer"></span>
          {#if entry.shortcut}
            <span class="shortcut">{entry.shortcut}</span>
          {/if}
        </button>
      {/each}
      {#if $paletteResults.length === 0}
        <div class="empty">没有匹配的命令</div>
      {/if}
    </div>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 60;
    background: rgb(0 0 0 / 28%);
  }

  /* 面板居中、固定 460×290（原版 CommandPaletteController 的尺寸）。 */
  .palette {
    position: absolute;
    left: 50%;
    top: 50%;
    transform: translate(-50%, -50%);
    display: flex;
    flex-direction: column;
    width: 460px;
    height: 290px;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 8px;
    box-shadow: 0 18px 48px rgb(0 0 0 / 45%);
    overflow: hidden;
  }

  .search {
    flex: 0 0 auto;
    height: 44px;
    padding: 0 16px;
    border: none;
    border-bottom: 1px solid var(--border);
    border-radius: 0;
    background: transparent;
    color: var(--text-strong);
    font-size: 15px;
  }

  .search:focus {
    outline: none;
  }

  /* 列表一直排到面板下缘，超出的行在下面被裁掉（原版注释同此意）。 */
  .results {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    padding: 6px;
    display: flex;
    flex-direction: column;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 6px;
    width: 100%;
    min-height: 28px;
    padding: 5px 8px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--text);
    font-size: 12px;
    text-align: left;
  }

  .row.chosen {
    background: var(--accent-surface);
    color: var(--text-strong);
  }

  .row.disabled {
    color: var(--text-dim);
    opacity: 0.55;
    cursor: default;
  }

  .row .check {
    display: inline-block;
    width: 12px;
    color: var(--accent, #3b82f6);
  }

  .row .title {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row .spacer {
    flex: 1 1 auto;
  }

  .row .shortcut {
    color: var(--text-dim);
    font-family: ui-monospace, "Cascadia Mono", Consolas, monospace;
    font-size: 11px;
  }

  .empty {
    padding: 18px;
    color: var(--text-dim);
    font-size: 12px;
    text-align: center;
  }
</style>
