package bridge

// 票 86：导入 CMYK TIFF 时采纳它内嵌的配置。

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
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
