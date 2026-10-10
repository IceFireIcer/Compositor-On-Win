// agentdemo 是 .comp 代理写入契约（reference/Swift/docs/writing-comp-files.md）
// 的官方参考实现：一个不依赖任何第三方库、只写标准库 PNG 的最小代理。
//
//	go run ./scripts/agentdemo -dir "C:\path\to\demo.comp"
//	go run ./scripts/agentdemo -dir "C:\path\to\demo.comp" -steps 3 -pause 400ms
//
// 第一次运行会建出包（目录 + images/ + manifest.json），画一个低对比度的小场景：
// 一个渐变背景图层，加 1–3 个柔和的渐变光斑图层。之后每次运行都复用已有包的
// documentID 与图层 id：覆盖同名图层的 PNG、把新图层追加到顶部，再写回清单。
// 每步改动之间按 -pause 停顿，方便在应用里看着画布一步步更新。
//
// 写入顺序遵循契约，任何时候被应用读到都不会是"半成品"：
//
//  1. 先把新的 PNG 写进 images/（每个都走临时文件 + 原子改名）；
//  2. 再把清单写到包内的 .manifest.json.tmp，最后一次 rename 覆盖 manifest.json；
//  3. 最后删掉清单不再引用的旧 PNG。
//
// 退出码：参数或写入出错时打印中文错误并以非零退出，不 panic。
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"compositor-win/internal/domain"
	"compositor-win/internal/fsatomic"
	"compositor-win/internal/project"
)

const (
	// defaultWidth / defaultHeight 是 16:9 的默认画布。
	defaultWidth  = 960
	defaultHeight = 540
	// defaultSeed 固定，保证不传 -seed 时逐字节可复现。
	defaultSeed = int64(20240517)
	// defaultResolution 是 macOS 新文档的默认 ppi。
	defaultResolution = 72

	// manifestTmpName 是包内清单暂存文件名。契约建议用 .manifest.json.tmp：
	// 以点开头不会被当成 PNG，也不会和 images/ 里的资产撞名。
	manifestTmpName = ".manifest.json.tmp"
	// manifestName 是清单的最终名字。
	manifestName = "manifest.json"

	// uuidNamespace 是派生确定性 UUID 的命名空间前缀，避免与其它工具生成的 id 混淆。
	uuidNamespace = "compositor-win/scripts/agentdemo"

	// maxOrbs 是场景里最多能追加几个光斑图层。
	maxOrbs = 3

	// renameTimeout 是 manifest.json 改名的重试总时长：Windows 上杀毒/索引/
	// OneDrive 同步可能短暂持有文件句柄，让改名以"拒绝访问"失败；短暂重试比
	// 直接失败更贴近真实桌面环境。
	renameTimeout  = 2 * time.Second
	renameAttempts = 40
)

// errMissingDir 是 -dir 缺失时的错误，单独导出以便测试用 errors.Is 断言。
var errMissingDir = errors.New("-dir 是必填参数，请给出 .comp 包的目录")

// Config 是一次运行的全部输入。零值不可用，请用 DefaultConfig 再改字段。
type Config struct {
	// Dir 是 .comp 包的目录（不存在则创建）。
	Dir string
	// Steps 是连续执行的改动步数，必须 ≥ 1。
	Steps int
	// Pause 是两步之间的停顿，方便人在应用里看到每一版。0 表示不停顿。
	Pause time.Duration
	// Width / Height 是画布尺寸（首次运行时写进清单）。
	Width  int
	Height int
	// Seed 决定所有像素与派生 id，保证同一 seed 逐字节可复现。
	Seed int64
}

// DefaultConfig 返回 CLI 默认值（Dir 留空，由调用方填）。
func DefaultConfig() Config {
	return Config{
		Steps:  1,
		Pause:  0,
		Width:  defaultWidth,
		Height: defaultHeight,
		Seed:   defaultSeed,
	}
}

// Validate 检查参数。返回的错误是中文的，且能直接展示给使用者。
func (c Config) Validate() error {
	if strings.TrimSpace(c.Dir) == "" {
		return errMissingDir
	}
	if c.Steps < 1 {
		return fmt.Errorf("-steps 必须 ≥ 1（得到 %d）：至少要写一步", c.Steps)
	}
	if c.Pause < 0 {
		return fmt.Errorf("-pause 不能为负（得到 %s）", c.Pause)
	}
	if c.Width < 1 || c.Height < 1 {
		return fmt.Errorf("-width/-height 必须为正数（得到 %d × %d）", c.Width, c.Height)
	}
	if c.Width > domain.MaxSide || c.Height > domain.MaxSide {
		return fmt.Errorf("-width/-height 超出 1–%d px（得到 %d × %d）", domain.MaxSide, c.Width, c.Height)
	}
	if int64(c.Width)*int64(c.Height) > domain.MaxSurfacePixels {
		return fmt.Errorf("-width/-height 的像素数 %d 超过 %d 上限", int64(c.Width)*int64(c.Height), domain.MaxSurfacePixels)
	}
	return nil
}

// StepReport 描述一步往包里写了什么，供 CLI 打印。
type StepReport struct {
	// Index 是场景步号（从 1 开始）；再次运行会接着上一次的进度继续编号。
	Index int
	// Created 表示这一步建出了新包（而不是改已有包）。
	Created bool
	// Added 是这一步新增的图层名（自下而上）。
	Added []string
	// Updated 是这一步覆盖了像素的图层名（自下而上）。
	Updated []string
	// LayerCount 是这一步写完后包里的图层总数。
	LayerCount int
	// Assets 是这一步写进 images/ 的 PNG 文件名。
	Assets []string
	// Detail 是一句话总结，CLI 直接打印。
	Detail string
}

// Result 是一次 Run 的汇总。
type Result struct {
	// Dir 是最终写入的包目录。
	Dir string
	// DocumentID 是包的身份，跨运行保持不变。
	DocumentID string
	// Steps 是每一步的报告。
	Steps []StepReport
}

// OpenHook 在 manifest.json 改名提交之前被调用（写入安全顺序的观察点）。
// dir 是包目录；此时旧的 manifest.json 仍完整可读，.manifest.json.tmp 已写好。
// 钩子返回错误会中止本次写入。
type OpenHook func(dir string) error

// Run 依次执行 cfg.Steps 步改动，返回每一步写了什么。
//
// 步号是"场景进度"而不是"本次调用的第几步"：如果包里已经有光斑 1，那么本次
// 的第一条改动就是场景的第 2 步（追加光斑 2）。这样反复调用 -steps 1 会一步步
// 把场景搭起来，而不是每次重画同一版。
//
// hook 为 nil 时不做任何观察；测试用它断言"改名之前旧清单仍然完整可读"。
// 任一步失败都会立即返回错误，并且此时包里的清单要么是旧的、要么是新的。
func Run(cfg Config, hook OpenHook) (*Result, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	offset, err := stepOffset(cfg.Dir)
	if err != nil {
		return nil, err
	}
	// fresh 表示这次运行是从"包还不存在"开始的；只有这种运行的第一步算"新建包"。
	fresh := offset == 0
	res := &Result{Dir: cfg.Dir}
	for i := 1; i <= cfg.Steps; i++ {
		step := offset + i
		report, docID, err := runStep(cfg, step, fresh, hook)
		if err != nil {
			return nil, fmt.Errorf("第 %d 步写入失败: %w", step, err)
		}
		res.DocumentID = docID
		res.Steps = append(res.Steps, report)
		if cfg.Pause > 0 && i < cfg.Steps {
			time.Sleep(cfg.Pause)
		}
	}
	return res, nil
}

// stepOffset 返回已有包"已经完成到第几步"：场景第 k 步的状态是背景 + k 个光斑，
// 所以已连续存在的光斑数就是已完成的步数。空包（或只有背景）返回 0，下一次调用
// 就从第 1 步开始。这样同一目录反复运行会持续推进场景，而不是每次从第 1 步重画。
func stepOffset(dir string) (int, error) {
	doc, err := openExisting(dir)
	if err != nil {
		return 0, err
	}
	if doc == nil {
		return 0, nil
	}
	// 从第一个光斑起连续数：有几个光斑，场景就走到第几步。
	steps := 0
	for i := 0; i < maxOrbs; i++ {
		found := false
		for j := range doc.Layers {
			if doc.Layers[j].Name == orbSpecs[i].name {
				found = true
				break
			}
		}
		if !found {
			break
		}
		steps++
	}
	return steps, nil
}

// runStep 执行一步：打开已有包（若有）→ 生成像素 → 安全顺序写回。
// fresh 为真表示这次运行面对的是一个还不存在的包（首次运行）。
func runStep(cfg Config, step int, fresh bool, hook OpenHook) (StepReport, string, error) {
	existing, err := openExisting(cfg.Dir)
	if err != nil {
		return StepReport{}, "", err
	}
	if existing != nil && (existing.Width != cfg.Width || existing.Height != cfg.Height) {
		return StepReport{}, "", fmt.Errorf(
			"已有包的画布是 %d × %d，与本次的 %d × %d 不一致；改画布尺寸不是本示例支持的改动",
			existing.Width, existing.Height, cfg.Width, cfg.Height)
	}

	// 像素完全由 seed 与步号决定：同一 seed 反复运行逐字节一致。
	plan := buildPlan(cfg, step)

	doc, report, err := buildDocument(cfg, existing, plan)
	if err != nil {
		return StepReport{}, "", err
	}
	if existing == nil {
		doc.DocumentID = deriveUUID("document", cfg.Seed)
	} else {
		doc.DocumentID = existing.DocumentID
	}
	// 报告里只有"从无到有"的那一步算新建包；再次运行只是改已有包。
	report.Created = fresh && step == 1
	if len(doc.Layers) > 0 {
		active := doc.Layers[len(doc.Layers)-1].ID
		doc.ActiveLayerID = &active
	}

	assets, err := renderAssets(cfg.Dir, doc, plan)
	if err != nil {
		return StepReport{}, "", err
	}
	if err := writePackage(cfg.Dir, doc, assets, hook); err != nil {
		return StepReport{}, "", err
	}

	report.Index = step
	report.LayerCount = len(doc.Layers)
	report.Assets = sortedAssetNames(assets)
	report.Detail = describe(report)
	return report, doc.DocumentID, nil
}

// openExisting 读取目录里已有的包。目录不存在或没有 manifest.json 都返回
// (nil, nil)：那是"首次运行"。
func openExisting(dir string) (*domain.Document, error) {
	if _, err := os.Stat(filepath.Join(dir, manifestName)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("检查 %s 失败: %w", manifestName, err)
	}
	doc, err := (&project.Store{}).Open(dir)
	if err != nil {
		return nil, fmt.Errorf("读取已有包失败（请确认 %s 是有效的 .comp 包）: %w", dir, err)
	}
	return doc, nil
}

// planLayer 是这一步计划里的一个图层：确定性 id + 要渲染的像素 + 变换。
type planLayer struct {
	id        string
	name      string
	img       *image.RGBA
	origin    [2]float64
	blendMode domain.BlendMode
}

// buildPlan 给出第 step 步应有的场景，自下而上：
// 背景恒定存在，之后每多一步就多一个光斑，最多 maxOrbs 个。
func buildPlan(cfg Config, step int) []planLayer {
	bg := renderBackground(cfg.Width, cfg.Height, cfg.Seed)
	plan := []planLayer{{
		id:        deriveUUID("background", cfg.Seed),
		name:      backgroundLayerName,
		img:       bg,
		origin:    [2]float64{0, 0},
		blendMode: domain.BlendNormal,
	}}
	// 第 1 步建出背景 + 第 1 个光斑；此后每多一步多一个光斑。
	want := step
	if want > maxOrbs {
		want = maxOrbs
	}
	for i := 0; i < want; i++ {
		spec := orbSpecs[i]
		bounds := orbGeometry(cfg, spec)
		plan = append(plan, planLayer{
			id:        deriveUUID(fmt.Sprintf("orb-%d", i+1), cfg.Seed),
			name:      spec.name,
			img:       renderOrb(spec, bounds),
			origin:    [2]float64{float64(bounds.Min.X), float64(bounds.Min.Y)},
			blendMode: spec.blendMode,
		})
	}
	return plan
}

// orbGeometry 把参考画布上的光斑矩形缩放到实际画布：等比缩放位置与尺寸，并保证
// 宽高至少 1px（transform.size 不允许为 0）。
func orbGeometry(cfg Config, spec orbSpec) image.Rectangle {
	scale := ratio(cfg.Width, defaultWidth)
	if s := ratio(cfg.Height, defaultHeight); s < scale {
		scale = s
	}
	x0 := int(math.Round(float64(spec.x0) * scale))
	y0 := int(math.Round(float64(spec.y0) * scale))
	w := int(math.Round(float64(spec.w) * scale))
	h := int(math.Round(float64(spec.h) * scale))
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	return image.Rect(x0, y0, x0+w, y0+h)
}

// buildDocument 把计划落到清单上：
//   - 已有图层（包括应用里自己加的）全部原样保留；
//   - 计划里的图层按名字找：找到就原地更新（保留 id 与图片名），没找到就追加到顶部；
//   - 图层顺序始终自下而上。
func buildDocument(cfg Config, existing *domain.Document, plan []planLayer) (*domain.Document, StepReport, error) {
	doc := &domain.Document{
		Format:     domain.FormatID,
		Version:    domain.FormatVersion,
		ColorSpace: domain.ColorSpaceSRGB,
		Width:      cfg.Width,
		Height:     cfg.Height,
	}
	if existing != nil {
		// 保留未知/额外字段：只覆盖本示例负责的部分。
		*doc = *existing
		doc.Version = domain.FormatVersion
		doc.Width = cfg.Width
		doc.Height = cfg.Height
	}
	resolution := defaultResolution
	if existing != nil && existing.Resolution != nil {
		resolution = *existing.Resolution
	}
	doc.Resolution = &resolution

	var report StepReport
	indexByName := make(map[string]int, len(doc.Layers))
	for i := range doc.Layers {
		indexByName[doc.Layers[i].Name] = i
	}
	planned := make([]domain.Layer, 0, len(plan))
	planNames := make(map[string]bool, len(plan))
	for _, p := range plan {
		planNames[p.name] = true
		if i, ok := indexByName[p.name]; ok {
			// 复用已有图层：保住 id 与图片名，只覆盖像素与外观。
			layer := doc.Layers[i]
			applyPlan(&layer, p, existing != nil)
			planned = append(planned, layer)
			report.Updated = append(report.Updated, p.name)
			continue
		}
		layer := domain.Layer{ID: p.id, Name: p.name}
		applyPlan(&layer, p, existing != nil)
		planned = append(planned, layer)
		report.Added = append(report.Added, p.name)
	}
	// 层序自下而上：本示例负责的图层在底部（背景永远在最底），应用自己加的
	// 图层保持相对顺序留在顶部。
	kept := make([]domain.Layer, 0, len(doc.Layers))
	for i := range doc.Layers {
		if !planNames[doc.Layers[i].Name] {
			kept = append(kept, doc.Layers[i])
		}
	}
	doc.Layers = append(planned, kept...)
	return doc, report, nil
}

// applyPlan 把计划里的像素尺寸与外观写进图层记录。keepID 为真时保留图层原有的
// id（以及由 id 推出的图片名），这正是契约要求的"改已有图层要保住身份"。
func applyPlan(layer *domain.Layer, p planLayer, keepID bool) {
	if !keepID {
		layer.ID = p.id
	}
	imageFile := strings.ToUpper(layer.ID) + ".png"
	layer.ImageFile = &imageFile
	layer.IsVisible = true
	layer.IsGroup = boolPtr(false)

	opacity := orbOpacity
	if p.blendMode == domain.BlendNormal {
		opacity = 1
	}
	layer.Opacity = &opacity
	blend := p.blendMode
	layer.BlendMode = &blend

	bounds := p.img.Bounds()
	layer.Transform = domain.Transform{
		Origin:   p.origin,
		Size:     [2]float64{float64(bounds.Dx()), float64(bounds.Dy())},
		Rotation: 0,
		FlipX:    false,
		FlipY:    false,
		Sampling: domain.SamplingHighQuality,
	}
}

// renderAssets 生成 doc 引用的全部 PNG 字节。
//
// 计划内的图层用刚渲染好的像素；其余图层（应用自己加的）从磁盘读回原字节，
// 这样一次写入不会动别人的图像。
func renderAssets(dir string, doc *domain.Document, plan []planLayer) (map[string][]byte, error) {
	pixels := make(map[string]*image.RGBA, len(plan))
	for _, p := range plan {
		pixels[strings.ToUpper(p.id)+".png"] = p.img
	}
	assets := make(map[string][]byte, len(doc.Layers))
	for i := range doc.Layers {
		name := assetNameOf(&doc.Layers[i])
		if name == "" {
			continue
		}
		if img, ok := pixels[name]; ok {
			data, err := encodePNG(img)
			if err != nil {
				return nil, fmt.Errorf("编码图层 %s 的 PNG 失败: %w", doc.Layers[i].Name, err)
			}
			assets[name] = data
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, imagesDirName, name))
		if err != nil {
			return nil, fmt.Errorf("读取图层 %s 的图片 %s 失败: %w", doc.Layers[i].Name, name, err)
		}
		assets[name] = data
	}
	return assets, nil
}

// assetNameOf 返回图层的图片文件名（没有图片则空）。
func assetNameOf(l *domain.Layer) string {
	if l.ImageFile == nil {
		return ""
	}
	return *l.ImageFile
}

// encodePNG 用标准库把 RGBA 位图编码成 8-bit RGBA PNG（颜色类型 6）。
func encodePNG(img *image.RGBA) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writePackage 以契约的安全顺序写包：
//
//  1. images/ 下的每个 PNG 先写临时文件再原子改名；
//  2. 清单写进包内的 .manifest.json.tmp；
//  3. hook（若有）在提交前观察——此刻旧 manifest.json 仍然完整可读；
//  4. 一次 rename 覆盖 manifest.json（Windows 上 MoveFileEx 带 REPLACE_EXISTING，
//     所以是原子替换，不存在"没有清单"的窗口）；
//  5. 删掉清单不再引用的旧 PNG。
func writePackage(dir string, doc *domain.Document, assets map[string][]byte, hook OpenHook) error {
	manifest, err := encodeManifest(doc)
	if err != nil {
		return err
	}
	imagesDir := filepath.Join(dir, imagesDirName)
	if err := os.MkdirAll(imagesDir, 0o755); err != nil {
		return fmt.Errorf("创建 %s 目录失败: %w", imagesDirName, err)
	}
	for name, data := range assets {
		if !assetNamePattern.MatchString(name) {
			return fmt.Errorf("资产名 %q 不符合 <大写UUID>.png 约定", name)
		}
		if err := fsatomic.WriteFile(filepath.Join(imagesDir, name), data, 0o644); err != nil {
			return fmt.Errorf("写入图像 %s 失败: %w", name, err)
		}
	}

	// 清单先落到包内的暂存文件，再一次 rename 覆盖 manifest.json。改名之前旧清单
	// 原样在盘上，所以此刻钩子（若有）仍能读到完整可读的旧包。
	//
	// 这里没有直接用 fsatomic.WriteFile：契约要求暂存文件叫 .manifest.json.tmp，
	// 而 fsatomic 只提供"<path>.tmp + 改名"的整包写法，改名那一步不单独暴露。
	tmpPath := filepath.Join(dir, manifestTmpName)
	if err := os.WriteFile(tmpPath, manifest, 0o644); err != nil {
		return fmt.Errorf("写入清单暂存文件失败: %w", err)
	}
	if hook != nil {
		if err := hook(dir); err != nil {
			_ = os.Remove(tmpPath)
			return fmt.Errorf("提交前观察失败: %w", err)
		}
	}
	if err := commitManifest(tmpPath, filepath.Join(dir, manifestName)); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("提交 manifest.json 失败: %w", err)
	}
	removeStaleAssets(imagesDir, doc)
	return nil
}

// commitManifest 把暂存清单改名成 manifest.json。Windows 上 rename 直接覆盖已
// 存在的目标（MoveFileEx + MOVEFILE_REPLACE_EXISTING），所以不需要"先删旧文件"——
// 那一步在改名失败时会直接把清单删没。杀毒/索引/云同步可能短暂持有句柄让改名报
// "拒绝访问"，因此带一点重试；重试耗尽时旧清单原样保留。
func commitManifest(from, to string) error {
	// 改名与重试策略统一在 internal/fsatomic（同目录一次 rename 覆盖 + 短重试），
	// 免得这里再养一套"什么算瞬态错误"的判断。
	if err := fsatomic.Replace(from, to); err != nil {
		return fmt.Errorf("提交清单失败: %w", err)
	}
	return nil
}

// removeStaleAssets 删除 images/ 里清单已不再引用的 PNG。只碰严格符合
// <大写UUID>.png / <大写UUID>.mask.png 的文件，临时文件和外来文件一律不动。
func removeStaleAssets(imagesDir string, doc *domain.Document) {
	referenced := make(map[string]bool, len(doc.Layers))
	for i := range doc.Layers {
		if name := assetNameOf(&doc.Layers[i]); name != "" {
			referenced[name] = true
		}
	}
	entries, err := os.ReadDir(imagesDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if !assetNamePattern.MatchString(name) || referenced[name] {
			continue
		}
		_ = os.Remove(filepath.Join(imagesDir, name))
	}
}

// encodeManifest 把清单编码成与项目存储一致的样式：两空格缩进、HTML 不转义、
// 结尾无换行。键序按 encoding/json 的字典序，与 macOS 的 .sortedKeys 一致。
func encodeManifest(doc *domain.Document) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("清单无法编码: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// deriveUUID 从 (用途, seed) 派生一个稳定的 UUID v4 形状字符串。
// 用派生而不是随机：同一 -seed 反复运行会得到同样的 documentID 与图层 id，
// 这样测试才能断言"复用身份"。
func deriveUUID(purpose string, seed int64) string {
	key := fmt.Sprintf("%s/%s/%d", uuidNamespace, purpose, seed)
	sum := sha256.Sum256([]byte(key))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x40 // 版本 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 变体
	return fmt.Sprintf("%08X-%04X-%04X-%04X-%012X",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// describe 把一步的报告压成一行，说明往包里写了什么。
func describe(r StepReport) string {
	action := "更新"
	if r.Created {
		action = "新建包并写入"
	}
	var parts []string
	if len(r.Added) > 0 {
		parts = append(parts, "新增图层 "+strings.Join(r.Added, "、"))
	}
	if len(r.Updated) > 0 {
		parts = append(parts, "覆盖像素 "+strings.Join(r.Updated, "、"))
	}
	if len(parts) == 0 {
		parts = append(parts, "无改动")
	}
	return fmt.Sprintf("%s %s；图层自下而上 %d 个；images/ 写入 %d 个 PNG（%s）",
		action, strings.Join(parts, "；"), r.LayerCount, len(r.Assets), strings.Join(r.Assets, ", "))
}

// sortedAssetNames 返回排序后的资产名，让打印与断言稳定。
func sortedAssetNames(assets map[string][]byte) []string {
	names := make([]string, 0, len(assets))
	for name := range assets {
		names = append(names, name)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}

// boolPtr 是可选布尔字段的小工具（清单里 isGroup 用指针表示"可缺省"）。
func boolPtr(v bool) *bool { return &v }

// parseConfig 解析命令行参数。与 Run 分开，方便测试直接构造 Config。
func parseConfig(args []string, stderr *os.File) (Config, error) {
	cfg := DefaultConfig()
	fs := flag.NewFlagSet("agentdemo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&cfg.Dir, "dir", "", "要写入的 .comp 包目录（必填）")
	fs.IntVar(&cfg.Steps, "steps", cfg.Steps, "连续执行的改动步数（≥ 1）")
	fs.DurationVar(&cfg.Pause, "pause", cfg.Pause, "两步之间的停顿，例如 400ms")
	fs.IntVar(&cfg.Width, "width", cfg.Width, "画布宽度（首次运行写进清单）")
	fs.IntVar(&cfg.Height, "height", cfg.Height, "画布高度（首次运行写进清单）")
	fs.Int64Var(&cfg.Seed, "seed", cfg.Seed, "随机种子，同一种子逐字节可复现")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "用法: go run ./scripts/agentdemo -dir <包目录> [-steps N] [-pause 400ms]\n")
		fmt.Fprintf(stderr, "场景: %d × %d 的低对比度画面（渐变背景 + 最多 %d 个柔和光斑），默认值可用 -width/-height 改\n\n",
			cfg.Width, cfg.Height, maxOrbs)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func main() {
	cfg, err := parseConfig(os.Args[1:], os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentdemo: %v\n", err)
		fmt.Fprintln(os.Stderr, "提示: go run ./scripts/agentdemo -dir \"C:\\path\\to\\demo.comp\" -steps 3 -pause 400ms")
		os.Exit(2)
	}
	res, err := Run(cfg, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agentdemo: %v\n", err)
		os.Exit(1)
	}
	for _, step := range res.Steps {
		fmt.Printf("第 %d 步：%s\n", step.Index, step.Detail)
	}
	fmt.Printf("完成：%s（documentID %s，共 %d 步）\n", res.Dir, res.DocumentID, len(res.Steps))
}

// ---------------------------------------------------------------------------
// 场景生成：只依赖标准库 image / image/png，画一个低对比度的小场景。
// ---------------------------------------------------------------------------

// imagesDirName 是包内存放 PNG 的固定子目录。
const imagesDirName = "images"

// backgroundLayerName 是背景图层的名字，也是"复用已有图层"的匹配键。
const backgroundLayerName = "背景"

// orbOpacity 是光斑图层的整体不透明度，配合柔和的径向渐变做出低对比度。
const orbOpacity = 0.55

// assetNamePattern 是资产的严格拼写：大写 UUID + .png（可带 .mask）。
// 与 internal/project 的资产命名规则一致。
var assetNamePattern = regexp.MustCompile("^[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}(\\.mask)?\\.png$")

// orbSpec 描述一个光斑图层在画布上的位置与颜色。
type orbSpec struct {
	name      string
	x0, y0    int
	w, h      int
	cx, cy    float64
	radius    float64
	r, g, b   uint8
	blendMode domain.BlendMode
}

// orbSpecs 是最多三个光斑的编排：左上一个偏冷、右下两个偏暖，都低对比度。
// 坐标是参考画布（defaultWidth × defaultHeight）上的值，实际画布不同时按比例缩放
// （见 orbGeometry），所以小画布不会把光斑裁掉，大画布也不会让光斑缩在角落。
var orbSpecs = []orbSpec{
	{
		name: "光斑 1", x0: 80, y0: 60, w: 460, h: 320,
		cx: 200, cy: 140, radius: 210,
		r: 96, g: 128, b: 168, blendMode: domain.BlendScreen,
	},
	{
		name: "光斑 2", x0: 420, y0: 200, w: 440, h: 300,
		cx: 210, cy: 150, radius: 190,
		r: 168, g: 120, b: 96, blendMode: domain.BlendSoftLight,
	},
	{
		name: "光斑 3", x0: 200, y0: 300, w: 380, h: 220,
		cx: 180, cy: 105, radius: 150,
		r: 120, g: 152, b: 120, blendMode: domain.BlendOverlay,
	},
}

// renderBackground 画一个从上到下、从左到右都极缓的深蓝灰渐变，再叠一点
// 由 seed 决定的细微噪点：整体对比度很低，适合做底。
func renderBackground(width, height int, seed int64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	rng := rand.New(rand.NewSource(seed ^ 0x5eed0001))
	for y := 0; y < height; y++ {
		ty := ratio(y, height)
		for x := 0; x < width; x++ {
			tx := ratio(x, width)
			// 顶亮底暗（约 26 级灰度差），左侧略亮于右侧。
			base := 0.20 + 0.10*(1-ty) + 0.02*(1-tx)
			r := clamp01(base + 0.02)
			g := clamp01(base + 0.035)
			b := clamp01(base + 0.075)
			img.SetRGBA(x, y, jitter(r, g, b, rng, 2))
		}
	}
	return img
}

// renderOrb 画一个带柔和径向渐变的光斑，尺寸取自 bounds（已按画布缩放）。半径外
// 全透明，半径内从中心到边缘平滑淡出；像素只依赖自身坐标，保证同一 seed 下逐字节
// 可复现。
func renderOrb(spec orbSpec, bounds image.Rectangle) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	// 缩放后半径按同一比例走，渐变形状才不会变。
	scale := ratio(bounds.Dx(), spec.w)
	radius := spec.radius * scale
	if radius < 1 {
		radius = 1
	}
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			dx := float64(x) + 0.5 - spec.cx*scale
			dy := float64(y) + 0.5 - spec.cy*scale
			d := math.Sqrt(dx*dx+dy*dy) / radius
			if d >= 1 {
				continue // 保持全透明
			}
			t := smoothstep(1 - d) // 中心 1，边缘 0
			alpha := t * t         // 再压一次，让边缘更软
			// 中心略亮，边缘接近基色。
			lift := 0.85 + 0.15*t
			img.SetRGBA(x, y, color.RGBA{
				R: clampByte(float64(spec.r) * lift * alpha),
				G: clampByte(float64(spec.g) * lift * alpha),
				B: clampByte(float64(spec.b) * lift * alpha),
				A: clampByte(255 * alpha),
			})
		}
	}
	return img
}

// ratio 返回 i/n，n 为 0 时返回 0。
func ratio(i, n int) float64 {
	if n <= 0 {
		return 0
	}
	return float64(i) / float64(n)
}

// smoothstep 把 [0,1] 映射成平滑的 S 曲线，两端导数为 0。
func smoothstep(t float64) float64 {
	t = clamp01(t)
	return t * t * (3 - 2*t)
}

// clamp01 把 v 限制在 [0,1]。
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// clampByte 把 0–255 的浮点值转成字节，并做四舍五入与截断。
func clampByte(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// jitter 给颜色加 ±amount 级的确定性噪点，抑制渐变色带。
func jitter(r, g, b float64, rng *rand.Rand, amount int) color.RGBA {
	n := func() int {
		if amount <= 0 {
			return 0
		}
		return rng.Intn(2*amount+1) - amount
	}
	return color.RGBA{
		R: clampByte(r*255 + float64(n())),
		G: clampByte(g*255 + float64(n())),
		B: clampByte(b*255 + float64(n())),
		A: 255,
	}
}
