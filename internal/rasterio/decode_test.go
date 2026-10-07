package rasterio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"compositor-win/internal/render"
	"golang.org/x/image/tiff"
)

// stripeBitmap builds a W×H bitmap whose pixels are distinct letters A, B, C…
// (row-major), so every orientation mapping is visible.
func stripeBitmap(w, h int) *render.Bitmap {
	b := render.NewBitmap(w, h)
	for i := 0; i < w*h; i++ {
		b.Pix[i*4] = uint8('A' + i)
		b.Pix[i*4+3] = 255
	}
	return b
}

func letters(b *render.Bitmap) string {
	out := make([]byte, b.W*b.H)
	for i := range out {
		out[i] = b.Pix[i*4]
	}
	return string(out)
}

func TestApplyOrientation(t *testing.T) {
	// Stored 2×3:  A B
	//              C D
	//              E F
	src := stripeBitmap(2, 3)
	threeWide := func(rows ...string) string { return rows[0] + rows[1] }
	cases := []struct {
		orientation int
		want        string
		w, h        int
	}{
		{1, "ABCDEF", 2, 3},
		{2, "BADCFE", 2, 3},
		{3, "FEDCBA", 2, 3},
		{4, "EFCDAB", 2, 3},
		{5, threeWide("ACE", "BDF"), 3, 2},
		{6, threeWide("ECA", "FDB"), 3, 2},
		{7, threeWide("FDB", "ECA"), 3, 2},
		{8, threeWide("BDF", "ACE"), 3, 2},
	}
	for _, c := range cases {
		got := ApplyOrientation(src, c.orientation)
		if got.W != c.w || got.H != c.h {
			t.Errorf("方向 %d：尺寸 %d×%d，想要 %d×%d", c.orientation, got.W, got.H, c.w, c.h)
		}
		if letters(got) != c.want {
			t.Errorf("方向 %d：得到 %q，想要 %q", c.orientation, letters(got), c.want)
		}
	}
	// Unknown values stay upright and return the same bitmap.
	if ApplyOrientation(src, 9) != src || ApplyOrientation(src, 0) != src {
		t.Error("未知方向应原样返回")
	}
}

// exifAPP1 wraps a TIFF payload with the orientation tag in a JPEG APP1
// segment ("Exif\0\0" prefix, big-endian length).
func exifAPP1(orientation uint16, little bool) []byte {
	bo := []byte("MM\x00\x2a")
	u16, u32 := binary.BigEndian.AppendUint16, binary.BigEndian.AppendUint32
	if little {
		bo = []byte("II\x2a\x00")
		u16, u32 = binary.LittleEndian.AppendUint16, binary.LittleEndian.AppendUint32
	}
	payload := make([]byte, 0, 8+2+12+4)
	payload = append(payload, bo...) // TIFF header
	payload = u32(payload, 8)        // IFD0 at offset 8
	payload = u16(payload, 1)        // one entry
	payload = u16(payload, 0x0112)   // orientation tag
	payload = u16(payload, 3)        // SHORT
	payload = u16(payload, 1)        // count 1
	payload = u16(payload, orientation)
	payload = u16(payload, 0) // pad
	payload = u32(payload, 0) // next IFD
	seg := append([]byte("Exif\x00\x00"), payload...)
	out := []byte{0xFF, 0xE1}
	out = binary.BigEndian.AppendUint16(out, uint16(len(seg)+2))
	return append(out, seg...)
}

func TestJPEGEXIFOrientationParsing(t *testing.T) {
	data := []byte{0xFF, 0xD8} // SOI
	data = append(data, exifAPP1(6, false)...)
	data = append(data, 0xFF, 0xD9) // EOI
	if got := jpegEXIFOrientation(data); got != 6 {
		t.Errorf("EXIF 方向 6：得到 %d", got)
	}
	if got := jpegEXIFOrientation([]byte{0xFF, 0xD8, 0xFF, 0xD9}); got != 1 {
		t.Error("无 EXIF 段应为 1")
	}
	bigEndian6 := append([]byte{0xFF, 0xD8}, exifAPP1(8, false)...)
	if got := jpegEXIFOrientation(bigEndian6); got != 8 {
		t.Errorf("EXIF 方向 8：得到 %d", got)
	}
	little := append([]byte{0xFF, 0xD8}, exifAPP1(3, true)...)
	if got := jpegEXIFOrientation(little); got != 3 {
		t.Errorf("小端 EXIF 方向 3：得到 %d", got)
	}
	truncated := append([]byte{0xFF, 0xD8}, exifAPP1(6, false)[:10]...)
	if got := jpegEXIFOrientation(truncated); got != 1 {
		t.Errorf("截断的 EXIF 应为 1，得到 %d", got)
	}
}

// handTIFF builds a bare TIFF header + IFD0 with one orientation entry.
func handTIFF(orientation uint16, little bool) []byte {
	bo := []byte("MM\x00\x2a")
	u16, u32 := binary.BigEndian.AppendUint16, binary.BigEndian.AppendUint32
	if little {
		bo = []byte("II\x2a\x00")
		u16, u32 = binary.LittleEndian.AppendUint16, binary.LittleEndian.AppendUint32
	}
	data := append([]byte{}, bo...)
	data = u32(data, 8)
	data = u16(data, 1)
	data = u16(data, 0x0112)
	data = u16(data, 3)
	data = u16(data, 1)
	data = u16(data, orientation)
	data = u16(data, 0)
	data = u32(data, 0)
	return data
}

func TestTIFFOrientationParsing(t *testing.T) {
	if got := tiffOrientation(handTIFF(8, false)); got != 8 {
		t.Errorf("TIFF 大端方向 8：得到 %d", got)
	}
	if got := tiffOrientation(handTIFF(5, true)); got != 5 {
		t.Errorf("TIFF 小端方向 5：得到 %d", got)
	}
	if got := tiffOrientation([]byte("XX\x00\x00\x00\x00\x00\x00")); got != 1 {
		t.Error("坏字节序应为 1")
	}
	bigTIFF := handTIFF(6, false)
	bigTIFF[2], bigTIFF[3] = 0x00, 0x2b // magic 43
	if got := tiffOrientation(bigTIFF); got != 1 {
		t.Error("BigTIFF 应保守返回 1")
	}
}

func TestPNGEXIFOrientationParsing(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	base := buf.Bytes()
	insert := func(payload []byte) []byte {
		out := append([]byte{}, base[:len(base)-12]...) // strip IEND
		body := append([]byte("eXIf"), payload...)
		chunk := binary.BigEndian.AppendUint32(nil, uint32(len(payload))) // length, then type/data/crc
		chunk = append(chunk, body...)
		chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(body))
		out = append(out, chunk...)
		return append(out, base[len(base)-12:]...) // IEND back
	}
	// eXIf carries the full TIFF layout, header included.
	withEXIF := insert(handTIFF(7, true))
	if got := pngEXIFOrientation(withEXIF); got != 7 {
		t.Errorf("PNG eXIf 方向 7：得到 %d", got)
	}
	if got := pngEXIFOrientation(base); got != 1 {
		t.Error("无 eXIf 应为 1")
	}
}

func TestDecodePNGPremultipliesAndNames(t *testing.T) {
	dir := t.TempDir()
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 128, B: 0, A: 128})
	src.SetNRGBA(1, 0, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	path := filepath.Join(dir, "示例 图.png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Decode(path, domainBudget())
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != "示例 图" {
		t.Errorf("图层名 %q", res.Name)
	}
	if res.Width != 2 || res.Height != 1 {
		t.Errorf("尺寸 %d×%d", res.Width, res.Height)
	}
	want := []uint8{128, 64, 0, 128, 10, 20, 30, 255}
	for i := range want {
		if res.Bitmap.Pix[i] != want[i] {
			t.Fatalf("像素 %d：得到 %d，想要 %d（预乘）", i, res.Bitmap.Pix[i], want[i])
		}
	}
}

func TestDecodePNGEndToEndWithOrientation(t *testing.T) {
	dir := t.TempDir()
	src := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
	src.SetNRGBA(1, 0, color.NRGBA{R: 0, G: 0, B: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, src); err != nil {
		t.Fatal(err)
	}
	// Insert an eXIf chunk carrying orientation 6 (rotate 90 CW).
	out := append([]byte{}, buf.Bytes()[:len(buf.Bytes())-12]...)
	body := append([]byte("eXIf"), handTIFF(6, true)...)
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(body)-4))
	chunk = append(chunk, body...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(body))
	out = append(out, chunk...)
	out = append(out, buf.Bytes()[len(buf.Bytes())-12:]...)
	path := filepath.Join(dir, "rotated.png")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Decode(path, domainBudget())
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 1 || res.Height != 2 {
		t.Fatalf("方向 6 应交换边：得到 %d×%d", res.Width, res.Height)
	}
	if r, b := res.Bitmap.Pix[0], res.Bitmap.Pix[2]; r < 250 || b > 5 {
		t.Errorf("旋转 90° 后应红上蓝下：得到 (%d,%d,%d)", r, res.Bitmap.Pix[1], b)
	}
	if r, b := res.Bitmap.Pix[4], res.Bitmap.Pix[6]; b < 250 || r > 5 {
		t.Errorf("下行应为蓝：得到 (%d,%d,%d)", r, res.Bitmap.Pix[5], b)
	}
}

func TestDecodeJPEGPlain(t *testing.T) {
	dir := t.TempDir()
	src := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			if x < 4 {
				src.Set(x, y, color.NRGBA{R: 255, G: 0, B: 0, A: 255})
			} else {
				src.Set(x, y, color.NRGBA{R: 0, G: 0, B: 255, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "plain.jpg")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Decode(path, domainBudget())
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 8 || res.Height != 4 || res.Name != "plain" {
		t.Fatalf("得到 %d×%d %q", res.Width, res.Height, res.Name)
	}
	if r, b := res.Bitmap.Pix[0], res.Bitmap.Pix[2]; r < 200 || b > 60 {
		t.Errorf("左上应为红：得到 (%d,%d,%d)", r, res.Bitmap.Pix[1], b)
	}
}

func TestDecodeTIFF(t *testing.T) {
	dir := t.TempDir()
	src := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	src.SetNRGBA(2, 1, color.NRGBA{R: 200, G: 100, B: 50, A: 100})
	var buf bytes.Buffer
	if err := tiff.Encode(&buf, src, nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "scan.tif")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Decode(path, domainBudget())
	if err != nil {
		t.Fatal(err)
	}
	if res.Width != 3 || res.Height != 2 {
		t.Fatalf("尺寸 %d×%d", res.Width, res.Height)
	}
	i := (1*3 + 2) * 4
	a := uint32(res.Bitmap.Pix[i+3])
	if res.Bitmap.Pix[i] != uint8(200*a/255) || res.Bitmap.Pix[i+3] != 100 {
		t.Errorf("TIFF 半透明像素应预乘：得到 %v", res.Bitmap.Pix[i:i+4])
	}
}

func TestDecodeRefusesLimitsAndUnknownTypes(t *testing.T) {
	dir := t.TempDir()
	// IHDR-only PNG declaring a 30001-wide image: too large by side, before
	// any pixels are decoded.
	ihdr := func(w, h uint32) []byte {
		body := binary.BigEndian.AppendUint32([]byte{}, w)
		body = binary.BigEndian.AppendUint32(body, h)
		body = append(body, 8, 6, 0, 0, 0)
		out := binary.BigEndian.AppendUint32([]byte{}, 13)
		out = append(out, "IHDR"...)
		out = append(out, body...)
		out = binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(out[4:]))
		return out
	}
	huge := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}, ihdr(30001, 1)...)
	huge = append(huge, 0, 0, 0, 0)
	huge = append(huge, "IEND"...)
	hugePath := filepath.Join(dir, "huge.png")
	if err := os.WriteFile(hugePath, huge, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(hugePath, domainBudget()); err == nil || !isTooLarge(err) {
		t.Errorf("30001px 宽应报 TooLarge，得到 %v", err)
	}
	// A small image over the remaining-pixel budget.
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	tinyPath := filepath.Join(dir, "tiny.png")
	if err := os.WriteFile(tinyPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(tinyPath, 3); err == nil || !isTooLarge(err) {
		t.Errorf("超出像素预算应报 TooLarge，得到 %v", err)
	}
	gifPath := filepath.Join(dir, "anim.gif")
	if err := os.WriteFile(gifPath, []byte("GIF89a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(gifPath, domainBudget()); err != ErrUnsupported {
		t.Errorf("不受支持类型应 ErrUnsupported，得到 %v", err)
	}
	junkPath := filepath.Join(dir, "junk.png")
	if err := os.WriteFile(junkPath, []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(junkPath, domainBudget()); err == nil || !isUnreadable(err) {
		t.Errorf("坏文件应 ErrUnreadable，得到 %v", err)
	}
}

func domainBudget() int { return 200_000_000 }

func isTooLarge(err error) bool {
	_, ok := err.(*TooLargeError)
	return ok
}

func isUnreadable(err error) bool { return errors.Is(err, ErrUnreadable) }
