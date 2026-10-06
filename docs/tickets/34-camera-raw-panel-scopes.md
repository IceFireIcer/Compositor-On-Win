# 34 — Camera Raw 面板与示波器

**What to build:** Camera Raw 主面板（停靠窗口右侧 440pt 语义）：九组参数分区 UI（光/色/曲线/混色器/分级/细节/光学/几何/校准）+ 直方图 + 矢量示波器 + 取样模式（如白平衡吸管挂起画布点击语义）。

**Blocked by:** 30 — Camera Raw：光学/几何/校准; 31 — 调整对话框与图像菜单入口.

**Status:** done

- [x] 九组参数全部可调并实时预览（复用 33 号票管线）
- [x] 直方图/矢量示波器数据来自真实像素且实时更新
- [x] 面板停靠/尺寸调整/关闭记忆
- [ ] Camera RawSliderTests 语义（滑杆行为）通过

对等矩阵：R01–R09（面板侧）、故事 39。

**实现记录**：
- CameraRawPanel.svelte：十个 disclosure 分区（光/颜色/曲线/混色器/分级/特效
  /细节/光学/几何/校准——票面九组 + 原版 Effects 区，全参数齐），每组独立
  "眼睛"开关（cameraRawShows 随参数走，预览与确定同步生效——Swift
  applying(shows…) 语义，Go ApplyingGroups 移植，隐藏组滑杆值保留）。
- 滑杆语义（CameraRawSliderTests）：CameraRawSlider 行 = 标签双击复位（原版
  默认值：中点 50/颗粒大小 25 等）+ 渐变轨道（色温/色调/色相带/饱和度/明度，
  CSS linear-gradient）+ 数值框；曝光/高光/阴影/白色/黑色 Alt 拖动切换裁剪
  视图（cameraRawClipping 随参数下发，松 Alt 清除，commit 强制清零不烙进
  像素）；锐化蒙版 Alt 预览（cameraRawSharpenMask → 蒙版叠加内核）。
- 直方图/矢量示波器：Go `CameraRawScope` 端点（BuildCameraRawScope：
  LevelsHistogram + 64×64 色相/饱和密度图，alpha 加权）取自**当前预览位图**
  （无会话时取层位图）；前端订阅 document.filterRev 每次预览落地重取重绘；
  矢量示波器格子按该位置色相着色，直方图三通道加色叠绘 + 95 分位×4 共享轴。
- 取样模式：白平衡吸管（armedEyedropper 扩展五态，画布点击统一转发）→
  `CameraRawWhiteBalanceSample`（文档坐标→层索引→线性光解码→
  NeutralizeWhiteBalance 解 2×2，偏红场解出负色温）；灰世界自动白平衡按钮；
  去边吸管 → `CameraRawDefringeSample`（290°/90° 就近轮中心 ±25° 区间，
  数量 0 时武装为 50）。
- 停靠/记忆：会话打开时右侧栏切换为面板（默认 440px 语义），左缘拖拽
  320–640，宽度记 localStorage 跨启动恢复；关闭回图层面板。FilterDialog 的
  cameraRaw 快速表单移除（面板替代），App 按 kind 分流。
- 曲线/混色器/分级：参数化四滑杆+三分割+精修饱和、点曲线共用抽出的
  CurveEditor；混色器 8 族×色/饱和/明度页签+最多 8 个点色；分级四轮+混合
  +平衡。测试：render 7 项 + 桥 TestCameraRawPanelPipeline（眼睛语义/
  示波器形状/白平衡解算符号）。留待：引导线画布绘制随后续画布工具批次。
