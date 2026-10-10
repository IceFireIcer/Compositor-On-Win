package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"compositor-win/internal/domain"
	"compositor-win/internal/project"
)

// testConfig 返回指向 t.TempDir() 的配置：测试绝不写用户的真实目录。
func testConfig(t *testing.T) Config {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Dir = filepath.Join(t.TempDir(), "demo.comp")
	// 三分之一画布：光斑按比例一起缩小（见 orbGeometry），场景完整可见，像素量只有
	// 默认画布的九分之一，测试因此很快。
	cfg.Width = defaultWidth / 3
	cfg.Height = defaultHeight / 3
	return cfg
}

// openPackage 用真实加载器打开包，打不开就直接失败。
func openPackage(t *testing.T, dir string) *domain.Document {
	t.Helper()
	doc, err := (&project.Store{}).Open(dir)
	if err != nil {
		t.Fatalf("Store{}.Open(%q) 失败: %v", dir, err)
	}
	return doc
}

// runSteps 执行若干步，失败即终止测试。
func runSteps(t *testing.T, cfg Config, hook OpenHook) *Result {
	t.Helper()
	res, err := Run(cfg, hook)
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	return res
}

// entryNames 列出目录下的条目名，便于断言"没有残留临时文件"。
func entryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取目录 %q 失败: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// assertNoTempFiles 断言包里没有 .tmp 之类的半成品残留。
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	for _, name := range entryNames(t, dir) {
		if strings.Contains(name, ".tmp") {
			t.Errorf("包内残留临时文件 %q，写入没有清理干净", name)
		}
	}
	imagesDir := filepath.Join(dir, imagesDirName)
	if entries, err := os.ReadDir(imagesDir); err == nil {
		for _, e := range entries {
			if strings.Contains(e.Name(), ".tmp") {
				t.Errorf("images/ 内残留临时文件 %q", e.Name())
			}
		}
	}
}

// imageBytes 读出一个资产文件的原始字节。
func imageBytes(t *testing.T, dir, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, imagesDirName, name))
	if err != nil {
		t.Fatalf("读取资产 %s 失败: %v", name, err)
	}
	return data
}

// TestFirstRunWritesLoadablePackage 覆盖第 1 项：首次运行建出的包能通过真实加载器，
// 图层数/名字/混合模式/尺寸符合生成规则，version 为 11，引用的 PNG 都是 8-bit RGBA。
func TestFirstRunWritesLoadablePackage(t *testing.T) {
	cfg := testConfig(t)
	res := runSteps(t, cfg, nil)
	if len(res.Steps) != 1 {
		t.Fatalf("默认 steps 应为 1 步，得到 %d 步", len(res.Steps))
	}
	if !res.Steps[0].Created {
		t.Error("首次运行应报告新建包")
	}

	doc := openPackage(t, cfg.Dir)
	if doc.Format != domain.FormatID {
		t.Errorf("format = %q，期望 %q", doc.Format, domain.FormatID)
	}
	if doc.Version != 11 {
		t.Errorf("manifest 版本 = %d，期望 11", doc.Version)
	}
	if doc.ColorSpace != domain.ColorSpaceSRGB {
		t.Errorf("colorSpace = %q，期望 %q", doc.ColorSpace, domain.ColorSpaceSRGB)
	}
	if doc.DocumentID != res.DocumentID {
		t.Errorf("documentID = %q，与报告 %q 不一致", doc.DocumentID, res.DocumentID)
	}
	if doc.Width != cfg.Width || doc.Height != cfg.Height {
		t.Errorf("画布 = %d × %d，期望 %d × %d", doc.Width, doc.Height, cfg.Width, cfg.Height)
	}
	if doc.Resolution == nil || *doc.Resolution != defaultResolution {
		t.Errorf("resolution = %v，期望 %d", doc.Resolution, defaultResolution)
	}

	// 生成规则：第 1 步 = 背景 + 1 个光斑，自下而上。
	wantNames := []string{backgroundLayerName, orbSpecs[0].name}
	if len(doc.Layers) != len(wantNames) {
		t.Fatalf("图层数 = %d，期望 %d", len(doc.Layers), len(wantNames))
	}
	for i, want := range wantNames {
		if doc.Layers[i].Name != want {
			t.Errorf("图层 %d 名字 = %q，期望 %q（层序应自下而上）", i, doc.Layers[i].Name, want)
		}
	}
	// 背景铺满画布、Normal、不透明；光斑是计划里的位置与混合模式。
	bg := doc.Layers[0]
	if bg.BlendMode == nil || *bg.BlendMode != domain.BlendNormal {
		t.Errorf("背景混合模式 = %v，期望 Normal", bg.BlendMode)
	}
	if bg.Opacity == nil || *bg.Opacity != 1 {
		t.Errorf("背景不透明度 = %v，期望 1", bg.Opacity)
	}
	if bg.Size != [2]float64{float64(cfg.Width), float64(cfg.Height)} {
		t.Errorf("背景尺寸 = %v，期望铺满 %d × %d", bg.Size, cfg.Width, cfg.Height)
	}
	if bg.Origin != [2]float64{0, 0} {
		t.Errorf("背景原点 = %v，期望 [0 0]", bg.Origin)
	}

	spec := orbSpecs[0]
	orb := doc.Layers[1]
	if orb.BlendMode == nil || *orb.BlendMode != spec.blendMode {
		t.Errorf("光斑混合模式 = %v，期望 %q", orb.BlendMode, spec.blendMode)
	}
	if orb.Opacity == nil || *orb.Opacity != orbOpacity {
		t.Errorf("光斑不透明度 = %v，期望 %v", orb.Opacity, orbOpacity)
	}
	want := orbGeometry(cfg, spec)
	if orb.Size != [2]float64{float64(want.Dx()), float64(want.Dy())} {
		t.Errorf("光斑尺寸 = %v，期望 %d × %d", orb.Size, want.Dx(), want.Dy())
	}
	if orb.Origin != [2]float64{float64(want.Min.X), float64(want.Min.Y)} {
		t.Errorf("光斑原点 = %v，期望 [%d %d]", orb.Origin, want.Min.X, want.Min.Y)
	}
	// 缩放后的光斑必须完整落在画布内：这是场景编排的前提。
	if !want.In(image.Rect(0, 0, cfg.Width, cfg.Height)) {
		t.Errorf("光斑矩形 %v 超出画布 %d × %d", want, cfg.Width, cfg.Height)
	}
	for i := range doc.Layers {
		l := &doc.Layers[i]
		if l.Sampling != domain.SamplingHighQuality {
			t.Errorf("图层 %s 的 sampling = %q，期望 %q", l.Name, l.Sampling, domain.SamplingHighQuality)
		}
		if !l.IsVisible {
			t.Errorf("图层 %s 应可见", l.Name)
		}
		if l.ImageFile == nil || *l.ImageFile != strings.ToUpper(l.ID)+".png" {
			t.Errorf("图层 %s 的 imageFile = %v，必须等于 <大写 id>.png", l.Name, l.ImageFile)
		}
		if l.Transform.Size[0] < 1 || l.Transform.Size[1] < 1 {
			t.Errorf("图层 %s 的尺寸无效: %v", l.Name, l.Transform.Size)
		}
	}

	// 引用的 PNG 都在，且都是 8-bit PNG，尺寸与 transform.size 一致。
	for i := range doc.Layers {
		l := &doc.Layers[i]
		path := filepath.Join(cfg.Dir, imagesDirName, *l.ImageFile)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("图层 %s 引用的 PNG 不存在: %v", l.Name, err)
		}
		data := imageBytes(t, cfg.Dir, *l.ImageFile)
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("图层 %s 的 PNG 解码失败: %v", l.Name, err)
		}
		bounds := img.Bounds()
		if float64(bounds.Dx()) != l.Transform.Size[0] || float64(bounds.Dy()) != l.Transform.Size[1] {
			t.Errorf("图层 %s 的 PNG 尺寸 %d × %d 与 transform.size %v 不一致",
				l.Name, bounds.Dx(), bounds.Dy(), l.Transform.Size)
		}
		if bounds.Dx() != int(l.Transform.Size[0]) || bounds.Dy() != int(l.Transform.Size[1]) {
			t.Errorf("图层 %s 的尺寸不是整数像素: %v", l.Name, l.Transform.Size)
		}
		// 颜色模型：8-bit RGBA（非预乘或预乘都接受，位深必须是 8）。
		switch img.ColorModel() {
		case color.RGBAModel, color.NRGBAModel:
		default:
			t.Errorf("图层 %s 的 PNG 颜色模型 = %T，期望 8-bit RGBA", l.Name, img.ColorModel())
		}
	}

	assertNoTempFiles(t, cfg.Dir)
}

// TestRerunKeepsIdentityAndAppends 覆盖第 2 项：再次运行保留 documentID 与已有图层 id，
// 新图层追加到顶部（层序自下而上）。
func TestRerunKeepsIdentityAndAppends(t *testing.T) {
	cfg := testConfig(t)
	// 第 1 步建包（背景 + 光斑 1），随后的运行才是在已有包上"追加图层"。
	first := runSteps(t, cfg, nil)
	before := openPackage(t, cfg.Dir)

	oldIDs := make(map[string]string, len(before.Layers))
	for i := range before.Layers {
		oldIDs[before.Layers[i].Name] = before.Layers[i].ID
	}
	// 记下旧 PNG 的字节：同 seed 重跑像素本来就一致（见 TestReproducible），
	// 这里确认背景图层被重新渲染并写回，而不是被跳过。
	oldBG := imageBytes(t, cfg.Dir, *before.Layers[0].ImageFile)

	// 再跑一步：沿用同一个包，应该只在顶部追加。
	rerun := cfg
	rerun.Steps = 1
	second := runSteps(t, rerun, nil)
	after := openPackage(t, cfg.Dir)

	if after.DocumentID != before.DocumentID {
		t.Errorf("documentID 变了: %q → %q（契约要求保持身份）", before.DocumentID, after.DocumentID)
	}
	if after.DocumentID != first.DocumentID || after.DocumentID != second.DocumentID {
		t.Errorf("documentID 与两次报告不一致: %q / %q / %q", after.DocumentID, first.DocumentID, second.DocumentID)
	}
	if len(after.Layers) != len(before.Layers)+1 {
		t.Fatalf("再次运行应追加 1 个图层: %d → %d", len(before.Layers), len(after.Layers))
	}
	if len(after.Layers) != 3 {
		t.Fatalf("追加后应是背景 + 2 个光斑，得到 %d 个图层", len(after.Layers))
	}
	// 已有图层 id 不变、位置不变。
	for i := range before.Layers {
		if after.Layers[i].ID != before.Layers[i].ID {
			t.Errorf("图层 %d 的 id 变了: %q → %q", i, before.Layers[i].ID, after.Layers[i].ID)
		}
		if after.Layers[i].Name != before.Layers[i].Name {
			t.Errorf("图层 %d 的名字变了: %q → %q", i, before.Layers[i].Name, after.Layers[i].Name)
		}
		if id, ok := oldIDs[before.Layers[i].Name]; !ok || after.Layers[i].ID != id {
			t.Errorf("图层 %q 没有保住原 id", before.Layers[i].Name)
		}
	}
	// 新图层在顶部，且是计划里新出现的那个光斑。
	top := after.Layers[len(after.Layers)-1]
	if want := orbSpecs[1].name; top.Name != want {
		t.Errorf("新图层名字 = %q，期望 %q", top.Name, want)
	}
	if top.ID == before.Layers[0].ID || top.ID == before.Layers[1].ID {
		t.Errorf("新图层复用了已有 id: %s", top.ID)
	}
	if after.Layers[0].Name != backgroundLayerName {
		t.Errorf("最底层应是背景，得到 %q", after.Layers[0].Name)
	}

	// 背景被重新渲染写回：同 seed 下字节一致（可复现），但文件确实被覆盖过。
	rebuilt := imageBytes(t, cfg.Dir, *after.Layers[0].ImageFile)
	if !bytes.Equal(oldBG, rebuilt) {
		t.Error("同 seed 重跑，背景像素应当逐字节一致")
	}
	if second.Steps[0].Created {
		t.Error("已有包时不应报告新建")
	}
	if !first.Steps[0].Created {
		t.Error("首次运行的第一步应报告新建包")
	}
	if len(second.Steps[0].Updated) == 0 {
		t.Error("再次运行应报告覆盖了已有图层")
	}
	if len(second.Steps[0].Added) != 1 {
		t.Errorf("再次运行应报告新增 1 个图层，得到 %v", second.Steps[0].Added)
	}

	// 层序自下而上：背景在底，其后是计划里的光斑。
	for i, want := range []string{backgroundLayerName, orbSpecs[0].name, orbSpecs[1].name} {
		if after.Layers[i].Name != want {
			t.Errorf("图层 %d = %q，期望 %q", i, after.Layers[i].Name, want)
		}
	}
	assertNoTempFiles(t, cfg.Dir)
}

// TestStepsAccumulate 覆盖 -steps N：连续 N 步，每步多一个光斑，最多 maxOrbs 个。
func TestStepsAccumulate(t *testing.T) {
	cfg := testConfig(t)
	cfg.Steps = 3
	res := runSteps(t, cfg, nil)
	if len(res.Steps) != 3 {
		t.Fatalf("steps=3 应产生 3 份报告，得到 %d", len(res.Steps))
	}
	doc := openPackage(t, cfg.Dir)
	want := 1 + maxOrbs // 背景 + 3 个光斑
	if len(doc.Layers) != want {
		t.Fatalf("3 步后图层数 = %d，期望 %d", len(doc.Layers), want)
	}
	// 第 1 步建出背景 + 第 1 个光斑，之后每步恰好多 1 个。
	for i, r := range res.Steps {
		if r.Index != i+1 {
			t.Errorf("报告 %d 的步号 = %d", i, r.Index)
		}
		if r.LayerCount != 2+i {
			t.Errorf("第 %d 步的图层数 = %d，期望 %d", r.Index, r.LayerCount, 2+i)
		}
		wantAdded := 1
		if r.Index == 1 {
			wantAdded = 2
		}
		if len(r.Added) != wantAdded {
			t.Errorf("第 %d 步应新增 %d 个图层，得到 %v", r.Index, wantAdded, r.Added)
		}
	}
	// 超过 maxOrbs 后仍可运行，只是不再新增。
	cfg.Steps = maxOrbs + 1
	res = runSteps(t, cfg, nil)
	doc = openPackage(t, cfg.Dir)
	if len(doc.Layers) != 1+maxOrbs {
		t.Errorf("步数超过光斑上限后图层数 = %d，期望 %d", len(doc.Layers), 1+maxOrbs)
	}
	last := res.Steps[len(res.Steps)-1]
	if len(last.Added) != 0 {
		t.Errorf("最后一步不应新增图层，得到 %v", last.Added)
	}
	if last.LayerCount != 1+maxOrbs {
		t.Errorf("最后一步图层数 = %d，期望 %d", last.LayerCount, 1+maxOrbs)
	}
	assertNoTempFiles(t, cfg.Dir)
}

// TestWriteOrderKeepsOldManifestReadable 覆盖第 3 项（安全顺序）：在 manifest 改名之前
// 观察，旧清单仍然能被真实加载器完整读出；整个写入过程结束后没有临时文件残留。
func TestWriteOrderKeepsOldManifestReadable(t *testing.T) {
	cfg := testConfig(t)
	runSteps(t, cfg, nil)
	before := openPackage(t, cfg.Dir)

	// 只再走一步，专门观察这一次提交。
	one := cfg
	one.Steps = 1

	var hookCalls int
	var observed *domain.Document
	var observedErr error
	var sawTemp bool
	hook := func(dir string) error {
		hookCalls++
		if _, err := os.Stat(filepath.Join(dir, manifestTmpName)); err != nil {
			t.Errorf("改名之前应已写好 %s: %v", manifestTmpName, err)
		}
		sawTemp = true
		// 关键断言：改名之前，磁盘上的旧 manifest.json 依然完整可读。
		observed, observedErr = (&project.Store{}).Open(dir)
		return nil
	}

	runSteps(t, one, hook)
	if hookCalls != 1 {
		t.Fatalf("单步运行应触发 1 次提交前钩子，得到 %d", hookCalls)
	}
	if !sawTemp {
		t.Error("钩子没有观察到暂存文件")
	}
	if observedErr != nil {
		t.Fatalf("改名之前旧清单不可读: %v", observedErr)
	}
	if observed.DocumentID != before.DocumentID {
		t.Errorf("改名之前读到的是别的文档: %q vs %q", observed.DocumentID, before.DocumentID)
	}
	if len(observed.Layers) != len(before.Layers) {
		t.Errorf("改名之前应看到旧图层数 %d，得到 %d", len(before.Layers), len(observed.Layers))
	}
	// 旧清单仍然只引用旧 PNG：此刻 images/ 里已有新 PNG，但清单还没提到它。
	for i := range observed.Layers {
		if observed.Layers[i].ImageFile == nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(cfg.Dir, imagesDirName, *observed.Layers[i].ImageFile)); err != nil {
			t.Errorf("改名之前旧清单引用的 %s 应存在: %v", *observed.Layers[i].ImageFile, err)
		}
	}

	// 提交之后是新清单。
	after := openPackage(t, cfg.Dir)
	if len(after.Layers) != len(before.Layers)+1 {
		t.Errorf("提交后图层数 = %d，期望 %d", len(after.Layers), len(before.Layers)+1)
	}
	assertNoTempFiles(t, cfg.Dir)
}

// TestWriteOrderNeverMissingManifest 在多步写入的每一次提交前都观察一次，确认
// "要么旧要么新，不会缺"。
func TestWriteOrderNeverMissingManifest(t *testing.T) {
	cfg := testConfig(t)
	cfg.Steps = 3
	cfg.Pause = 0
	calls := 0
	hook := func(dir string) error {
		calls++
		// 第一次提交是"建包"：提交前还没有清单，这是正常的。
		// 之后每一次提交前，磁盘上都必须有一份完整可读的清单。
		if calls == 1 {
			return nil
		}
		if _, err := os.Stat(filepath.Join(dir, manifestName)); err != nil {
			t.Errorf("第 %d 次提交前 manifest.json 不见了: %v", calls, err)
		}
		if _, err := (&project.Store{}).Open(dir); err != nil {
			t.Errorf("第 %d 次提交前旧清单不可读: %v", calls, err)
		}
		return nil
	}
	runSteps(t, cfg, hook)
	if calls != cfg.Steps {
		t.Errorf("钩子调用次数 = %d，期望 %d", calls, cfg.Steps)
	}
	assertNoTempFiles(t, cfg.Dir)
}

// TestHookErrorAbortsCleanly 钩子报错时写入中止，但旧包仍然完好、没有临时文件残留。
func TestHookErrorAbortsCleanly(t *testing.T) {
	cfg := testConfig(t)
	runSteps(t, cfg, nil)
	before := openPackage(t, cfg.Dir)

	wantErr := os.ErrInvalid
	_, err := Run(cfg, func(string) error { return wantErr })
	if err == nil {
		t.Fatal("钩子报错时 Run 应返回错误")
	}
	// 钩子在这次运行的第一条改动上失败；步号接着包里的场景进度。
	if !strings.Contains(err.Error(), "步写入失败") {
		t.Errorf("错误信息应指出失败步号，得到: %v", err)
	}
	if !strings.Contains(err.Error(), "提交前观察失败") {
		t.Errorf("错误信息应说明是提交前观察失败，得到: %v", err)
	}
	after := openPackage(t, cfg.Dir)
	if len(after.Layers) != len(before.Layers) {
		t.Errorf("中止后图层数应不变: %d → %d", len(before.Layers), len(after.Layers))
	}
	assertNoTempFiles(t, cfg.Dir)
}

// TestConfigValidation 覆盖第 4 项：参数校验给出清晰错误。
func TestConfigValidation(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantSub string
	}{
		{"缺少 dir", func(c *Config) { c.Dir = "" }, "-dir"},
		{"dir 只有空白", func(c *Config) { c.Dir = "   " }, "-dir"},
		{"steps 为 0", func(c *Config) { c.Steps = 0 }, "-steps"},
		{"steps 为负", func(c *Config) { c.Steps = -3 }, "-steps"},
		{"pause 为负", func(c *Config) { c.Pause = -time.Second }, "-pause"},
		{"宽度为 0", func(c *Config) { c.Width = 0 }, "-width/-height"},
		{"高度为负", func(c *Config) { c.Height = -1 }, "-width/-height"},
		{"边长超限", func(c *Config) { c.Width = domain.MaxSide + 1 }, "超出"},
		{"像素超限", func(c *Config) { c.Width, c.Height = domain.MaxSide, domain.MaxSide }, "上限"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig(t)
			tc.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("期望参数校验失败，但通过了")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("错误 %q 应包含 %q", err.Error(), tc.wantSub)
			}
		})
	}

	// -dir 缺失是专门的哨兵错误，便于调用方区分。
	cfg := DefaultConfig()
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "-dir") {
		t.Errorf("-dir 缺失应报错，得到 %v", err)
	}
	cfg = DefaultConfig()
	cfg.Dir = filepath.Join(t.TempDir(), "ok.comp")
	if err := cfg.Validate(); err != nil {
		t.Errorf("默认配置加 dir 应当合法，得到 %v", err)
	}

	// -dir 缺失是专门的哨兵错误，便于调用方区分。
	missing := DefaultConfig()
	if _, err := Run(missing, nil); !errors.Is(err, errMissingDir) {
		t.Errorf("-dir 缺失的 Run 应返回 errMissingDir，得到 %v", err)
	}

	// Run 必须先校验再落盘：非法参数不应创建任何目录。
	neverDir := filepath.Join(t.TempDir(), "never.comp")
	bad := DefaultConfig()
	bad.Dir = neverDir
	bad.Steps = 0
	if _, err := Run(bad, nil); err == nil {
		t.Error("steps=0 的 Run 应失败")
	}
	if _, err := os.Stat(neverDir); !os.IsNotExist(err) {
		t.Errorf("参数非法时不应创建目录，但 %s 存在", neverDir)
	}
}

// TestParseConfigRejectsBadArgs 覆盖 CLI 层的参数校验。
func TestParseConfigRejectsBadArgs(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name    string
		args    []string
		wantSub string
	}{
		{"没有参数", nil, "-dir"},
		{"只给 steps", []string{"-steps", "2"}, "-dir"},
		{"steps 为 0", []string{"-dir", dir, "-steps", "0"}, "-steps"},
		{"未知参数", []string{"-dir", dir, "-nope"}, "flag provided"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseConfig(tc.args, os.Stderr)
			if err == nil {
				t.Fatal("期望解析失败，但通过了")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("错误 %q 应包含 %q", err.Error(), tc.wantSub)
			}
		})
	}

	// 正常参数解析成期望的配置。
	cfg, err := parseConfig([]string{"-dir", dir, "-steps", "3", "-pause", "400ms", "-width", "320", "-height", "180", "-seed", "7"}, os.Stderr)
	if err != nil {
		t.Fatalf("合法参数应解析成功: %v", err)
	}
	if cfg.Dir != dir || cfg.Steps != 3 || cfg.Pause != 400*time.Millisecond || cfg.Width != 320 || cfg.Height != 180 || cfg.Seed != 7 {
		t.Errorf("解析结果不符: %+v", cfg)
	}
}

// TestReproducible 覆盖第 5 项：同 seed 两次运行产生逐字节相同的 PNG。
func TestReproducible(t *testing.T) {
	run := func() (map[string][]byte, string) {
		cfg := testConfig(t)
		cfg.Steps = 2
		res := runSteps(t, cfg, nil)
		assets := map[string][]byte{}
		for _, name := range res.Steps[len(res.Steps)-1].Assets {
			assets[name] = imageBytes(t, cfg.Dir, name)
		}
		return assets, res.DocumentID
	}
	a, docA := run()
	b, docB := run()

	if len(a) == 0 {
		t.Fatal("没有收集到任何资产")
	}
	if len(a) != len(b) {
		t.Fatalf("两次运行的资产数不同: %d vs %d", len(a), len(b))
	}
	for name, data := range a {
		other, ok := b[name]
		if !ok {
			t.Errorf("第二次运行缺少资产 %s", name)
			continue
		}
		if !bytes.Equal(data, other) {
			t.Errorf("资产 %s 的字节不可复现（%d vs %d 字节）", name, len(data), len(other))
		}
	}
	if docA != docB {
		t.Errorf("同 seed 的 documentID 不同: %q vs %q", docA, docB)
	}
}

// TestDifferentSeedChangesPixels 确认 seed 真的参与生成：换 seed 像素不同。
func TestDifferentSeedChangesPixels(t *testing.T) {
	cfgA := testConfig(t)
	cfgA.Steps = 1
	runSteps(t, cfgA, nil)
	docA := openPackage(t, cfgA.Dir)
	bgA := imageBytes(t, cfgA.Dir, *docA.Layers[0].ImageFile)

	cfgB := testConfig(t)
	cfgB.Steps = 1
	cfgB.Seed = cfgA.Seed + 1
	runSteps(t, cfgB, nil)
	docB := openPackage(t, cfgB.Dir)
	bgB := imageBytes(t, cfgB.Dir, *docB.Layers[0].ImageFile)

	if bytes.Equal(bgA, bgB) {
		t.Error("不同 seed 应产生不同像素")
	}
	if docA.DocumentID == docB.DocumentID {
		t.Error("不同 seed 应派生不同 documentID")
	}
}

// TestCanvasMismatchRejected 已有包的画布与本次参数不一致时，给出清晰错误而不是写坏包。
func TestCanvasMismatchRejected(t *testing.T) {
	cfg := testConfig(t)
	runSteps(t, cfg, nil)
	before := openPackage(t, cfg.Dir)

	other := cfg
	other.Width = cfg.Width + 10
	_, err := Run(other, nil)
	if err == nil {
		t.Fatal("画布不一致应报错")
	}
	if !strings.Contains(err.Error(), "画布") {
		t.Errorf("错误应说明画布不一致，得到: %v", err)
	}
	after := openPackage(t, cfg.Dir)
	if len(after.Layers) != len(before.Layers) || after.Width != before.Width {
		t.Error("画布不一致时不应改动已有包")
	}
	assertNoTempFiles(t, cfg.Dir)
}

// TestPreservesForeignLayers 应用里自己加的图层不会被删除，只在顶部追加。
func TestPreservesForeignLayers(t *testing.T) {
	cfg := testConfig(t)
	runSteps(t, cfg, nil)

	// 直接往清单里插一个"应用自己加的"图层，再跑一步。
	doc := openPackage(t, cfg.Dir)
	foreignID := "AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE"
	foreignImage := foreignID + ".png"
	normal := domain.BlendNormal
	one := 1.0
	doc.Layers = append(doc.Layers, domain.Layer{
		ID:        foreignID,
		Name:      "用户手绘",
		IsVisible: true,
		Transform: domain.Transform{
			Origin:   [2]float64{1, 2},
			Size:     [2]float64{8, 6},
			Sampling: domain.SamplingHighQuality,
		},
		ImageFile: &foreignImage,
		Opacity:   &one,
		BlendMode: &normal,
	})
	if err := writePackage(cfg.Dir, doc, map[string][]byte{foreignImage: mustPNG(t, 8, 6)}, nil); err != nil {
		t.Fatalf("写入外来图层失败: %v", err)
	}

	// 再走一步，确保新光斑追加在"用户手绘"之下、其余图层之上。
	runSteps(t, cfg, nil)
	after := openPackage(t, cfg.Dir)
	foundAt := -1
	for i := range after.Layers {
		if after.Layers[i].ID != foreignID {
			continue
		}
		foundAt = i
		if after.Layers[i].Name != "用户手绘" {
			t.Errorf("外来图层名字被改了: %q", after.Layers[i].Name)
		}
		// 外来图层的图片必须还在（未被当成过期资产删掉）。
		if _, err := os.Stat(filepath.Join(cfg.Dir, imagesDirName, foreignImage)); err != nil {
			t.Errorf("外来图层的 PNG 被删了: %v", err)
		}
	}
	if foundAt < 0 {
		t.Fatal("外来图层被删掉了")
	}
	// 本示例的图层在底部（背景最底），用户自己的图层留在顶部。
	for i, want := range []string{backgroundLayerName, orbSpecs[0].name, orbSpecs[1].name, "用户手绘"} {
		if after.Layers[i].Name != want {
			t.Errorf("图层 %d = %q，期望 %q", i, after.Layers[i].Name, want)
		}
	}
	if foundAt != len(after.Layers)-1 {
		t.Errorf("外来图层应在最顶部，实际在第 %d 位", foundAt)
	}
	assertNoTempFiles(t, cfg.Dir)
}

// TestStaleAssetsRemoved 清单不再引用的旧 PNG 会被清掉（契约第 3 步）。
func TestStaleAssetsRemoved(t *testing.T) {
	cfg := testConfig(t)
	runSteps(t, cfg, nil)

	stale := "11111111-2222-4333-8444-555555555555.png"
	if err := os.WriteFile(filepath.Join(cfg.Dir, imagesDirName, stale), mustPNG(t, 4, 4), 0o644); err != nil {
		t.Fatalf("写入待清理资产失败: %v", err)
	}
	// 非资产命名的文件不属于我们清理的范围，必须留下。
	keep := filepath.Join(cfg.Dir, imagesDirName, "note.txt")
	if err := os.WriteFile(keep, []byte("keep me"), 0o644); err != nil {
		t.Fatalf("写入外来文件失败: %v", err)
	}

	runSteps(t, cfg, nil)
	if _, err := os.Stat(filepath.Join(cfg.Dir, imagesDirName, stale)); !os.IsNotExist(err) {
		t.Error("清单不再引用的资产应被删除")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("非资产命名的外来文件不应被删除: %v", err)
	}
	assertNoTempFiles(t, cfg.Dir)
}

// mustPNG 造一张最小的合法 8-bit RGBA PNG，供测试直接写包用。
func mustPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := renderBackground(w, h, 1)
	data, err := encodePNG(img)
	if err != nil {
		t.Fatalf("编码测试 PNG 失败: %v", err)
	}
	return data
}

// TestOpenNonPackageDir 指向一个不是包的目录时给出中文错误，且不 panic。
func TestOpenNonPackageDir(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Dir = dir
	cfg.Width, cfg.Height = 32, 32
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte("{ not json"), 0o644); err != nil {
		t.Fatalf("写入坏清单失败: %v", err)
	}
	_, err := Run(cfg, nil)
	if err == nil {
		t.Fatal("坏清单应报错")
	}
	if !strings.Contains(err.Error(), "读取已有包失败") {
		t.Errorf("错误应说明读取已有包失败，得到: %v", err)
	}
	assertNoTempFiles(t, dir)
}
