package watch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestCoordinatorReportsChangedContentOnce(t *testing.T) {
	dir := setupProject(t, manifestV1, map[string]string{"A.png": pngOne})

	c := NewCoordinator(dir)
	c.Base = 10 * time.Millisecond
	defer c.Stop()

	var r recorder
	c.OnStable = r.record
	if err := c.Remember(); err != nil {
		t.Fatalf("Remember: %v", err)
	}

	// An external write burst lands, then the watcher's onChange fires.
	if err := os.WriteFile(filepath.Join(dir, "images", "A.png"), []byte(pngTwo), 0o644); err != nil {
		t.Fatal(err)
	}
	writeManifestAtomically(t, dir, manifestV2)
	c.Notify()

	want, err := Compute(dir)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool { return r.count() > 0 }, "OnStable never fired")
	if got := r.digest(); got != want {
		t.Fatalf("OnStable digest = %s, want %s", got, want)
	}
	time.Sleep(200 * time.Millisecond)
	if got := r.count(); got != 1 {
		t.Fatalf("OnStable fired %d times, want 1", got)
	}
}

func TestCoordinatorDropsRewritesWithSameContent(t *testing.T) {
	dir := setupProject(t, manifestV1, map[string]string{"A.png": pngOne})

	c := NewCoordinator(dir)
	c.Base = 10 * time.Millisecond
	defer c.Stop()

	var r recorder
	c.OnStable = r.record
	if err := c.Remember(); err != nil {
		t.Fatalf("Remember: %v", err)
	}

	// A sync client rewrote the package with identical bytes: no reload.
	if err := os.WriteFile(filepath.Join(dir, "images", "A.png"), []byte(pngOne), 0o644); err != nil {
		t.Fatal(err)
	}
	writeManifestAtomically(t, dir, manifestV1)
	c.Notify()

	time.Sleep(300 * time.Millisecond)
	if got := r.count(); got != 0 {
		t.Fatalf("OnStable fired %d times for content-identical rewrite, want 0", got)
	}
}

func TestCoordinatorBacksOffWhileWriterIsMidWrite(t *testing.T) {
	c := NewCoordinator(t.TempDir())
	c.Base = 5 * time.Millisecond
	defer c.Stop()

	// The package keeps changing for the first four polls (a writer still at
	// work), then settles. OnStable must wait for the settled digest.
	var calls int32
	c.Digest = func(string) (string, error) {
		n := atomic.AddInt32(&calls, 1)
		if n <= 4 {
			return fmt.Sprintf("mid-write-%d", n), nil
		}
		return "final", nil
	}
	var r recorder
	c.OnStable = r.record

	c.Notify()
	waitFor(t, 2*time.Second, func() bool { return r.count() > 0 }, "OnStable never fired")
	if got := r.digest(); got != "final" {
		t.Fatalf("OnStable digest = %q, want %q", got, "final")
	}
	if got := atomic.LoadInt32(&calls); got < 5 {
		t.Fatalf("digest computed %d times, want at least 5 (backoff must poll until stable)", got)
	}
	time.Sleep(100 * time.Millisecond)
	if got := r.count(); got != 1 {
		t.Fatalf("OnStable fired %d times, want 1", got)
	}
}

func TestCoordinatorGivesUpAfterAttemptCap(t *testing.T) {
	c := NewCoordinator(t.TempDir())
	c.Base = 1 * time.Millisecond
	c.MaxAttempts = 3
	defer c.Stop()

	// A writer that never settles: the backoff must terminate instead of
	// spinning forever, reporting the last digest seen (best effort).
	var calls int32
	c.Digest = func(string) (string, error) {
		n := atomic.AddInt32(&calls, 1)
		return fmt.Sprintf("changing-%d", n), nil
	}
	var r recorder
	c.OnStable = r.record

	c.Notify()
	waitFor(t, 2*time.Second, func() bool { return r.count() > 0 }, "coordinator never reached a terminal state")
	if got := atomic.LoadInt32(&calls); got != int32(c.MaxAttempts+1) {
		t.Fatalf("digest computed %d times, want exactly MaxAttempts+1 = %d", got, c.MaxAttempts+1)
	}
	time.Sleep(100 * time.Millisecond)
	if got := r.count(); got != 1 {
		t.Fatalf("OnStable fired %d times, want 1", got)
	}
}

func TestCoordinatorTerminalWhenDigestNeverComputes(t *testing.T) {
	c := NewCoordinator(t.TempDir())
	c.Base = 1 * time.Millisecond
	c.MaxAttempts = 2
	defer c.Stop()

	var calls int32
	halfWritten := errors.New("package half written")
	c.Digest = func(string) (string, error) {
		atomic.AddInt32(&calls, 1)
		return "", halfWritten
	}
	var r recorder
	c.OnStable = r.record

	c.Notify()
	time.Sleep(200 * time.Millisecond)
	if got := r.count(); got != 0 {
		t.Fatalf("OnStable fired %d times despite persistent errors, want 0", got)
	}
	if got := atomic.LoadInt32(&calls); got != int32(c.MaxAttempts+1) {
		t.Fatalf("digest computed %d times, want exactly %d (bounded, no infinite loop)", got, c.MaxAttempts+1)
	}

	// The writer finished; the next change is checked afresh.
	setDigest(c, func(string) (string, error) { return "recovered", nil })
	c.Notify()
	waitFor(t, 2*time.Second, func() bool { return r.count() > 0 }, "coordinator stuck after error cycle")
	if got := r.digest(); got != "recovered" {
		t.Fatalf("OnStable digest = %q, want %q", got, "recovered")
	}
}

func TestCoordinatorMergesNotifyDuringCheck(t *testing.T) {
	c := NewCoordinator(t.TempDir())
	c.Base = 1 * time.Millisecond
	defer c.Stop()

	// Hold the first digest computation to simulate a check in progress; a
	// Notify arriving then must merge into the running cycle, not start a new one.
	release := make(chan struct{})
	var calls int32
	c.Digest = func(string) (string, error) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			<-release
		}
		return "stable", nil
	}
	var r recorder
	c.OnStable = r.record

	c.Notify()
	time.Sleep(50 * time.Millisecond)
	c.Notify()
	close(release)

	waitFor(t, 2*time.Second, func() bool { return r.count() > 0 }, "OnStable never fired")
	time.Sleep(100 * time.Millisecond)
	if got := r.count(); got != 1 {
		t.Fatalf("OnStable fired %d times, want 1", got)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("digest computed %d times, want 2 (second Notify merged into running cycle)", got)
	}
}

func TestCoordinatorStopQuiescesCallbacks(t *testing.T) {
	dir := setupProject(t, manifestV1, map[string]string{"A.png": pngOne})

	c := NewCoordinator(dir)
	c.Base = 50 * time.Millisecond

	var r recorder
	c.OnStable = r.record
	if err := c.Remember(); err != nil {
		t.Fatalf("Remember: %v", err)
	}

	writeManifestAtomically(t, dir, manifestV2)
	c.Notify()
	time.Sleep(10 * time.Millisecond) // the cycle is now parked in a backoff sleep

	c.Stop()
	// Stop must cancel the pending backoff: no more polls, no callback.
	time.Sleep(3 * c.Base)
	if got := r.count(); got != 0 {
		t.Fatalf("OnStable fired %d times after Stop, want 0", got)
	}

	// Late Notify calls are ignored too.
	c.Notify()
	time.Sleep(3 * c.Base)
	if got := r.count(); got != 0 {
		t.Fatalf("OnStable fired %d times after Stop on late Notify, want 0", got)
	}
}
