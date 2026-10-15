package rasterio

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDecodeEXRKeepsHighlights imports a third-party EXR (ffmpeg wrote it)
// and checks the layer stays HDR: the deep raster is what gets saved, and
// the 8-bit copy is only the clipped display.
func TestDecodeEXRKeepsHighlights(t *testing.T) {
	src := filepath.Join("..", "hdrio", "testdata", "ffmpeg-half-none.exr")
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("夹具缺失: %v", err)
	}
	got, err := Decode(src, 1<<20)
	if err != nil {
		t.Fatalf("导入 EXR: %v", err)
	}
	if got.Bitmap == nil || got.Bitmap.Deep == nil || !got.Bitmap.Deep.HDR {
		t.Fatal("EXR 导入应得到 HDR 图层")
	}
	if got.Width != 4 || got.Height != 4 {
		t.Fatalf("尺寸 %d×%d，想要 4×4", got.Width, got.Height)
	}
	if got.Name != "ffmpeg-half-none" {
		t.Fatalf("图层名 = %q", got.Name)
	}
}

func TestDecodeRadianceKeepsHighlights(t *testing.T) {
	src := filepath.Join("..", "hdrio", "testdata", "ffmpeg-linear.hdr")
	got, err := Decode(src, 1<<20)
	if err != nil {
		t.Fatalf("导入 Radiance: %v", err)
	}
	if got.Bitmap == nil || got.Bitmap.Deep == nil || !got.Bitmap.Deep.HDR {
		t.Fatal("Radiance HDR 导入应得到 HDR 图层")
	}
}
