# 08 — 黄金基准 harness

**What to build:** 像素内核验收设施：把 reference 区 9 个 C 内核以拷贝件形式纳入测试目录（注明来源提交），C 驱动（参数 JSON → PNG）可在 Windows 原生编译生成参考图库；Go 比对协议（逐位，浮点敏感内核显式 ε≤1/255）；首批参考用例覆盖原版测试典型输入。

**Blocked by:** 01 — 脚手架与开发链路.

**Status:** done

- [x] 9 个内核全部编译通过并有参考 PNG 生成用例
- [x] 参考图库 + 参数 JSON 入库、可复现（固定种子/确定性）
- [x] 比对协议测试：正确实现通过、故意偏差 1 位即失败
- [x] 后续票的"与 C 一致"验收都指向本设施（命令化入口）

对等矩阵：后续所有像素行（A/R/S/T07/T08/C17 等）的验收依托。


**实现说明（2026-10-06）：** `tests/golden/c/`（9 内核逐字节拷贝件，来源提交 865e467，见 c/README.md；`dispatch/dispatch.h` + `golden_dispatch.c` 为 Windows 最小 GCD/blocks 垫片——串行执行保确定性，拷贝件本体未改动；`driver_main.c` 驱动含 levels/exposure/gradient_map 三个 Swift 表构建器的 C 移植与合成 33³ 立方）；`cases/*.json` 16 例覆盖全部 9 内核（驱动 15 个命令入口）；`gen` 命令再生成 `ref/*.png`（已验证重生成零差异）；`golden_test.go` 比对协议（逐位默认、case 显式 ε≤1/255，故意偏差 1 位即失败的协议测试）+ 已移植内核真实比对（levels×2/gradient-map/cube 逐位一致、exposure ε=1 一致）+ 未移植内核具名跳过（票 26/27 落地即自动启用）。发现并修正：Go 内核中间算术须为 float32 才能与 C 内核逐位对齐（ApplyLUT/ApplyCube 已改）。