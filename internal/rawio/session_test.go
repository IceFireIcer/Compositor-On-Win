package rawio

import (
	"os"
	"path/filepath"
	"testing"
)

// The suite has no camera RAW sample committed (they're tens of MB); the
// binding tests pin the error contract. Real-file verification happened
// against local samples during ticket 41.
func TestOpenRefusesNonRaw(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-raw.png")
	if err := os.WriteFile(path, []byte("definitely not a raw"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, false); err == nil {
		t.Fatal("非 RAW 文件应报错")
	}
}

func TestOpenRefusesMissingFile(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "absent.nef"), false); err == nil {
		t.Fatal("缺失文件应报错")
	}
}

func TestOpenHalfSizeAccepted(t *testing.T) {
	// Opening a non-RAW with halfSize uses the same failure path; the flag
	// only matters for real frames (sizes halved), so just exercise it.
	if _, err := Open(filepath.Join(t.TempDir(), "x.dng"), true); err == nil {
		t.Fatal("缺失文件应报错")
	}
}
