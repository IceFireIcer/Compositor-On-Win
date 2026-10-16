package render

// 票 87：CMYK 编码器**拒绝写出坏文件**的那几条分支。
//
// 这些分支此前只被 ExportAs 的端到端用例间接走过，而端到端总是带着一份配置 —— 所以
// 「没有配置就不写 PDF」这条从没被执行过。编码器是公开 API，直接钉住它自己的拒绝行为。

import (
	"strings"
	"testing"
)

func TestCMYKEncodersRefuseAMismatchedInkBuffer(t *testing.T) {
	// 4×1 需要 16 字节；给 12 字节。
	short := make([]uint8, 12)
	cases := []struct {
		name string
		call func() error
	}{
		{"TIFF", func() error { _, err := EncodeCMYKTIFFWithDPI(short, 4, 1, 72, nil); return err }},
		{"PDF", func() error { _, err := EncodeCMYKPDFWithDPI(short, 4, 1, 72, []byte{1}); return err }},
		{"JPEG", func() error { _, err := EncodeCMYKJPEG(short, 4, 1, 90); return err }},
	}
	for _, c := range cases {
		err := c.call()
		if err == nil {
			t.Errorf("%s 收到长度不符的油墨缓冲应当报错", c.name)
			continue
		}
		if !strings.Contains(err.Error(), "油墨缓冲") {
			t.Errorf("%s 的错误应当说明是缓冲长度问题，得到 %v", c.name, err)
		}
	}
}

// PDF 的图必须说明自己在什么油墨里；没有配置时不能写一个不说明的文件。
func TestCMYKPDFRefusesToWriteWithoutAProfile(t *testing.T) {
	ink := make([]uint8, 1*1*4)
	_, err := EncodeCMYKPDFWithDPI(ink, 1, 1, 72, nil)
	if err == nil {
		t.Fatal("没有配置时应当拒绝写 PDF")
	}
	if !strings.Contains(err.Error(), "ICC") {
		t.Fatalf("错误应当说明缺的是 ICC 配置，得到 %v", err)
	}
}

// 尺寸无效也要挡住，否则下游会拿着 0 去分配。
func TestCMYKEncodersRefuseBadDimensions(t *testing.T) {
	if _, err := EncodeCMYKTIFFWithDPI(nil, 0, 1, 72, nil); err == nil {
		t.Error("TIFF 的 0 宽应当报错")
	}
	if _, err := EncodeCMYKPDFWithDPI(nil, 1, 0, 72, []byte{1}); err == nil {
		t.Error("PDF 的 0 高应当报错")
	}
	if _, err := EncodeCMYKJPEG(nil, 0, 0, 90); err == nil {
		t.Error("JPEG 的 0×0 应当报错")
	}
}
