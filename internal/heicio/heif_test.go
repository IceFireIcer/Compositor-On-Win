package heicio

import (
	"os"
	"path/filepath"
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
