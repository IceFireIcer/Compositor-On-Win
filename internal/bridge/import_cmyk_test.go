package bridge

// 票 86：导入 CMYK TIFF 时采纳它内嵌的配置。

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"compositor-win/internal/colorio"
)

// 票 86：导入一个 CMYK TIFF 时，它带进来的配置要被**留存并选为当前配置** ——
// 这样校样才回到那份文件当初针对的油墨。这是桥接层的那几行胶水，单独钉一条。
func TestImportingACMykTIFFAdoptsItsEmbeddedProfile(t *testing.T) {
	t.Setenv("COMPOSITOR_COLORIO_DATA_DIR", t.TempDir())
	colorio.ClearProofCache()
	if err := colorio.SetChosen(""); err != nil {
		t.Fatal(err)
	}
	// 取一份**不是回退**的配置：Chosen() 在没选过时会回退到 Generic 或第一份，
	// 所以只有挑一份别的，才能分辨「采纳了内嵌的」与「回退到默认」。
	all := colorio.Installed()
	var target colorio.Profile
	for i := len(all) - 1; i >= 0; i-- {
		if colorio.IsCMYKProfile(all[i].Data) {
			target = all[i]
			break
		}
	}
	if target.Path == "" {
		t.Skip("本机没有可用的 CMYK 配置")
	}
	if before := colorio.Chosen(); before != nil && before.Name == target.Name {
		t.Skip("回退恰好就是目标配置，这条测试分辨不出采纳")
	}
	data, err := os.ReadFile(target.Path)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "cmyk.tif")
	if err := os.WriteFile(path, bridgeCMYKTIFF(t, data), 0o644); err != nil {
		t.Fatal(err)
	}
	svc, _ := newTestService(t, 4, 4)
	begin, err := svc.beginImport([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(begin.Items) != 1 || begin.Items[0].Status != "ok" {
		t.Fatalf("导入应当成功: %+v", begin.Items)
	}
	chosen := colorio.Chosen()
	if chosen == nil {
		t.Fatal("导入后应当有一份被选中的 CMYK 配置")
	}
	// 只打印名字：Profile 里带着整份配置字节，打进失败信息会淹没输出。
	if chosen.Name != target.Name {
		t.Fatalf("选中的是 %q，想要被采纳的那份 %q", chosen.Name, target.Name)
	}
}

// 票 86：CMYK PSD 走的是 project_io 的 loadPhotoshop，**不经过** rasterio.Decode —— 所以
// 「内嵌配置留进会话」这条原来对它不生效。这里钉住补上的那一步。
func TestLoadingACMykPSDAdoptsItsProfile(t *testing.T) {
	t.Setenv("COMPOSITOR_COLORIO_DATA_DIR", t.TempDir())
	colorio.ClearProofCache()
	if err := colorio.SetChosen(""); err != nil {
		t.Fatal(err)
	}
	// 取一份**不是回退**的配置：Chosen() 没选过时会回退到 Generic 或第一份，只有挑一份别的
	// 才能分辨「采纳了内嵌的」与「回退到默认」。
	all := colorio.Installed()
	var target colorio.Profile
	for i := len(all) - 1; i >= 0; i-- {
		if colorio.IsCMYKProfile(all[i].Data) {
			target = all[i]
			break
		}
	}
	if target.Path == "" {
		t.Skip("本机没有可用的 CMYK 配置")
	}
	if before := colorio.Chosen(); before != nil && before.Name == target.Name {
		t.Skip("回退恰好就是目标配置，这条测试分辨不出采纳")
	}
	data, err := os.ReadFile(target.Path)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "cmyk.psd")
	if err := os.WriteFile(path, bridgeCMYKPSD(data), 0o644); err != nil {
		t.Fatal(err)
	}
	svc, _ := newTestService(t, 4, 4)
	if _, _, _, err := svc.loadPhotoshop(path); err != nil {
		t.Fatalf("加载 CMYK PSD: %v", err)
	}
	chosen := colorio.Chosen()
	if chosen == nil {
		t.Fatal("加载后应当有一份被选中的 CMYK 配置")
	}
	if chosen.Name != target.Name {
		t.Fatalf("选中的是 %q，想要被采纳的那份 %q", chosen.Name, target.Name)
	}
}

// 票 86 第 3 条：**打开 CMYK → 保存 .comp → 重开**，像素与配置都要还在。
//
// 这条原先一直没勾，是因为导入/导出两侧、「配置留进会话」都各有测试，但**没有一条**串起
// 「落盘再读回来」。它验证三件事：
//  1. 重开后像素与保存前逐字节一致（.comp 里存的是转换后的 sRGB，转换不会因为往返而漂移）；
//  2. 图层结构还在；
//  3. 校样用的那份配置仍是导入时采纳的那份（`cmyk.json` 持久化在 colorio 数据目录里）。
func TestCMYKImportSurvivesSaveAndReopen(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("COMPOSITOR_COLORIO_DATA_DIR", dataDir)
	colorio.ClearProofCache()
	if err := colorio.SetChosen(""); err != nil {
		t.Fatal(err)
	}

	// 挑一份**不是回退**的 CMYK 配置：`Chosen()` 在没选过时会回退到 Generic 或第一份，
	// 若挑的正好是它，「持久化了」与「回退了」就分辨不出来 —— 第一版就是这么写错的，
	// 把 `adopt` 里的 SetChosen 去掉，测试照样通过。
	var target colorio.Profile
	for i := len(colorio.Installed()) - 1; i >= 0; i-- {
		p := colorio.Installed()[i]
		if data, err := os.ReadFile(p.Path); err == nil && colorio.IsCMYKProfile(data) {
			target = p
			break
		}
	}
	if target.Path == "" {
		t.Skip("本机没有可用的 CMYK 配置")
	}
	if before := colorio.Chosen(); before != nil && before.Name == target.Name {
		t.Skip("回退恰好就是目标配置，这条测试分辨不出采纳")
	}
	profile, err := os.ReadFile(target.Path)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "cmyk.tif")
	if err := os.WriteFile(srcPath, bridgeCMYKTIFF(t, profile), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. 打开。
	svc, _ := newTestService(t, 4, 4)
	if _, err := svc.beginImport([]string{srcPath}); err != nil {
		t.Fatalf("导入 CMYK TIFF: %v", err)
	}
	adopted := colorio.Chosen()
	if adopted == nil {
		t.Fatal("导入后应当有一份被选中的 CMYK 配置")
	}
	// 持久化本身要**直接查文件**，不能靠 Chosen() 去猜：adopt 会把配置复制进 colorio 目录，
	// 副本名字与采纳的一致，于是「没写 cmyk.json、回退到副本」与「写了 cmyk.json」在
	// Chosen().Name 上看起来一模一样 —— 第一版就是这么写的，把 adopt 里的 SetChosen 全去掉，
	// 测试照样通过。
	chosenJSON, err := os.ReadFile(filepath.Join(dataDir, "cmyk.json"))
	if err != nil {
		t.Fatalf("采纳后应当写下 cmyk.json: %v", err)
	}
	if !strings.Contains(string(chosenJSON), adopted.Name) {
		t.Fatalf("cmyk.json 里应当记着 %q，得到 %s", adopted.Name, chosenJSON)
	}
	sess := activeSessionOf(t, svc)
	var asset string
	for i := range sess.doc.Layers {
		if sess.doc.Layers[i].ImageFile != nil {
			asset = *sess.doc.Layers[i].ImageFile
			break
		}
	}
	if asset == "" {
		t.Fatal("导入后应当有一个带位图的图层")
	}
	before := append([]uint8(nil), sess.bitmaps[asset].Pix...)
	layerCount := len(sess.doc.Layers)

	// 2. 保存。
	savePath := filepath.Join(dir, "roundtrip.comp")
	svc.saveDialog = func(context.Context) (string, error) { return savePath, nil }
	if _, err := svc.SaveProjectDialog(); err != nil {
		t.Fatalf("保存: %v", err)
	}

	// 3. 重开：全新 workspace/service，**同一个 colorio 数据目录**。
	ws2 := NewWorkspace()
	svc2 := NewService(ws2)
	svc2.openDialog = func(context.Context) (string, error) { return savePath, nil }
	if _, err := svc2.OpenProjectDialog(); err != nil {
		t.Fatalf("重开: %v", err)
	}
	sess2 := activeSessionOf(t, svc2)
	if len(sess2.doc.Layers) != layerCount {
		t.Fatalf("重开后图层数 = %d，想要 %d", len(sess2.doc.Layers), layerCount)
	}
	reopened := sess2.bitmaps[asset]
	if reopened == nil {
		t.Fatalf("重开后位图库缺少 %s", asset)
	}
	if len(reopened.Pix) != len(before) {
		t.Fatalf("重开后位图长度 = %d，想要 %d", len(reopened.Pix), len(before))
	}
	for i := range before {
		if reopened.Pix[i] != before[i] {
			t.Fatalf("像素 %d：保存前 %d，重开后 %d —— 往返把颜色改了", i, before[i], reopened.Pix[i])
		}
	}

	// 4. 校样用的仍是导入时采纳的那份配置。
	again := colorio.Chosen()
	if again == nil {
		t.Fatal("重开后应当仍有一份被选中的 CMYK 配置")
	}
	if again.Name != adopted.Name {
		t.Fatalf("重开后的配置是 %q，想要 %q", again.Name, adopted.Name)
	}
}

// bridgeCMYKPSD 拼一份 2×1 的拼合 CMYK PSD（内嵌给定配置）：左纸白、右满墨，四通道反相。
func bridgeCMYKPSD(profile []byte) []byte {
	var out []byte
	be := func(v uint32, n int) {
		for i := n - 1; i >= 0; i-- {
			out = append(out, uint8(v>>(8*uint(i))))
		}
	}
	str := func(s string) { out = append(out, s...) }
	str("8BPS")
	be(1, 2)
	out = append(out, make([]byte, 6)...)
	be(4, 2) // 通道数
	be(1, 4) // 高
	be(2, 4) // 宽
	be(8, 2)
	be(4, 2) // 模式 4 = CMYK
	be(0, 4) // 色彩模式数据

	var res []byte
	res = append(res, []byte("8BIM")...)
	res = append(res, 0x04, 0x0F) // 资源 id 1039
	res = append(res, 0, 0)       // 名字长度 0 + 偶数字节填充
	res = append(res, byte(len(profile)>>24), byte(len(profile)>>16), byte(len(profile)>>8), byte(len(profile)))
	res = append(res, profile...)
	if len(profile)%2 == 1 {
		res = append(res, 0)
	}
	be(uint32(len(res)), 4)
	out = append(out, res...)
	be(0, 4) // 图层段 0 → 拼合
	be(0, 2) // 图像数据：无压缩
	for c := 0; c < 4; c++ {
		out = append(out, 255, 0) // 左：不上墨；右：满墨
	}
	return out
}

// bridgeCMYKProfile 找一份可用的系统 CMYK 配置，找不到就跳过。
func bridgeCMYKProfile(t *testing.T) []byte {
	t.Helper()
	for _, p := range colorio.Installed() {
		if data, err := os.ReadFile(p.Path); err == nil && len(data) > 0 {
			return data
		}
	}
	for _, p := range colorio.ScanFolder(`C:\Windows\System32\spool\drivers\color`) {
		if data, err := os.ReadFile(p.Path); err == nil && len(data) > 0 {
			return data
		}
	}
	t.Skip("本机没有可用的 CMYK ICC 配置")
	return nil
}

// bridgeCMYKTIFF 拼一份最小的无压缩 CMYK TIFF（带内嵌配置）—— x/image/tiff 的 writer
// 不写 CMYK，所以只能手工拼。
func bridgeCMYKTIFF(t *testing.T, profile []byte) []byte {
	t.Helper()
	w, h := 1, 1
	ink := []uint8{0, 0, 0, 0}
	type entry struct {
		tag, typ uint16
		count    uint32
		value    uint32
	}
	entries := []entry{
		{256, 4, 1, uint32(w)}, {257, 4, 1, uint32(h)}, {258, 3, 4, 0},
		{259, 3, 1, 1}, {262, 3, 1, 5}, {273, 4, 1, 0},
		{277, 3, 1, 4}, {278, 4, 1, uint32(h)}, {279, 4, 1, uint32(len(ink))},
		{34675, 7, uint32(len(profile)), 0},
	}
	ifdOffset := 8
	bitsOffset := ifdOffset + 2 + len(entries)*12 + 4
	profileOffset := bitsOffset + 8
	inkOffset := profileOffset + len(profile)
	var buf bytes.Buffer
	le := binary.LittleEndian
	buf.WriteString("II")
	_ = binary.Write(&buf, le, uint16(42))
	_ = binary.Write(&buf, le, uint32(ifdOffset))
	_ = binary.Write(&buf, le, uint16(len(entries)))
	for _, e := range entries {
		value := e.value
		switch e.tag {
		case 258:
			value = uint32(bitsOffset)
		case 273:
			value = uint32(inkOffset)
		case 34675:
			value = uint32(profileOffset)
		}
		_ = binary.Write(&buf, le, e.tag)
		_ = binary.Write(&buf, le, e.typ)
		_ = binary.Write(&buf, le, e.count)
		_ = binary.Write(&buf, le, value)
	}
	_ = binary.Write(&buf, le, uint32(0))
	for i := 0; i < 4; i++ {
		_ = binary.Write(&buf, le, uint16(8))
	}
	buf.Write(profile)
	buf.Write(ink)
	return buf.Bytes()
}
