package heicio

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// No HEIC sample is committed (license uncertainty around shipping real
// camera files); the binding tests pin the error contract. Real-file
// verification happened against a locally produced sample in ticket 41.
func TestDecodeRefusesNonHeic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake.heic")
	if err := os.WriteFile(path, []byte("not a heif container"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(path); err == nil {
		t.Fatal("坏容器应报错")
	}
}

func TestDecodeRefusesMissingFile(t *testing.T) {
	if _, err := Decode(filepath.Join(t.TempDir(), "absent.heic")); err == nil {
		t.Fatal("缺失文件应报错")
	}
}

// 相机写出的 .hif/.heif 与 .heic 走同一条 libheif 路径：扩展名不改变错误
// 契约（路由在 bridge 的 IsHEICPath）。
func TestDecodeCameraHEIFExtensionsShareTheHeicPath(t *testing.T) {
	for _, ext := range []string{"hif", "heif", "heic"} {
		path := filepath.Join(t.TempDir(), "camera."+ext)
		if err := os.WriteFile(path, []byte("not a heif container"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Decode(path); err == nil || !errors.Is(err, ErrUnreadable) {
			t.Errorf("%s：坏容器应报 ErrUnreadable，得到 %v", ext, err)
		}
	}
}

// libheif 解不开的编码（AV1、JPEG 2000、VVC…）必须报出「不支持的编码」，
// 不能混进「文件已损坏」，也不能返回空图。映射是纯函数，所以不依赖真文件。
func TestClassifyHEIFErrorSeparatesUnsupportedCodec(t *testing.T) {
	cases := []struct {
		name    string
		code    int
		subcode int
		want    error
	}{
		{"缺少解码器", heifCodeUnsupportedFeature, heifSubUnsupportedCodec, ErrUnsupportedCodec},
		{"未知图像类型", heifCodeUnsupportedFeature, heifSubUnsupportedImageType, ErrUnsupportedCodec},
		{"未知必要属性", heifCodeUnsupportedFeature, heifSubUnsupportedEssentialProperty, ErrUnsupportedCodec},
		{"没有匹配的解码器插件", heifCodePluginLoading, heifSubNoMatchingDecoderInstalled, ErrUnsupportedCodec},
		// libheif 的 Unsupported_filetype 是"这不是本机能读的文件类型"
		// （heif_error.h:45），不是"缺解码器"——改名成 .heic 的 JPEG 落在这里，
		// 报"文件可能已损坏或不可用"比报"不支持 AV1/J2K 编码"更诚实。
		{"不是本机能读的文件类型", heifCodeUnsupportedFiletype, 0, ErrUnreadable},
		{"损坏的容器", heifCodeInvalidInput, heifSubNoFtypBox, ErrUnreadable},
		{"解码器插件报错", heifCodeDecoderPluginError, 0, ErrUnreadable},
	}
	for _, c := range cases {
		err := classifyHEIFError(c.code, c.subcode, "libheif says so")
		if !errors.Is(err, c.want) {
			t.Errorf("%s：得到 %v，想要 %v", c.name, err, c.want)
			continue
		}
		if c.want == ErrUnsupportedCodec && errors.Is(err, ErrUnreadable) {
			t.Errorf("%s：不支持的编码不得同时算作「无法读取」", c.name)
		}
		if !strings.Contains(err.Error(), "libheif says so") {
			t.Errorf("%s：应带上 libheif 的原始说明，得到 %v", c.name, err)
		}
	}
	// libheif 没给说明时也要有可读的原因。
	if msg := classifyHEIFError(heifCodeUnsupportedFeature, heifSubUnsupportedCodec, "").Error(); msg == "" {
		t.Error("空说明也应有错误文本")
	}
}
