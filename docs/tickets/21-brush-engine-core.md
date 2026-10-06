# 21 — 笔刷引擎核心

**What to build:** 笔刷模型移植：256px 瓦片、大小/硬度/间距/平滑/不透明度累积、绘制/擦除模式、Shift 直线；抬笔把瓦片补丁提交为不可变栅格快照（未动瓦片共享）并进入撤销；笔刷与橡皮同一管线。

**Blocked by:** 06 — 快照式撤销历史; 09 — CPU 合成器真值; 13 — 视口与画布导航.

**Status:** done

- [x] 硬度/间距/平滑/不透明度累积数值测试（BrushTests 语义）
- [x] 抬笔提交后像素与预览逐位一致
- [x] 未动瓦片跨提交共享（内存断言）；撤销恢复完整笔触
- [x] 瓦片边界交叉笔触无缝（BrushIntersectionTests 语义）

对等矩阵：T06（模型侧）、故事 18。

**实现说明（2026-10-06，并行子代理）：** `internal/render/brush.go`（14 测试）——256px 瓦片、falloff k=2.5、spacing 1.5%/2.5%+0.25px 下限、**不透明度累积=coverage 瓦片+重组时折入（重复 dab 封顶不加深）**、擦除 destination-out、Shift 直线、抬笔不可变快照（未动瓦片 unsafe.SliceData 共享断言）、跨瓦片无缝探针。
