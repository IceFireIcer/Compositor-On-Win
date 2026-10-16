<script lang="ts">
  import { onMount } from "svelte";
  import { chordFromEvent, chordLabel, isNoneChord, type ShortcutChord } from "../shortcuts/chords";
  import {
    SHORTCUTS,
    originalChord,
    type ShortcutDefinition,
  } from "../shortcuts/table";
  import {
    loadOverrides,
    problemIn,
    saveOverrides,
    shortcutsSheet,
  } from "../shortcuts/settings";

  /**
   * KeyboardShortcutsSheet（票 47）——原版 KeyboardShortcutsSheet +
   * ShortcutRecorder（KeyboardShortcuts.swift:229-320）的 Windows 版：
   * 三个分组、"单击一条快捷键再按新组合"的说明、搜索框（按标题不区分大小写）、
   * 每行一个录制按钮（显示当前和弦标签）、保存/取消/恢复默认，
   * 以及原版那条“快捷键编辑器本身用回车保存、Esc 取消（录制中除外）”的注记。
   *
   * 录制：捕获阶段监听 window 的 keydown（连 Ctrl+W 一类也会被捕获），
   * 用 chordFromEvent 取和弦；纯修饰键或非单键不写入（原版是 NSSound.beep()，
   * 这里改成按钮闪一下），录制中 Enter/Esc 归录制器、不归表单。
   */

  const GROUPS = ["Menus", "Canvas & Layers", "Text Editing"] as const;
  const GROUP_LABELS: Record<(typeof GROUPS)[number], string> = {
    Menus: "菜单",
    "Canvas & Layers": "画布与图层",
    "Text Editing": "文字编辑",
  };

  let draft = $state<Record<string, ShortcutChord>>({});
  let search = $state("");
  let recording = $state<string | null>(null);
  let flash = $state<string | null>(null);
  let searchInput = $state<HTMLInputElement | null>(null);

  const problem = $derived(problemIn(draft));

  function openSheet(): void {
    // 每次打开都从持久层取一次（上一次可能被别的入口改过）。
    draft = { ...loadOverrides() };
    search = "";
    recording = null;
  }

  function currentChord(definition: ShortcutDefinition): ShortcutChord {
    return draft[definition.id] ?? originalChord(definition);
  }

  function visible(definition: ShortcutDefinition): boolean {
    const needle = search.trim().toLowerCase();
    if (needle === "") return true;
    return definition.title.toLowerCase().includes(needle);
  }

  function groupCount(group: (typeof GROUPS)[number]): number {
    return SHORTCUTS.filter((d) => d.group === group && visible(d)).length;
  }

  function startRecording(id: string, button: HTMLButtonElement): void {
    recording = id;
    button.focus();
  }

  function stopRecording(): void {
    recording = null;
  }

  function rejectRecording(id: string): void {
    // 原版 NSSound.beep()；浏览器里没有对应的提示音，闪一下按钮。
    flash = id;
    setTimeout(() => {
      if (flash === id) flash = null;
    }, 220);
  }

  /** 捕获阶段的窗口监听：录制中一切按键都先经过这里。 */
  function onCaptureKeydown(e: KeyboardEvent): void {
    if (recording === null) return;
    e.preventDefault();
    e.stopPropagation();
    // 纯修饰键不录制，也不结束录制（原版按钮继续等下一个键）。
    const chord = chordFromEvent(e);
    if (chord === null) return;
    if (Array.from(chord.key).length !== 1) {
      rejectRecording(recording);
      return;
    }
    draft = { ...draft, [recording]: chord };
    stopRecording();
  }

  function onKeydown(e: KeyboardEvent): void {
    // 录制中：Enter/Esc 归录制器（上面的捕获监听已拦下）。
    if (recording !== null) return;
    if (e.key === "Escape") {
      e.preventDefault();
      close();
    } else if (e.key === "Enter") {
      e.preventDefault();
      save();
    }
  }

  function save(): void {
    if (recording !== null || problem !== null) return;
    saveOverrides(draft);
    close();
  }

  function cancel(): void {
    stopRecording();
    close();
  }

  /**
   * 录制器上的 ✕：把这一条设为「没有键」（原版 KeyboardShortcuts.swift:293-300）。
   * 只在**正在录制**的那一行出现，否则整张表会变成一列 ✕。
   */
  function clearShortcut(id: string): void {
    draft = { ...draft, [id]: { key: "", modifiers: 0 } };
    stopRecording();
  }

  /** 恢复默认：只清草稿（原版 `draft = [:]`），保存时才落盘。 */
  function resetDraft(): void {
    stopRecording();
    draft = {};
  }

  function close(): void {
    stopRecording();
    shortcutsSheet.set(false);
  }

  onMount(() => {
    openSheet();
    searchInput?.focus();
    return stopRecording;
  });
</script>

<svelte:window onkeydowncapture={onCaptureKeydown} onkeydown={onKeydown} />

<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
<div
  class="backdrop"
  role="presentation"
  onclick={(e) => {
    if (e.target === e.currentTarget) cancel();
  }}
>
  <div class="sheet" role="dialog" aria-modal="true" aria-label="键盘快捷键">
    <header>
      <h2>键盘快捷键</h2>
      <button class="close" title="关闭" onclick={cancel}>×</button>
    </header>

    <p class="note">点一条快捷键，然后按下新的组合键。保存后生效。</p>

    <input
      class="search"
      type="search"
      placeholder="搜索快捷键"
      aria-label="搜索快捷键"
      bind:value={search}
      bind:this={searchInput}
    />

    <div class="rows">
      {#each GROUPS as group (group)}
        <h3>{GROUP_LABELS[group]}（{groupCount(group)}）</h3>
        {#each SHORTCUTS.filter((d) => d.group === group && visible(d)) as definition (definition.id)}
          <div class="row">
            <span class="title" title={definition.id}>{definition.title}</span>
            {#if draft[definition.id] !== undefined}
              <span class="changed" title="已改动">•</span>
            {/if}
            <button
              class="recorder"
              class:recording={recording === definition.id}
              class:flash={flash === definition.id}
              aria-label={recording === definition.id ? "请按组合键" : chordLabel(currentChord(definition))}
              onclick={(e) => startRecording(definition.id, e.currentTarget)}
            >
              {recording === definition.id ? "按下按键…" : chordLabel(currentChord(definition))}
            </button>
            {#if recording === definition.id && !isNoneChord(currentChord(definition))}
              <button
                class="clear"
                title="移除这条快捷键"
                aria-label="移除这条快捷键"
                onclick={() => clearShortcut(definition.id)}
              >✕</button>
            {/if}
          </div>
        {/each}
      {/each}
    </div>

    {#if problem}
      <p class="problem" role="alert">{problem}</p>
    {/if}

    <footer>
      <button onclick={resetDraft} title="丢弃草稿里的改动（保存后恢复原版组合键）">恢复默认</button>
      <span class="footer-note">快捷键编辑器本身使用回车保存、Esc 取消（录制中除外）</span>
      <button onclick={cancel}>取消</button>
      <button class="primary" disabled={recording !== null || problem !== null} onclick={save}>保存</button>
    </footer>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.45);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 40;
  }

  .sheet {
    width: 660px;
    max-width: 94vw;
    max-height: 86vh;
    padding: 18px 20px;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 8px;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  h2 {
    margin: 0;
    font-size: 15px;
  }

  h3 {
    margin: 12px 0 4px;
    font-size: 12px;
    color: var(--text-dim);
    position: sticky;
    top: 0;
    background: var(--bg-panel);
  }

  .note {
    margin: 0;
    color: var(--text-dim);
    font-size: 12px;
  }

  .search {
    background: var(--bg-raised);
    border: 1px solid var(--border);
    border-radius: 4px;
    color: var(--text);
    padding: 6px 8px;
    font: inherit;
  }

  .rows {
    overflow-y: auto;
    flex: 1;
    min-height: 220px;
    padding-right: 6px;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 2px 0;
  }

  .row .title {
    flex: 1;
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .changed {
    color: var(--accent);
  }

  .clear {
    background: transparent;
    border: none;
    color: var(--muted, #888);
    font-size: 12px;
    padding: 0 4px;
  }

  .recorder {
    min-width: 132px;
    padding: 3px 10px;
    font-size: 12px;
    font-variant-numeric: tabular-nums;
  }

  .recorder.recording {
    border-color: var(--accent);
    color: var(--accent);
  }

  .recorder.flash {
    border-color: var(--danger);
    color: var(--danger);
  }

  .problem {
    margin: 0;
    color: #e08a2e;
    font-size: 12px;
    min-height: 16px;
  }

  footer {
    display: flex;
    align-items: center;
    gap: 8px;
    border-top: 1px solid var(--border);
    padding-top: 10px;
  }

  .footer-note {
    flex: 1;
    color: var(--text-dim);
    font-size: 11px;
  }

  footer button {
    padding: 5px 14px;
  }

  footer .primary {
    background: var(--accent-dim);
    border-color: var(--accent);
  }

  footer .primary:disabled {
    opacity: 0.45;
  }

  .close {
    background: transparent;
    border: none;
    color: var(--text-dim);
    font-size: 16px;
    padding: 2px 6px;
  }
</style>
