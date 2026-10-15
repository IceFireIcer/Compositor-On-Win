# 72 — CMYK ICC 校样与色域警告

**What to build:** 用户可选择 ICC 配置，对 RGB 画布进行 CMYK 软打样并查看色域警告。

**Blocked by:** 09 ✅ — CPU 合成器真值; ADR-0008 ✅ — 已获用户确认（2026-10-15）。

**Status:** in-progress（2026-10-15）——校样与色域警告已接到显示，视图菜单可载入并选择 ICC，`internal/colorio` 的校样规则有上游同构用例。仍无随包内置的通用 CMYK 配置。

- [x] 决策写清 RGB 文档真源、ICC 处理与跨平台结果（ADR-0008 第 1–3、5 条）。
- [x] View 菜单可切换印刷校样、色域警告并选/载入 ICC；校样不污染项目 RGB 像素。
- [x] 关闭校样还原原画布显示；切换色域警告不污染保存及 RGB 导出。
- [x] 用 `CMYKTests` 的结构性断言核对配置选择、校样与警告（ADR-0008 §5：不逐位比对）。导入/导出分见 86、87。

## 验证

| 用例 | 覆盖 |
| --- | --- |
| `internal/colorio` `TestProofLookupsShape`、`TestProofWhiteSurvives`、`TestProofMarksAtLeastOneSaturatedPrimary`、`TestProofMarkedLeavesInGamutAlone` | 上游 `proofDullsWhatInkCantPrint` 的结构：白点不动、饱和原色被标、in-gamut 不被改 |
| `internal/colorio` `TestProofRuleFromSyntheticShown`、`TestGamutOutsideRule` | 色域规则（往返位移 > 10 个 8 位单位）脱离 ICC 可测 |
| `internal/bridge` `TestProofingLeavesTheDocumentAndTheFileAlone` | 取校样不推进 rev、不改保存出来的 `.comp` 一个字节 |
| `internal/bridge` `TestCMYKProofLookupsRefusesWhenNoProfileExists` | 没有配置时报错，而不是发明一张表 |
| `CommandPalette.browser.test.mjs`（票 76 那条同源） | 校样是视图状态：面板/菜单只读开关，不进文档 |

## 已知差异

1. **没有随包内置通用 CMYK 配置**（ADR-0008 §4 未完成）：Windows 的 `spool\drivers\color` 自带 USWebCoatedSWOP、CoatedFOGRA39 等，够用；找不到时菜单报错而不是兜一张合成表——合成的 CMYK 配置等于把校样变成空操作，比报错更糟。补齐需要一份许可明确的 ICC，属发行准备。
2. lcms2 与 ColorSync 不逐位一致，验收按 ADR-0008 §5 取结构性断言。
3. 校样在 WebGPU 显示通道采样 33³ 三线性查找表；CPU 合成真值不参与（ADR-0008 §3）。

对等矩阵：U19。原版锚点：`CMYK.swift`、`CMYKTests.swift`；提交 `59025aa`。本票现在仅登记，不授权改色彩空间。
