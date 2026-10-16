# 80 — Camera Raw 分区重置

**What to build:** 用户可从 Camera Raw 每个分区标题独立重置该分区，其他设置保持不变。

**Blocked by:** 28 — Camera Raw：光/色/曲线; 34 — Camera Raw 面板与示波器。

**Status:** in-progress（2026-10-16；Go 侧的分区逻辑已落地并有测试，面板接线待做）

- [x] 十个分区逐一恢复滑块、曲线、选择项和中点默认值，其他分区不变。
      `internal/render/cameraraw_reset.go`：`ResetGroup` / `KeepingGroup` / `IsGroupDefault`，
      对应上游的 `resetting` / `keeping` / `isDefault`。**面板接线待做**。
- [ ] 重置后该分区重新显示；混色器点索引归零、几何引导草稿清除。**待做**（上游在
      `EditorSession.resetCameraRaw` 里：`setShows(group, true)`、混色器点索引 0、几何引导草稿清空）。
- [ ] 重置是一次可撤销的预览更新，提交/取消不会污染其他分区。**待做**（走既有的
      `updateFilterSettings` 预览通道即可，它本来就落在滤镜会话的提交/取消里）。

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
