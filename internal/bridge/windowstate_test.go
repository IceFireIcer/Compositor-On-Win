package bridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowStoreRoundTrip(t *testing.T) {
	store := NewWindowStoreAt(filepath.Join(t.TempDir(), "window.json"))
	if err := store.Save(1280, 800); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 1280 || got.Height != 800 {
		t.Fatalf("load = %+v", got)
	}
}

func TestWindowStoreMissingDefaultsToZero(t *testing.T) {
	store := NewWindowStoreAt(filepath.Join(t.TempDir(), "window.json"))
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 0 || got.Height != 0 {
		t.Fatalf("missing file should give zero state, got %+v", got)
	}
}

func TestWindowStoreCorruptErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "window.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewWindowStoreAt(path)
	if _, err := store.Load(); err == nil {
		t.Fatal("corrupt file should error")
	}
}

func TestWindowStoreSaveCreatesDir(t *testing.T) {
	store := NewWindowStoreAt(filepath.Join(t.TempDir(), "nested", "dir", "window.json"))
	if err := store.Save(100, 100); err != nil {
		t.Fatal(err)
	}
}
