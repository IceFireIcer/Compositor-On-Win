package rasterio

// 票 86：CMYK TIFF 的读取与转换。
//
// 夹具是**手工拼**出来的：x/image/tiff 的 writer 不写 CMYK，所以这里按 TIFF 的布局
// 逐段拼一份最小的无压缩 CMYK TIFF（含可选的 ICC 标签 34675）。

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"compositor-win/internal/colorio"
	"compositor-win/internal/render"
)

// cmykTestProfile 找一份可用的 CMYK 配置：先看应用自己的目录，再看 Windows 系统目录。
// 找不到就跳过 —— 不发明一份合成配置（那正是票 72 拒绝做的事）。
func cmykTestProfile(t *testing.T) []byte {
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

// buildCMYKTIFF 拼一份最小的小端无压缩 CMYK TIFF。profile 为 nil 时不带 34675 标签。
func buildCMYKTIFF(t *testing.T, w, h int, ink []uint8, profile []byte) []byte {
	t.Helper()
	type entry struct {
		tag, typ uint16
		count    uint32
		value    uint32
	}
	entries := []entry{
		{256, 4, 1, uint32(w)},        // ImageWidth
		{257, 4, 1, uint32(h)},        // ImageLength
		{258, 3, 4, 0},                // BitsPerSample -> offset
		{259, 3, 1, 1},                // Compression = none
		{262, 3, 1, 5},                // Photometric = CMYK
		{273, 4, 1, 0},                // StripOffsets -> offset
		{277, 3, 1, 4},                // SamplesPerPixel
		{278, 4, 1, uint32(h)},        // RowsPerStrip
		{279, 4, 1, uint32(len(ink))}, // StripByteCounts
	}
	if profile != nil {
		entries = append(entries, entry{34675, 7, uint32(len(profile)), 0}) // ICCProfile
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].tag < entries[j].tag })

	ifdOffset := 8
	ifdLen := 2 + len(entries)*12 + 4
	bitsOffset := ifdOffset + ifdLen
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
	_ = binary.Write(&buf, le, uint32(0)) // no next IFD
	for i := 0; i < 4; i++ {
		_ = binary.Write(&buf, le, uint16(8))
	}
	buf.Write(profile)
	buf.Write(ink)
	return buf.Bytes()
}

// 白墨（C=M=Y=K=0）经配置转换后应当是白；半调油墨应当明显偏暗。这条同时验证了
// 「读得到油墨」与「按内嵌配置转换」两件事。
func TestCMYKTIFFImportsThroughItsEmbeddedProfile(t *testing.T) {
	profile := cmykTestProfile(t)
	ink := []uint8{
		0, 0, 0, 0, // 白
		0, 0, 0, 255, // 黑
	}
	data := buildCMYKTIFF(t, 2, 1, ink, profile)
	path := filepath.Join(t.TempDir(), "cmyk.tif")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Decode(path, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if result.Width != 2 || result.Height != 1 {
		t.Fatalf("尺寸 = %d×%d", result.Width, result.Height)
	}
	// 来源配置必须跟着结果出来，调用方（桥接）才能留存它供校样使用。
	if !bytes.Equal(result.SourceProfile, profile) {
		t.Fatalf("SourceProfile 应当是文件内嵌的那份配置（%d 字节 vs %d）", len(result.SourceProfile), len(profile))
	}
	white := result.Bitmap.Pix[0]
	if white < 240 {
		t.Errorf("白墨应当转成接近白，得到 %d", white)
	}
	// 满黑墨转出来的具体值取决于所用配置的黑点（相对比色把它映到那里），所以按
	// ADR-0008 §5 只做结构性断言：明显偏暗即可，不钉死数值。
	black := result.Bitmap.Pix[4]
	if black > 80 {
		t.Errorf("满黑墨应当转成明显偏暗，得到 %d", black)
	}
}

// 票 87 与票 86 的闭环：写出的 CMYK TIFF 能被我们自己的读取器读回来，配置也随文件回来，
// 再按同一份配置转回 RGB。按 ADR-0008 §5 取结构性断言 —— lcms2 的往返对饱和色有量化与
// 色域映射的位移，钉死数值没有意义。
func TestCMYKTIFFRoundTripsThroughTheReader(t *testing.T) {
	profile := cmykTestProfile(t)
	// 红、绿、蓝、白、黑，各一个像素。
	rgb := []uint8{255, 0, 0, 0, 255, 0, 0, 0, 255, 255, 255, 255, 0, 0, 0}
	const w, h = 5, 1
	ink, err := colorio.ConvertToInk(rgb, w, h, profile, [3]uint8{255, 255, 255})
	if err != nil {
		t.Fatalf("ConvertToInk: %v", err)
	}
	data, err := render.EncodeCMYKTIFFWithDPI(ink, w, h, 72, profile)
	if err != nil {
		t.Fatalf("EncodeCMYKTIFFWithDPI: %v", err)
	}
	back, err := decodeCMYKTIFF(data)
	if err != nil {
		t.Fatalf("写出的文件读不回来: %v", err)
	}
	if back.Width != w || back.Height != h {
		t.Fatalf("尺寸 = %d×%d，想要 %d×%d", back.Width, back.Height, w, h)
	}
	if !bytes.Equal(back.Profile, profile) {
		t.Fatal("内嵌的配置应当原样回来")
	}
	if !bytes.Equal(back.Ink, ink) {
		t.Fatal("油墨平面应当原样回来（TIFF 是无损的）")
	}
	got, err := colorio.ConvertToRGB(back.Ink, w, h, back.Profile)
	if err != nil {
		t.Fatalf("ConvertToRGB: %v", err)
	}
	// 结构性断言：每个像素的主通道仍然是原来那个。
	for x := 0; x < 3; x++ {
		channel := x // 0=红 1=绿 2=蓝
		value := got[x*3+channel]
		for c := 0; c < 3; c++ {
			if c != channel && value < got[x*3+c] {
				t.Errorf("像素 %d 的主通道不再是 %d：%v", x, channel, got[x*3:x*3+3])
			}
		}
	}
	// 纸白与黑是这套配置的两个端点，往返应当基本精确。
	if got[9] < 240 {
		t.Errorf("白应当回到接近白，得到 %v", got[9:12])
	}
	if got[12] > 60 {
		t.Errorf("黑应当回到接近黑，得到 %v", got[12:15])
	}
}

// 票 86 的 JPEG 那一半：带内嵌配置的 CMYK JPEG 走真实导入路径，颜色按配置转成 RGB，
// 配置跟着结果出来给桥接采纳。
func TestCMYKJPEGImportsThroughItsEmbeddedProfile(t *testing.T) {
	profile := cmykTestProfile(t)
	// 纸白与满黑各一个像素 —— 转换的绝对值取决于所用配置的黑点，所以只做结构性断言。
	ink := []uint8{0, 0, 0, 0, 0, 0, 0, 255}
	data, err := render.EncodeCMYKJPEGWithProfile(ink, 2, 1, 100, profile)
	if err != nil {
		t.Fatalf("EncodeCMYKJPEGWithProfile: %v", err)
	}
	path := filepath.Join(t.TempDir(), "cmyk.jpg")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Decode(path, 1000)
	if err != nil {
		t.Fatalf("CMYK JPEG 应当走导入路径: %v", err)
	}
	if result.Width != 2 || result.Height != 1 {
		t.Fatalf("尺寸 = %d×%d", result.Width, result.Height)
	}
	if !bytes.Equal(result.SourceProfile, profile) {
		t.Fatalf("SourceProfile 应当是文件内嵌的那份（%d 字节 vs %d）", len(result.SourceProfile), len(profile))
	}
	white := result.Bitmap.Pix[0]
	if white < 235 {
		t.Errorf("纸白应当转成接近白，得到 %d", white)
	}
	black := result.Bitmap.Pix[4]
	if black > 90 {
		t.Errorf("满黑墨应当转成明显偏暗，得到 %d", black)
	}
}

// 没有内嵌配置的 CMYK JPEG 必须**明确拒绝**，而不是猜一个 sRGB 出来。
func TestCMYKJPEGWithoutAProfileIsRefused(t *testing.T) {
	data, err := render.EncodeCMYKJPEG([]uint8{0, 0, 0, 0}, 1, 1, 90)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "noprofile.jpg")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Decode(path, 1000)
	if err == nil {
		t.Fatal("没有配置的 CMYK JPEG 应当被拒绝")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("ICC")) {
		t.Fatalf("错误应当说明缺的是 ICC 配置，得到 %v", err)
	}
}

// 没有内嵌配置的 CMYK TIFF 必须**明确拒绝**，而不是猜一个 sRGB 出来。
func TestCMYKTIFFWithoutAProfileIsRefused(t *testing.T) {
	data := buildCMYKTIFF(t, 1, 1, []uint8{0, 0, 0, 0}, nil)
	path := filepath.Join(t.TempDir(), "noprofile.tif")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Decode(path, 1000)
	if err == nil {
		t.Fatal("没有配置的 CMYK TIFF 应当被拒绝")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("ICC")) {
		t.Fatalf("错误应当说明缺的是 ICC 配置，得到 %v", err)
	}
}

// 不支持的压缩方式要明确报错，而不是解出垃圾。
func TestCMYKTIFFWithUnsupportedCompressionIsRefused(t *testing.T) {
	profile := cmykTestProfile(t)
	data := buildCMYKTIFF(t, 1, 1, []uint8{0, 0, 0, 0}, profile)
	// 把 Compression 标签（259）的值改成 LZW（5）：它是 IFD 的第 4 个条目。
	ifd := 8
	count := int(binary.LittleEndian.Uint16(data[ifd : ifd+2]))
	for i := 0; i < count; i++ {
		at := ifd + 2 + i*12
		if binary.LittleEndian.Uint16(data[at:]) == 259 {
			binary.LittleEndian.PutUint16(data[at+8:], 5)
		}
	}
	path := filepath.Join(t.TempDir(), "lzw.tif")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Decode(path, 1000)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("压缩")) {
		t.Fatalf("不支持的压缩应当明确报错，得到 %v", err)
	}
}
