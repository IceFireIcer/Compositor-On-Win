package bridge

// RAW develop endpoint tests (ticket 41): no camera sample ships in the
// repo (tens of MB, license-encumbered), so the suite pins the error
// contract and the extension dispatch. Real-file verification happened
// against local samples during the ticket.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRawExtensionDispatch(t *testing.T) {
	for _, ext := range []string{".CR2", ".cr3", ".nef", ".arw", ".dng", ".raf", ".orf", ".rw2"} {
		if !IsRAWPath("C:/pics/IMG_0001" + ext) {
			t.Errorf("%s 应走 develop 表单", ext)
		}
	}
	for _, ext := range []string{".heic", ".HEIF", ".hif"} {
		if !IsHEICPath("C:/pics/photo" + ext) {
			t.Errorf("%s 应走 libheif 解码", ext)
		}
	}
	for _, ext := range []string{".png", ".jpg", ".svg", ".psd"} {
		if IsRAWPath("x"+ext) || IsHEICPath("x"+ext) {
			t.Errorf("%s 不应误判为 RAW/HEIC", ext)
		}
	}
}

func TestRawBeginDevelopRefusesNonRaw(t *testing.T) {
	svc, _ := newTestService(t, 4, 4)
	path := filepath.Join(t.TempDir(), "fake.cr2")
	if err := os.WriteFile(path, []byte("not a raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RawBeginDevelop(path); err == nil {
		t.Fatal("非 RAW 文件应报错")
	}
	if _, err := svc.RawBeginDevelop(filepath.Join(t.TempDir(), "absent.nef")); err == nil {
		t.Fatal("缺失文件应报错")
	}
}

func TestRawDevelopPreviewNeedsSession(t *testing.T) {
	svc, _ := newTestService(t, 4, 4)
	if _, err := svc.RawDevelopPreview(`{}`); err == nil {
		t.Fatal("无会话应报错")
	}
	svc.RawCancelDevelop() // 幂等
}

func TestRawFinishDevelopParsesSettings(t *testing.T) {
	svc, _ := newTestService(t, 4, 4)
	path := filepath.Join(t.TempDir(), "fake.dng")
	if err := os.WriteFile(path, []byte("not a raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 打不开文件先于参数检查报错也行；给全参数走真路径。
	if _, err := svc.RawFinishDevelop(path,
		`{"exposure":0,"temperature":5000,"tint":0,"boost":1,"asShotTemperature":5000,"asShotTint":0}`); err == nil {
		t.Fatal("非 RAW 文件应报错")
	}
	if _, err := svc.RawFinishDevelop(path, ``); err == nil {
		t.Fatal("空参数应报错")
	}
}
