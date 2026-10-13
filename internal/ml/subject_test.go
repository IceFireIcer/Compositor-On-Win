package ml

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreferredModelPath(t *testing.T) {
	root := t.TempDir()
	bundled := filepath.Join(root, "installation", "models", ModelFileName)
	cached := filepath.Join(root, "appdata", ModelFileName)
	if got := preferredModelPath(bundled, cached); got != cached {
		t.Fatalf("missing packaged model: got %q, want cache %q", got, cached)
	}
	if err := os.MkdirAll(filepath.Dir(bundled), 0o755); err != nil {
		t.Fatal(err)
	}
	// Even a damaged package must be selected and rejected by verification;
	// silently falling back could hide a broken installation.
	if err := os.WriteFile(bundled, []byte("truncated package"), 0o444); err != nil {
		t.Fatal(err)
	}
	if got := preferredModelPath(bundled, cached); got != bundled {
		t.Fatalf("packaged model: got %q, want %q", got, bundled)
	}
	if err := verifyModel(bundled); err == nil {
		t.Fatal("accepted damaged packaged model")
	}
	if got := preferredModelPath("", cached); got != cached {
		t.Fatalf("unavailable executable path: got %q, want %q", got, cached)
	}
}

func TestEnsureModelRejectsCorruptCacheWithoutDownloading(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	if err := os.MkdirAll(ModelDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	path := cachedModelPath()
	for _, tc := range []struct {
		name    string
		size    int
		message string
	}{
		{"truncated", 32, "不完整"},
		{"wrong hash", modelMinBytes + 1, "SHA-256"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := make([]byte, tc.size)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			err := EnsureModel()
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("got %v, want corruption error containing %q", err, tc.message)
			}
			info, err := os.Stat(path)
			if err != nil || info.Size() != int64(tc.size) {
				t.Fatalf("corrupt cache should remain for diagnosis: stat=%v err=%v", info, err)
			}
		})
	}
}
