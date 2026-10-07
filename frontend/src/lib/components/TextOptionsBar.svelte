<script lang="ts">
  import { textSession, textDefaults, updateTextStyle, DEFAULT_TEXT_STYLE, clampOption, channelsToHex, hexToChannels } from "../state/text";

  /**
   * 文字选项栏（票 44）：工具头设置区——字体名、字号、颜色、对齐、字距、
   * 行高。会话进行中改字体/字号会实时作用于会话样式；颜色与对齐同步进
   * 默认值，供下一次新建使用（票 45 补充字体选择器与逐 run 混排）。
   */

  const style = $derived($textSession?.style ?? $textDefaults);

  function set<K extends keyof typeof style>(key: K, value: (typeof style)[K]): void {
    updateTextStyle({ [key]: value } as never);
  }
</script>

<div class="options" role="toolbar" aria-label="文字选项">
  <label>
    字体
    <input
      class="font"
      type="text"
      value={style.fontName}
      onchange={(e) => set("fontName", (e.target as HTMLInputElement).value || DEFAULT_TEXT_STYLE.fontName)}
    />
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
      value={channelsToHex(style.red, style.green, style.blue)}
      oninput={(e) => {
        const { r, g, b } = hexToChannels((e.target as HTMLInputElement).value);
        updateTextStyle({ red: r, green: g, blue: b });
      }}
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
  <span class="hint">点按建点文本 · 拖拽建段落框 · ⌘回车提交 / Esc 取消</span>
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

  .font {
    width: 130px;
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

  .hint {
    margin-left: auto;
    color: var(--text-dim);
  }
</style>
