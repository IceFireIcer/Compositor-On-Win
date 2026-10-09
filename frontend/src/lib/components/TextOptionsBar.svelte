<script lang="ts">
  import { textSession, textDefaults, updateTextStyle, clampOption, channelsToHex, hexToChannels } from "../state/text";
  import { setRunColor, setRunFont, uniformColorIn, uniformFontIn } from "../state/textRuns";
  import { fonts, fontsLoaded, isInstalled } from "../state/fonts";
  import FontPicker from "./FontPicker.svelte";

  /**
   * 文字选项栏（票 44/45）：工具头设置区——字体（选择器）、字号、颜色、
   * 对齐、字距、行高。会话进行中改字体/字号会实时作用于会话样式；颜色与
   * 对齐同步进默认值，供下一次新建使用（票 45 补充字体选择器与逐 run 混排）。
   *
   * 票 45：行内存在非折叠选区（session.selection）时，字体与颜色写入逐
   * run 样式（textRuns 的 setRunFont/setRunColor）；无选区时更新基础字段。
   * 选区内混排时字体显示“多种字体”、颜色回落到基础色。控件结果整体交给
   * updateTextStyle，保证 run 落进会话样式。
   */

  const style = $derived($textSession?.style ?? $textDefaults);
  const contentLength = $derived($textSession?.content.length);
  // 非折叠选区才走逐 run 语义（null/折叠 = 改基础字段）。
  const selection = $derived(
    $textSession?.selection && $textSession.selection.end > $textSession.selection.start
      ? $textSession.selection
      : null,
  );

  /** 选区内的统一字体；混排为空串（FontPicker 显示“多种字体”）。 */
  const activeFont = $derived(
    selection ? (uniformFontIn(style, selection, contentLength) ?? "") : style.fontName,
  );
  /** 选区内统一颜色；混排回落到基础色（与 Photoshop 的取色行为一致）。 */
  const activeColor = $derived(
    selection
      ? (uniformColorIn(style, selection, contentLength) ?? { red: style.red, green: style.green, blue: style.blue })
      : { red: style.red, green: style.green, blue: style.blue },
  );

  // 清单加载完成后再判断缺失，避免把“尚未加载”显示成未安装。
  const fontMissing = $derived(
    $fontsLoaded && $fonts.length > 0 && activeFont !== "" && !isInstalled(activeFont),
  );

  function set<K extends keyof typeof style>(key: K, value: (typeof style)[K]): void {
    updateTextStyle({ [key]: value } as never);
  }

  function pickFont(family: string): void {
    const sess = $textSession;
    // 无会话：改默认值的基础字体（原版 changeTextStyle 的 textDefaults 分支）。
    if (!sess) {
      updateTextStyle({ fontName: family });
      return;
    }
    // 有会话：逐 run setter 自己处理空/整段选区（→ 改基础字体并清空 fontRuns）。
    const next = setRunFont(sess.style, sess.selection, family, sess.content.length);
    updateTextStyle({ fontName: next.fontName, fontRuns: next.fontRuns });
  }

  function pickColor(hex: string): void {
    const { r, g, b } = hexToChannels(hex);
    const sess = $textSession;
    if (!sess) {
      updateTextStyle({ red: r, green: g, blue: b });
      return;
    }
    const next = setRunColor(sess.style, sess.selection, { red: r, green: g, blue: b }, sess.content.length);
    updateTextStyle({
      red: next.red,
      green: next.green,
      blue: next.blue,
      colorRuns: next.colorRuns,
    });
  }
</script>

<div class="options" role="toolbar" aria-label="文字选项">
  <label>
    字体
    <FontPicker value={activeFont} onChange={pickFont} />
    {#if fontMissing}
      <span class="missing">（本机未安装）</span>
    {/if}
  </label>
  <label>
    字号
    <input
      class="size"
      type="number"
      min="1"
      max="2000"
      value={style.fontSize}
      onchange={(e) => set("fontSize", clampOption(Number((e.target as HTMLInputElement).value), 1, 2000))}
    />
  </label>
  <label>
    颜色
    <input
      type="color"
      value={channelsToHex(activeColor.red, activeColor.green, activeColor.blue)}
      oninput={(e) => pickColor((e.target as HTMLInputElement).value)}
    />
  </label>
  <label>
    对齐
    <select
      value={style.alignment}
      onchange={(e) => set("alignment", (e.target as HTMLSelectElement).value as typeof style.alignment)}
    >
      <option value="Left">左</option>
      <option value="Center">居中</option>
      <option value="Right">右</option>
    </select>
  </label>
  <label>
    字距
    <input
      class="tracking"
      type="number"
      min="-100"
      max="1000"
      step="1"
      value={style.tracking}
      onchange={(e) => set("tracking", clampOption(Number((e.target as HTMLInputElement).value), -100, 1000))}
    />
  </label>
  <label>
    行高
    <input
      class="leading"
      type="number"
      min="0"
      max="5000"
      step="1"
      value={style.leading}
      onchange={(e) => set("leading", clampOption(Number((e.target as HTMLInputElement).value), 0, 5000))}
    />
  </label>
  <span class="hint">
    {#if selection}选中文字可单独改字体/颜色 · {/if}点按建点文本 · 拖拽建段落框 · ⌘回车提交 / Esc 取消
  </span>
</div>

<style>
  .options {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 4px 10px;
    font-size: 12px;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
  }

  label {
    display: flex;
    align-items: center;
    gap: 4px;
    color: var(--text-dim);
  }

  input,
  select {
    background: var(--bg-panel);
    color: inherit;
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 2px 4px;
    font-size: 12px;
  }

  .size,
  .tracking,
  .leading {
    width: 62px;
  }

  input[type="color"] {
    width: 30px;
    height: 22px;
    padding: 0;
  }

  .missing {
    color: var(--danger);
    font-size: 11px;
  }

  .hint {
    margin-left: auto;
    color: var(--text-dim);
  }
</style>
