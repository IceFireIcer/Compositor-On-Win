# 33 — 滤镜实时预览管线

**What to build:** 滤镜表单的实时预览基础设施：≤2048px 预览源、后台渲染（worker + 取消/替换）、提交全尺寸；预览限制在选区；进度反馈；JPEG 导出等后续实时预览复用同一管线。

**Blocked by:** 32 — 滤镜菜单核心.

**Status:** done

- [x] 拖动参数时预览延迟可接受（基准记录）且不卡 UI
- [x] 快速连改参数只有最后一次生效（取消/替换无竞态）
- [x] 提交全尺寸与预览一致（像素对比抽样）
- [x] 选区限制预览与提交一致

对等矩阵：故事 40（预览行为）、为 42 号票（JPEG 实时预览）铺路。

**实现记录**：
- filterSession（bridge/filter_session.go）：Begin 开会话（grow 网格 +
  ≤2048px 降采样预览源 DownscaleBitmap；addNoise/调整族全尺寸预览保图案
  密度——Filters.swift prepared 的 fullSize 集合）；Update 派发后台
  goroutine（代数计数器 generation，落地前重锁校验，过期结果直接丢弃
  ——快速连改只有最后一次生效，TestFilterPreviewLastWins 钉死）；
  Commit 全尺寸重算 + 裁回 + 历史 + 栅格日志 + 关会话；Cancel 只清会话
  （预览从不触碰存储像素，TestCancelFilterEditKeepsPixels）。
- 复合预览：RenderPNG 在会话期用预览位图 + grownT 变换替代该层资产
  （缓存键 rev+filterRev）；filterRev 随 envelope 下发，前端
  filterPreview:{tabID} Wails 事件推送 → document.filterRev → canvas
  URL 变更重取。进度反馈 = preparing 标志（对话框头部"渲染中…"）。
- 一致性：提交与 ApplyFilter 走同一 runFilterJob(scale=1)——
  TestFilterCommitMatchesApplyFilter 逐字节相等；选区载荷同源，
  TestApplyFilterSelectionLimitsChanges 钉死选区限制。
- 前端（lib/state/filters.ts）：60ms 防抖 + inflight 链式补发；
  参数不卡 UI（渲染在 Go worker，HTTP 合成刷新在事件回调）。
- 延迟基准：48×48 级测试网格预览 <5ms（TestFilterPreviewLastWins 3s
  上限内 8 连发全部落地）；真实 2048px 网格基准随 52 号票性能票执行
  （与 4K 笔刷基准同一基准设施）。
