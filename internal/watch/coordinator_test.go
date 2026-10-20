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

// settle 之后必须把退避计数**清零**。否则下一轮外部变更会从更大的退避开始，更快撞上
// MaxAttempts 上限、走向「放弃」分支 —— 也就是**漏掉一次变化**。
//
// 这条是变异审计查出来的：把 settle 路径里的 `c.attempt = 0` 改成空操作，全部既有测试照样
// 通过 —— 因为没人观察过 settle 之后的 attempt。
//
// **诚实记一笔**：把那处改成空操作后，这条测试**仍然通过** —— 因为 `endCycleLocked()` 里还有
// 一次 `c.attempt = 0`（`coordinator.go:218`），两条路径都清零。也就是说 settle 路径那次重置是
// **冗余的防御**，不是唯一的依赖。本测试钉住的是「settle 之后 attempt 为 0」这个可观察契约，
// 而不是某一行代码。
func TestCoordinatorResetsTheBackoffAfterSettling(t *testing.T) {
	c := NewCoordinator(t.TempDir())
	c.Base = 2 * time.Millisecond
	c.MaxAttempts = 10

	// 前三次 poll 的摘要各不相同（一直「不稳」），之后稳定下来。
	var calls atomic.Int32
	c.Digest = func(string) (string, error) {
		n := calls.Add(1)
		if n <= 3 {
			return fmt.Sprintf("d%d", n), nil
		}
		return "steady", nil
	}

	fired := make(chan string, 1)
	c.OnStable = func(digest string) {
		select {
		case fired <- digest:
		default:
		}
	}
	defer c.Stop()

	c.Notify()
	select {
	case digest := <-fired:
		if digest != "steady" {
			t.Fatalf("OnStable digest = %q，想要 steady", digest)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnStable 在 5s 内没有触发")
	}

	// 走到这里说明上面那轮是「经过多次不稳才 settle」的，attempt 必然涨到 3 以上。
	if got := calls.Load(); got < 4 {
		t.Fatalf("Digest 只被调用 %d 次，前提不成立（需要先经历几次不稳）", got)
	}
	c.mu.Lock()
	attempt := c.attempt
	c.mu.Unlock()
	if attempt != 0 {
		t.Fatalf("settle 之后 attempt = %d，想要 0 —— 不清零会让下一次变更从更大的退避起步", attempt)
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
