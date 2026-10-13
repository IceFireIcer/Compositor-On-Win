/**
 * 命令搜索的匹配与排序（票 54）——原版 CommandPaletteSearch
 * （reference/Swift/Compositor/UI/CommandPalette.swift:32-99）的 TS 移植，
 * 逐行对照，包括分档公式与并列时的菜单顺序：
 *
 *   1. 字母按序出现即命中（大小写无关，查询里的空白忽略）；
 *   2. 每个字母计分：命中 +1、落在词首 +8、紧接上一个命中 +5；
 *   3. 再分档：整段是词首（5）> 逐词词首（4）> 整段包含（3）>
 *      去空格后包含（2）> 只剩散落字母（1）；
 *   4. 分数 = 档位 × 1_000_000 +（散落档才加 total × 100）− 标题长度 / 4。
 *
 * 「lasso」要找到 Lasso，而不是把 l/a/s/s/o 散在长标题里的
 * 「Layer › … Hue/Saturation」；同分时保持菜单顺序，工具的模式才按 Tab
 * 的顺序出现（矩形选框 → 椭圆选框）。这里是纯函数，面板组件只负责渲染。
 */

/** 面板里的一行：菜单命令、图层蒙版命令或工具（原版 CommandPaletteEntry）。 */
export interface PaletteEntry {
  /** 在条目中唯一：标题，重名时追加出现序号（原版 id）。 */
  id: string;
  /** 面板显示并搜索的完整路径，如「滤镜 › 高斯模糊…」。 */
  title: string;
  /** 当前和弦标签（「Ctrl+Shift+Z」），没有快捷键时为 null。 */
  shortcut: string | null;
  /** 菜单项此刻是否可用：不可用的照样列出、灰显、永不执行。 */
  isEnabled: boolean;
  /** 此刻处于打开状态的开关（菜单上的勾），让人知道再选一次是关掉它。 */
  isOn: boolean;
  run: () => void;
}

const LETTER = /\p{L}/u;
const NUMBER = /\p{N}/u;

function isWordChar(character: string | undefined): boolean {
  return character !== undefined && (LETTER.test(character) || NUMBER.test(character));
}

/** 每个词都起 title 里的一个词（原版 wordsStartWords）。 */
function wordsStartWords(words: readonly string[], title: string): boolean {
  let from = 0;
  for (const word of words) {
    let found: number | null = null;
    let search = from;
    for (;;) {
      const index = title.indexOf(word, search);
      if (index < 0) break;
      if (index === 0 || !LETTER.test(title[index - 1] ?? "")) {
        found = index + word.length;
        break;
      }
      search = index + 1;
    }
    if (found === null) return false;
    from = found;
  }
  return true;
}

/**
 * 匹配得分：null = 不匹配；查询为空（或只有空白）时是 0。
 * 分数越大越靠前，只在散落字母档（tier 1）里才比较命中质量。
 */
export function paletteScore(query: string, text: string): number | null {
  const wanted = [...query.toLowerCase()].filter((character) => !/\s/.test(character));
  if (wanted.length === 0) return 0;
  const letters = [...text.toLowerCase()];
  let total = 0;
  let from = 0;
  let previous = -2;
  for (const character of wanted) {
    let found = -1;
    for (let index = from; index < letters.length; index++) {
      if (letters[index] === character) {
        found = index;
        break;
      }
    }
    if (found < 0) return null;
    const wordStart = found === 0 || !isWordChar(letters[found - 1]);
    total += 1 + (wordStart ? 8 : 0) + (found === previous + 1 ? 5 : 0);
    previous = found;
    from = found + 1;
  }

  const title = letters.join("");
  const phrase = query.toLowerCase().trim();
  const words = phrase.split(" ").filter((word) => word.length > 0);
  let tier: number;
  if (wordsStartWords([phrase], title)) tier = 5;
  else if (words.length > 1 && wordsStartWords(words, title)) tier = 4;
  else if (title.includes(phrase)) tier = 3;
  else if (title.replaceAll(" ", "").includes(wanted.join(""))) tier = 2;
  else tier = 1;
  return tier * 1_000_000 + (tier === 1 ? total * 100 : 0) - Math.trunc(letters.length / 4);
}

/**
 * 命中的条目，最好的在前，可用的排在不可用的之前；没有查询词时按菜单顺序，
 * 可用的在前（原版 CommandPaletteSearch.rank）。
 */
export function rankEntries(entries: readonly PaletteEntry[], query: string): PaletteEntry[] {
  const scored: { entry: PaletteEntry; score: number; index: number }[] = [];
  entries.forEach((entry, index) => {
    const score = paletteScore(query, entry.title);
    if (score !== null) scored.push({ entry, score, index });
  });
  scored.sort((a, b) => {
    if (a.entry.isEnabled !== b.entry.isEnabled) return a.entry.isEnabled ? -1 : 1;
    if (query === "" || a.score === b.score) return a.index - b.index;
    return b.score - a.score;
  });
  return scored.map((row) => row.entry);
}
