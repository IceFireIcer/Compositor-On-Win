# 历史黄金样本（不参与 harness）

本目录保存**已被上游取代**的黄金基准：它们的 C 内核在当前 pin 的
`reference/Swift` 里已不存在或签名已变（票 63、65），因此无法再驱动，
也就不再是 `cases/` + `ref/` 的活样本。保留它们是票面的要求：
「旧 `ContentFill.c` 黄金样本作为历史基准保留」、「旧 1.4.5 黄金样本的数值变化单独记录」。

## 内容

| 文件 | 来源 | 说明 |
| --- | --- | --- |
| `camera-raw-1.4.5.{json,png}` | 原 `cases/camera-raw.json` + `ref/camera-raw.png` | Camera Raw Light/Color 组，直接参数版 `adjust_camera_raw`（拷贝来源提交 `865e467`） |
| `content-fill-1.4.5.{json,png}` | 原 `cases/content-fill.json` + `ref/content-fill.png` | 单尺度 `content_fill`（上游 1.4.10 已删除该内核） |
| `ContentFill.{c,h}` | `git show HEAD:tests/golden/c/ContentFill.c` | 上游删除前的内核拷贝，来源提交 `865e467` |

## 记录的数值变化（1.4.5 → 1.4.9/1.4.10）

### camera-raw（Light/Color 组）

1.4.9 起 `adjust_camera_raw` 不再直接接收滑块，而是消费一张**由测量数据合成的查表**
（`CameraRawTables.bin` + `camera_raw_compose`，即票 78/90 的范围）。Go 侧仍保留直接内核，
并按票 63 对齐了高光/阴影的曲线（`tone_bump`，不再颠倒明暗顺序）。

用同一输入与同一组参数比较「旧 1.4.5 参考图」与「对齐后的 Go 直接内核」，实测：

```
max Δ147，mean Δ1，9216 字节中 2798 字节发生变化
```

这条记录由 `TestLegacyCameraRawBaselineDrift`（`tests/golden/legacy_test.go`）钉住：
重新运行 `go test ./tests/golden/ -run TestLegacyCameraRawBaselineDrift -v` 可复现。
差异集中在高光/阴影为负值或正值较大的像素上，符合 0b4c56c/75147a2 的曲线替换。

### content-fill

1.4.10 用多尺度 PatchMatch（`PatchPixels.c` / `patch_fill`）取代了 `ContentFill.c` 的
单尺度补丁复制。旧样本保留在此，新的活样本是 `cases/patch-fill-*.json`；上游的对照数字是
「弯曲山脊 + 圆盘」场景下与隐藏真值相差 7 级，而旧的单补丁做法相差 34 级
（见 `tests/golden/c/README.md` 与票 65 的 Go 测试）。

## 复核

```
go test ./tests/golden/ -run TestLegacy -count=1 -v
```
