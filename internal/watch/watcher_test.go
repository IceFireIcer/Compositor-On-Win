package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherCoalescesWriteBurstIntoOneCallback(t *testing.T) {
	dir := setupProject(t, manifestV1, map[string]string{"A.png": pngOne})

	w := NewWatcher()
	w.Coalesce = 150 * time.Millisecond
	defer w.Stop()

	var c counter
	if err := w.Start(dir, c.bump); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// One write burst per the agent contract: PNGs first, then the manifest
	// renamed over the old one. Several events, exactly one report.
	if err := os.WriteFile(filepath.Join(dir, "images", "A.png"), []byte(pngTwo), 0o644); err != nil {
		t.Fatal(err)
	}
	writeManifestAtomically(t, dir, manifestV2)

	waitFor(t, 3*time.Second, func() bool { return c.count() > 0 }, "no callback after write burst")
	time.Sleep(3 * w.Coalesce)
	if got := c.count(); got != 1 {
		t.Fatalf("callbacks after one write burst = %d, want 1", got)
	}
}

func TestWatcherSurvivesAtomicRenameOverManifest(t *testing.T) {
	dir := setupProject(t, manifestV1, map[string]string{"A.png": pngOne})

	w := NewWatcher()
	w.Coalesce = 150 * time.Millisecond
	defer w.Stop()

	var c counter
	if err := w.Start(dir, c.bump); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// An ordinary image write is noticed.
	if err := os.WriteFile(filepath.Join(dir, "images", "A.png"), []byte(pngTwo), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool { return c.count() >= 1 }, "no callback for image write")
	time.Sleep(3 * w.Coalesce)
	if got := c.count(); got != 1 {
		t.Fatalf("callbacks after first write = %d, want 1", got)
	}

	// Renaming a sibling over manifest.json retires the watch held on the old
	// file; the watcher must re-arm and still report the next change.
	writeManifestAtomically(t, dir, manifestV2)
	waitFor(t, 3*time.Second, func() bool { return c.count() >= 2 }, "no callback after atomic rename over manifest")
	time.Sleep(3 * w.Coalesce)

	// The images watch is alive as well after the swap.
	if err := os.WriteFile(filepath.Join(dir, "images", "A.png"), []byte(pngThree), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 3*time.Second, func() bool { return c.count() >= 3 }, "no callback for image write after manifest swap")
	time.Sleep(3 * w.Coalesce)
	if got := c.count(); got != 3 {
		t.Fatalf("callbacks = %d, want 3 (one per separated write)", got)
	}
}

func TestWatcherNoticesPackageCreatedIncrementally(t *testing.T) {
	// A package whose images/ dir appears only after Start: the re-arm loop
	// must pick it up once the first events flow.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifestV1), 0o644); err != nil {
		t.Fatal(err)
	}

	w := NewWatcher()
	w.Coalesce = 150 * time.Millisecond
	defer w.Stop()

	var c counter
	if err := w.Start(dir, c.bump); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "images"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "images", "A.png"), []byte(pngOne), 0o644); err != nil {
		t.Fatal(err)
	}
	writeManifestAtomically(t, dir, manifestV2)

	waitFor(t, 3*time.Second, func() bool { return c.count() > 0 }, "no callback after images dir appears")
}

func TestWatcherStopQuiescesCallbacks(t *testing.T) {
	dir := setupProject(t, manifestV1, map[string]string{"A.png": pngOne})

	w := NewWatcher()
	w.Coalesce = 100 * time.Millisecond

	var c counter
	if err := w.Start(dir, c.bump); err != nil {
		t.Fatalf("Start: %v", err)
	}

	writeManifestAtomically(t, dir, manifestV2)
	waitFor(t, 3*time.Second, func() bool { return c.count() > 0 }, "no callback before Stop")

	w.Stop()
	before := c.count()

	// Writes after Stop must not surface, even after several coalesce windows.
	if err := os.WriteFile(filepath.Join(dir, "images", "A.png"), []byte(pngTwo), 0o644); err != nil {
		t.Fatal(err)
	}
	writeManifestAtomically(t, dir, manifestV1)
	time.Sleep(5 * w.Coalesce)
	if got := c.count(); got != before {
		t.Fatalf("callbacks after Stop: got %d, want %d", got, before)
	}
}

func TestWatcherStartTwiceFails(t *testing.T) {
	dir := setupProject(t, manifestV1, nil)
	w := NewWatcher()
	defer w.Stop()
	if err := w.Start(dir, func() {}); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := w.Start(dir, func() {}); err == nil {
		t.Fatal("second Start should fail")
	}
}

func TestWatcherStartOnMissingDirFails(t *testing.T) {
	w := NewWatcher()
	defer w.Stop()
	missing := filepath.Join(t.TempDir(), "nope", "deeper")
	if err := w.Start(missing, func() {}); err == nil {
		t.Fatal("Start on a missing directory should fail")
	}
}
