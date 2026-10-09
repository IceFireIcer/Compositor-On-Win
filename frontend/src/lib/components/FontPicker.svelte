<script lang="ts">
  import { onMount } from "svelte";
  import { filterFonts, fonts, fontFamilies, fontsLoaded, isInstalled, loadFonts } from "../state/fonts";

  /**
   * 字体选择器（票 45）：折叠时是显示当前字体的按钮，展开后是搜索框 +
   * 按家族分组的滚动列表（右侧灰字显示 PostScript 名）。键盘：↑↓ 移动、
   * Enter 选择、Esc 关闭不选；点击外部关闭。选中只回传家族名——逐 run
   * 的写入语义由调用方（TextOptionsBar）负责。
   *
   * value 为空串表示“多种字体”（选中范围内混排），此时按钮显示占位文案。
   */

  let {
    value,
    onChange,
  }: {
    value: string;
    onChange: (family: string) => void;
  } = $props();

  let open = $state(false);
  let query = $state("");
  let highlight = $state(0);
  let root: HTMLDivElement;
  let search: HTMLInputElement | null = $state(null);

  // 挂载即预取清单，让“未安装”标记在展开前就准确。
  onMount(() => {
    void loadFonts();
  });

  const families = $derived(filterFonts(query, $fontFamilies));
  const mixed = $derived(value === "");
  const missing = $derived(
    $fontsLoaded && $fonts.length > 0 && !mixed && !isInstalled(value),
  );

  // 展开后把焦点交给搜索框（search 由 bind:this 赋值，故一并作为依赖）。
  $effect(() => {
    if (open && search) search.focus();
  });

  /** 当前字体所在家族的下标；缺失时回到第一项。 */
  function currentIndex(): number {
    const at = $fontFamilies.findIndex((group) => group.family === value);
    return Math.max(0, at);
  }

  function toggle(): void {
    if (open) {
      open = false;
      return;
    }
    void loadFonts();
    query = "";
    highlight = currentIndex();
    open = true;
  }

  /** 搜索词变化：列表被过滤，高亮回到第一项。 */
  function onSearchInput(): void {
    highlight = 0;
  }

  function choose(family: string): void {
    open = false;
    onChange(family);
  }

  function onKeydown(event: KeyboardEvent): void {
    if (event.key === "Escape") {
      if (!open) return;
      event.preventDefault();
      event.stopPropagation();
      open = false; // 关闭但不选择
      return;
    }
    if (!open) {
      if (event.key === "ArrowDown") {
        event.preventDefault();
        toggle();
      }
      return;
    }
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      const count = families.length;
      if (count === 0) return;
      highlight = (highlight + (event.key === "ArrowDown" ? 1 : count - 1)) % count;
    } else if (event.key === "Enter") {
      event.preventDefault();
      const chosen = families[highlight];
      if (chosen) choose(chosen.family);
    }
  }

  function onDocMousedown(event: MouseEvent): void {
    if (open && root && !root.contains(event.target as Node)) open = false;
  }

  // 键盘移动时把高亮行滚进可视区。
  function scrollActive(node: HTMLElement, active: boolean) {
    if (active) node.scrollIntoView({ block: "nearest" });
    return {
      update(next: boolean) {
        if (next) node.scrollIntoView({ block: "nearest" });
      },
    };
  }
</script>

<svelte:window onmousedown={onDocMousedown} />

<div class="picker" bind:this={root}>
  <button
    type="button"
    class="trigger"
    class:open
    aria-haspopup="listbox"
    aria-expanded={open}
    title={mixed ? "选中文字使用了多种字体" : value}
    onclick={toggle}
    onkeydown={onKeydown}
  >
    <span class="current" class:mixed>{mixed ? "多种字体" : value}</span>
    {#if missing}
      <span class="missing">未安装</span>
    {/if}
    <span class="caret" aria-hidden="true">▾</span>
  </button>

  {#if open}
    <div class="popup">
      <input
        class="search"
        type="text"
        placeholder="搜索字体…"
        spellcheck="false"
        bind:value={query}
        oninput={onSearchInput}
        onkeydown={onKeydown}
        bind:this={search}
      />
      <div class="list" role="listbox" aria-label="字体列表">
        {#each families as group, i (group.family)}
          <button
            type="button"
            class="row"
            class:active={i === highlight}
            role="option"
            aria-selected={i === highlight}
            title={group.faces.map((face) => face.postScriptName).join(" · ")}
            onmouseenter={() => (highlight = i)}
            onclick={() => choose(group.family)}
            onkeydown={onKeydown}
            use:scrollActive={i === highlight}
          >
            <span class="family">{group.family}</span>
            <span class="ps">{group.faces[0]?.postScriptName ?? ""}</span>
          </button>
        {/each}
        {#if families.length === 0}
          <p class="empty">{$fontsLoaded ? "没有匹配的字体" : "正在加载字体…"}</p>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .picker {
    position: relative;
  }

  .trigger {
    display: flex;
    align-items: center;
    gap: 4px;
    width: 150px;
    padding: 2px 4px;
    background: var(--bg-panel);
    color: inherit;
    border: 1px solid var(--border);
    border-radius: 4px;
    font-size: 12px;
    text-align: left;
  }

  .trigger.open {
    border-color: var(--accent);
  }

  .current {
    flex: 1;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .current.mixed {
    color: var(--text-dim);
  }

  .missing {
    flex: none;
    color: var(--danger);
    font-size: 10px;
  }

  .caret {
    flex: none;
    color: var(--text-dim);
    font-size: 9px;
  }

  .popup {
    position: absolute;
    top: calc(100% + 3px);
    left: 0;
    z-index: 50;
    width: 240px;
    padding: 4px;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 6px;
    box-shadow: 0 10px 30px rgb(0 0 0 / 30%);
  }

  .search {
    width: 100%;
    box-sizing: border-box;
    margin-bottom: 4px;
    padding: 3px 6px;
    background: var(--bg-raised);
    color: inherit;
    border: 1px solid var(--border);
    border-radius: 4px;
    font-size: 12px;
  }

  .list {
    max-height: 240px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
  }

  .row {
    display: flex;
    justify-content: space-between;
    align-items: baseline;
    gap: 10px;
    padding: 3px 6px;
    background: transparent;
    border: none;
    border-radius: 4px;
    font-size: 12px;
    text-align: left;
  }

  .row.active {
    background: var(--accent-dim);
  }

  .family {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }

  .ps {
    flex: none;
    color: var(--text-dim);
    font-size: 11px;
  }

  .empty {
    margin: 4px 6px;
    color: var(--text-dim);
    font-size: 12px;
  }
</style>
