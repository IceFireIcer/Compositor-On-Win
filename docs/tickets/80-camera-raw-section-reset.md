# 80 — Camera Raw 分区重置

**What to build:** 用户可从 Camera Raw 每个分区标题独立重置该分区，其他设置保持不变。

**Blocked by:** 28 — Camera Raw：光/色/曲线; 34 — Camera Raw 面板与示波器。

**Status:** done（2026-10-16）

- [x] 十个分区逐一恢复滑块、曲线、选择项和中点默认值，其他分区不变。
      `internal/render/cameraraw_reset.go`：`ResetGroup` / `KeepingGroup` / `IsGroupDefault`，
      对应上游的 `resetting` / `keeping` / `isDefault`；面板每个分区标题上有 ↺ 按钮，默认值由
      `Service.CameraRawGroupDefaults` 给出（前端不自己存一份）。
- [x] 重置后该分区重新显示。**混色器点索引与几何引导草稿在本仓没有对应状态**：上游清的是
      `edit.cameraRawPointIndex` 与 `edit.cameraRawGuideDraft`，而我们的面板既没有独立的点索引
      状态，也还没有引导线 UI（票 30 的几何部分尚未做到那一步）。**没有凭空造状态**，如实记录。
- [x] 重置是一次可撤销的预览更新：走既有的 `updateFilterSettings` 预览通道，本来就落在滤镜
      会话的提交/取消里，不写文档。

## 接线（2026-10-16）

`Service.CameraRawGroupDefaults(group)` 只回**该分区**的键（`CameraRawGroupDefaultFields`），
前端把它合并进 `cameraRaw` 并把该分区的眼睛打开。零值也照回：调用方需要看到「现在曝光是 0」，
而不是「没提曝光」—— 否则旧值会留在原地。

键清单（`cameraRawGroupJSONKeys`）与 `copyCameraRawGroup` 覆盖的字段由
`TestGroupJSONKeysMatchTheCopiedFields` 钉在一起：夹具把每个字段都改离默认，于是 `KeepingGroup`
里非零的键就是该分区真正拥有的字段，与清单对不上即失败。

**顺带修掉一类根因**：`render.CameraRawSettings` 及其子结构体原先没有 JSON tag，Go→前端回包
是 Go 的字段名（`"Optics"`），而面板按小写读。去紫边吸管就栽在这上面（上一轮修）。现在这些
结构体都带上了与面板一致的 camelCase tag；该结构体不进 `.comp`（`project` 包里没有它），所以
这不是格式变更。

## 已落地：分区逻辑（2026-10-16）

**默认值不是零值** —— 这是这一票的关键。上游 `CameraRawSettings()` 的属性初始化里有一批非零
默认：曲线的分割点 25/50/75、暗角中点与羽化 50、颗粒大小 25 与粗糙度 50、分级 Blending 50、
锐化半径 10 与细节 25、两个降噪细节 50、配置畸变与暗角 100、紫边/绿边色相端点 270/310 与
60/120、校准 Process 6、几何的 `Off`/`Perspective`、白平衡 `Custom`。**只把字段清零会丢掉它们** ——
那正是「重置之后画面变了样」的来源。`DefaultCameraRawSettings()` 把它们逐条列出，并在注释里
指向上游的行号。

`copyCameraRawGroup` 对应上游的 `CameraRawReset.copy`，十个分区各自复制哪些字段一一对应。

**我自己在这里踩了同一个坑**：`IsGroupDefault` 一开始拿 `KeepingGroup` 与 Go 的**零值**比较，
于是八个有非零默认的分区永远「不算默认」，重置控件永远不收敛。上游比的是 `CameraRawSettings()`
（默认初始化后的值），改成与 `DefaultCameraRawSettings().KeepingGroup(group)` 比较才对。

## 验证

| 用例 | 覆盖 |
| --- | --- |
| `TestResettingOneGroupLeavesTheOthersAlone` | 一个「每个字段都偏离默认」的夹具，逐个分区重置：该分区回到默认，**其余九个逐字段不变** |
| `TestGroupDefaultDetection` | 默认设置下十个分区都算默认；改过的夹具里十个都不算（同时证明夹具覆盖到了每个分区） |
| `TestDefaultCameraRawSettingsKeepTheirNonZeroDefaults` | 21 个非零默认逐个钉住，防止有人「顺手简化」成零值 |

对等矩阵：U27。原版锚点：`CameraRawReset.swift`、`CameraRawResetTests.swift`；提交 `f745d65`、`b8ff682`。
