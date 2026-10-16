# 81 — 快捷键清除与旧配置迁移

**What to build:** 用户可把快捷键明确设为 None；升级后无效或被拒绝的旧配置只丢弃自身，其他自定义键继续生效。

**Blocked by:** 47 — 快捷键系统与重映射。

**Status:** done（2026-10-16）

- [x] 清除后的命令没有默认快捷键且释放冲突键。原版用**空键**表示「没有键」
      （`ShortcutChord.unassigned`，KeyboardShortcuts.swift:8-9），而 `problem(in:)` 对空键
      直接 `continue`（:202）—— 我们两条都没有：空键会被判「请使用单个按键」，清除根本存不下去；
      已补 `isNoneChord` 与 `problemIn` 的跳过分支。表单也补上了原版录制器上的 ✕（:293-300）。
      **Delete/Backspace 规范化无需改动**（已核对）：上游 `keyboardShortcut` / `recorded`（:39-47）
      是在 SwiftUI 的 `.delete`（U+0008）与表里记的 U+007F 之间转换，Windows 没有 SwiftUI 这一层；
      我们的录制器在 `chords.ts:76-77` 已把 Backspace 与 Delete 都归一到 `\u{7f}`，与表一致。
- [x] CMYK 校样命令可无默认键并可被用户分配。上游有**两条**（`CMYK Proof Colors`、
      `CMYK Gamut Warning`，:112-114），出厂都是 `.unassigned`。两条都已加进表（Menus 组末尾）、
      接上 `toggleProof`，菜单对无键命令**不显示**提示（原版 `assigned(_:)` 返回 nil）。
      取表与切换从 `MenuBar.svelte` 挪到 `state/cmykProof.ts`，因为快捷键也要调它们 ——
      两处各写一份就会分叉。
- [x] 已删除命令、拒绝的 chord 和坏记录逐项忽略，不丢弃同一配置文件中的有效覆盖。
      改成上游的**逐条采纳**（:156-167）：按 id 排序，一条条加进已接受集合，只有它自己会引入
      问题时才丢它。`sanitize` 也不再因为一条坏记录返回 null（整份丢弃）。

## 修的是什么

| | 修复前 | 修复后（= 上游） |
| --- | --- | --- |
| 空键（清除） | 判为非法，存不下去 | `problemIn` 跳过它，不占键、不参与重复判定 |
| 空键的标签 | `""`（空串） | `"None"` |
| 一条坏记录 | `sanitize` 返回 null → **整份配置丢弃** | 只丢这一条 |
| 一条与默认冲突的记录 | `problemIn` 不过 → **整份配置丢弃** | 只丢这一条 |
| 采纳顺序 | 无（要么全要要么全不要） | 按 id 排序逐条试用，结果确定 |
| 无键命令的菜单提示 | 会显示「None」 | 不显示 |

## 验证

`settings.test.ts` 六条：清掉的命令不占键、**对照组**（不清除时同一个键确实判重复）、清掉后可存可读、
坏记录只丢自己、坏记录排在前面也不影响后面、两条互相冲突只丢后试的那条。最后一条用位掩码 4 的
和弦 —— 它在 Windows 录制器里从不置位，所以冲突的原因必然是**彼此**而不是撞上某条默认。

`table.test.ts` 四条：两条命令都在表里且出厂无键、空键不占键、分配空闲键合法而分配 Ctrl+Z 仍报重复、
分配之后还能再清掉。

`chords.test.ts` 一条：空键标签是 `None`（带修饰键也一样 —— 原版先判 isNone）。

对等矩阵：U28。原版锚点：`KeyboardShortcuts.swift`、`KeyboardShortcutTests.swift`；提交 `87e2283`。
