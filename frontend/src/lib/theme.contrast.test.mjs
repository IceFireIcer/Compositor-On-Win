import { readFileSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

// 票 92/U39：深色主题的对比度。
//
// 这个应用只有一套深色主题（app.css 的 `color-scheme: dark`），所以「深色主题下的对比度实测」
// 就是对那组令牌算 WCAG 比值 —— 纯计算，不需要浏览器，也不会因为别人调色降级而无人察觉。
//
// 令牌直接从 app.css 读，不抄一份到测试里：抄一份就变成「测试自己和自己比」。
// 用 .mjs 而不是 .ts：svelte-check 的 include 只覆盖 .ts/.js/.svelte，而它没有 Node 类型；
// 这份测试要 node:fs，所以走 .mjs（与 *.browser.test.mjs 同一约定）。
function readTokens() {
  const css = readFileSync(path.join(import.meta.dirname, "..", "app.css"), "utf8");
  const block = css.slice(css.indexOf(":root"), css.indexOf("}", css.indexOf(":root")));
  const tokens = {};
  for (const line of block.split("\n")) {
    const m = line.match(/--([\w-]+):\s*(#[0-9a-fA-F]{6})/);
    if (m) tokens[`--${m[1]}`] = m[2].toLowerCase();
  }
  return tokens;
}

function luminance(hex) {
  const channels = [1, 3, 5]
    .map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
    .map((v) => (v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4)));
  return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
}

function contrast(a, b) {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

describe("深色主题的对比度", () => {
  const tokens = readTokens();

  it("解析到了那组令牌", () => {
    // 解析失败会让下面每条都空转通过，先钉住。
    for (const name of ["--bg-app", "--bg-panel", "--bg-raised", "--text", "--text-strong", "--text-dim", "--accent", "--accent-surface"]) {
      expect(tokens[name], name).toMatch(/^#[0-9a-f]{6}$/);
    }
  });

  // 正文与弱文本都得过 AA（4.5:1）—— 弱文本用在提示与次要标签上，也是正常字号。
  const textPairs = [
    ["--text", "--bg-app", "正文 / 窗口底"],
    ["--text", "--bg-panel", "正文 / 面板"],
    ["--text", "--bg-raised", "正文 / 凸起"],
    ["--text", "--bg-canvas", "正文 / 画布底"],
    ["--text-strong", "--bg-panel", "强调 / 面板"],
    ["--text-dim", "--bg-app", "弱文本 / 窗口底"],
    ["--text-dim", "--bg-panel", "弱文本 / 面板"],
    ["--text-dim", "--bg-raised", "弱文本 / 凸起"],
    ["--accent", "--bg-panel", "强调色 / 面板"],
    ["--accent", "--accent-surface", "强调色 / 强调底"],
    ["--warning", "--bg-panel", "警告 / 面板"],
    ["--danger", "--bg-panel", "危险 / 面板"],
  ];

  it.each(textPairs)("%s on %s（%s）达到 AA", (fg, bg) => {
    expect(contrast(tokens[fg], tokens[bg])).toBeGreaterThanOrEqual(4.5);
  });

  // --border 是分隔线，不是文字，也不承担「识别控件」的职责：本轮在工具栏上量过，那里的控件都走
  // 浏览器原生外观（border-style: none、背景透明），边界由 UA 自己画。所以这里**不**要求它到 3:1，
  // 只钉住它确实比背景亮 —— 分隔线要看得见。
  it("--border 比面板底色亮，但保持在安静的一侧", () => {
    const ratio = contrast(tokens["--border"], tokens["--bg-panel"]);
    expect(ratio).toBeGreaterThan(1.2);
    expect(ratio).toBeLessThan(3);
  });
});
