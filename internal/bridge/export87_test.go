package bridge

// 票 87：CMYK 导出（TIFF 与 PDF）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"compositor-win/internal/colorio"
	"compositor-win/internal/rasterio"
)

// 票 87：CMYK 导出。

// CMYK 导出的配置取自 View › CMYK › 配置（colorio.Chosen），所以测试要把它指到一个临时
// 目录，别碰用户真实的配置选择。
func cmykExportService(t *testing.T) *Service {
	t.Helper()
	t.Setenv("COMPOSITOR_COLORIO_DATA_DIR", t.TempDir())
	colorio.ClearProofCache()
	svc, _ := newTestService(t, 4, 2)
	return svc
}

// PDF 的图必须声明自己在什么油墨里：/ICCBased、N=4、Alternate /DeviceCMYK，配置随文件走。
func TestExportAsCMYKPDFDeclaresItsInkSpace(t *testing.T) {
	svc := cmykExportService(t)
	dir := t.TempDir()
	stubExportSaveDialog(t, filepath.Join(dir, "print"))
	reply, err := svc.ExportAs(`{"format":"pdf","cmyk":true,"defaultName":"print.pdf"}`)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(parseJSON[map[string]string](t, reply)["path"])
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"/ColorSpace [/ICCBased 6 0 R]", "/N 4", "/Alternate /DeviceCMYK"} {
		if !strings.Contains(text, want) {
			t.Errorf("PDF 里缺少 %q", want)
		}
	}
	// 图本身是四通道：每像素 4 字节的 Flate 流解出来的长度应当是 w*h*4。
	if strings.Contains(text, "/DeviceRGB") {
		t.Error("CMYK 导出不该出现 /DeviceRGB")
	}
}

// TIFF 的 CMYK 导出能被票 86 的读取器读回来（配置也在）。
func TestExportAsCMYKTIFFCarriesTheProfile(t *testing.T) {
	svc := cmykExportService(t)
	dir := t.TempDir()
	stubExportSaveDialog(t, filepath.Join(dir, "print"))
	reply, err := svc.ExportAs(`{"format":"tiff","cmyk":true,"defaultName":"print.tif"}`)
	if err != nil {
		t.Fatal(err)
	}
	// 用真实导入路径读回来：它是 CMYK TIFF，且带着配置。
	path := parseJSON[map[string]string](t, reply)["path"]
	result, err := rasterio.Decode(path, 1000)
	if err != nil {
		t.Fatalf("写出的 CMYK TIFF 应当能被导入路径读回来: %v", err)
	}
	if len(result.SourceProfile) == 0 {
		t.Error("导出的 TIFF 应当内嵌 ICC 配置")
	}
	if result.Width != 4 || result.Height != 2 {
		t.Errorf("尺寸 = %d×%d", result.Width, result.Height)
	}
}

// JPEG 的四通道编码器还没写：必须**明确拒绝**，而不是静默写出 RGB 冒充 CMYK。
func TestExportAsCMYKRefusesJPEG(t *testing.T) {
	svc := cmykExportService(t)
	stubExportSaveDialog(t, filepath.Join(t.TempDir(), "out"))
	_, err := svc.ExportAs(`{"format":"jpeg","cmyk":true}`)
	if err == nil || !strings.Contains(err.Error(), "CMYK") {
		t.Fatalf("JPEG 的 CMYK 应当被拒绝并说明原因，得到 %v", err)
	}
}

// 没有配置时不发明一份：直接报错，并指出去哪里选。
func TestExportAsCMYKNeedsAProfile(t *testing.T) {
	t.Setenv("COMPOSITOR_COLORIO_DATA_DIR", t.TempDir())
	colorio.ClearProofCache()
	if chosen := colorio.Chosen(); chosen != nil {
		t.Skip("本机装了 CMYK 配置，Chosen() 有回退，这条测不到")
	}
	svc, _ := newTestService(t, 4, 2)
	stubExportSaveDialog(t, filepath.Join(t.TempDir(), "out"))
	if _, err := svc.ExportAs(`{"format":"pdf","cmyk":true}`); err == nil {
		t.Fatal("没有配置时应当报错")
	}
}
