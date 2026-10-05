package watch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// DefaultCoalescing matches Swift's ProjectWatcher.coalescing: how long to wait
// after the last event before reporting, so a save that touches several files
// reports once.
const DefaultCoalescing = 300 * time.Millisecond

// Watcher tells its owner when a project package changes on disk, whoever
// changed it: another app, an agent, a sync client, a git checkout. It watches
// the package folder, its manifest.json and its images folder, so there is no
// polling and no dependency on the writer using file coordination. Events are
// coalesced (default DefaultCoalescing) and reported by calling onChange once
// per quiet period.
//
// A package is replaced atomically by renaming a sibling over it, which retires
// the watch held on the old file; every event therefore re-arms the watch by
// path, so the new package is watched after the swap. This matches Swift's
// ProjectWatcher (reference/Swift/Compositor/IO/ProjectWatcher.swift): events
// there also re-arm the dispatch sources by path, retrying briefly while the
// package is mid-replacement.
//
// onChange is called from the watcher's own goroutine, one call at a time, in
// event order; it should be quick (hand the work to a Coordinator).
type Watcher struct {
	// Coalesce is the quiet period after the last event before onChange runs.
	// Set before Start; zero or negative means DefaultCoalescing.
	Coalesce time.Duration

	fs           *fsnotify.Watcher
	dir          string
	onChange     func()
	coalesce     time.Duration
	rearmQuiet   time.Duration // settle time before re-arming after an event
	rearmRetries int           // re-arm attempts while the package is mid-swap
	rearmRetry   time.Duration // pause between re-arm attempts

	mu      sync.Mutex
	started bool
	stopped bool

	done   chan struct{}
	signal chan struct{}
	wg     sync.WaitGroup
}

// NewWatcher returns a watcher with the default timing; configure Coalesce
// before Start.
func NewWatcher() *Watcher {
	return &Watcher{
		Coalesce:     DefaultCoalescing,
		rearmQuiet:   100 * time.Millisecond,
		rearmRetries: 20,
		rearmRetry:   50 * time.Millisecond,
		done:         make(chan struct{}),
		signal:       make(chan struct{}, 1),
	}
}

// Start watches the package at dir and reports coalesced changes by calling
// onChange. It fails if the watcher is already started or if dir cannot be
// watched; a missing manifest.json or images folder is tolerated, as writers
// create both and the re-arm loop picks the watches up.
func (w *Watcher) Start(dir string, onChange func()) error {
	// Held for the whole start so a failed Start leaves the watcher untouched
	// (no started flag, no half-created fsnotify watcher).
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return errors.New("watch: watcher already started")
	}
	if w.stopped {
		return errors.New("watch: watcher was stopped")
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("watch: %w", err)
	}
	if info, err := os.Stat(abs); err != nil {
		return fmt.Errorf("watch: %w", err)
	} else if !info.IsDir() {
		return fmt.Errorf("watch: %s is not a directory", abs)
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("watch: %w", err)
	}

	w.fs = fsw
	w.dir = abs
	w.onChange = onChange
	w.coalesce = w.Coalesce
	if w.coalesce <= 0 {
		w.coalesce = DefaultCoalescing
	}

	if err := w.arm(); err != nil {
		fsw.Close()
		w.fs = nil
		return err
	}

	w.started = true
	w.wg.Add(2)
	go w.pump()
	go w.rearmWorker()
	return nil
}

// watchedPaths mirrors Swift's watchedPaths: the package folder, its manifest
// and its images folder.
func (w *Watcher) watchedPaths() []string {
	return []string{
		w.dir,
		filepath.Join(w.dir, "manifest.json"),
		filepath.Join(w.dir, "images"),
	}
}

// arm adds a watch per path. Only the package folder itself is required; the
// manifest and images folder may not exist yet and are picked up by re-arm.
func (w *Watcher) arm() error {
	for i, path := range w.watchedPaths() {
		if _, err := os.Stat(path); err != nil {
			if i == 0 {
				return fmt.Errorf("watch: %w", err)
			}
			continue
		}
		if err := w.fs.Add(path); err != nil {
			if i == 0 {
				return fmt.Errorf("watch: %w", err)
			}
		}
	}
	return nil
}

// Stop stops watching and waits until pending work has ended: once it returns,
// no further onChange calls run. Stop is safe to call more than once.
func (w *Watcher) Stop() {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.stopped = true
	w.mu.Unlock()

	close(w.done)
	if w.fs != nil {
		w.fs.Close()
	}
	w.wg.Wait()
}

// pump serializes fsnotify events into coalesced onChange calls.
func (w *Watcher) pump() {
	defer w.wg.Done()

	var timer *time.Timer
	var timerC <-chan time.Time
	for {
		select {
		case <-w.done:
			return

		case event, ok := <-w.fs.Events:
			if !ok {
				return
			}
			if event.Op == 0 {
				continue
			}
			// Any event may mean the package was swapped underneath us: ask the
			// re-arm worker to re-arm by path once the writer has finished.
			w.requestRearm()
			// Coalesce: each event pushes the report one quiet period out.
			if timer != nil {
				timer.Stop()
			}
			timer = time.NewTimer(w.coalesce)
			timerC = timer.C

		case err, ok := <-w.fs.Errors:
			if !ok {
				return
			}
			// Watches that vanish mid-swap surface here; the re-arm worker
			// restores them, so the error itself needs no handling.
			_ = err

		case <-timerC:
			timerC = nil
			if w.isStopped() {
				return
			}
			w.onChange()
		}
	}
}

// requestRearm nudges the re-arm worker, collapsing a burst of events into one.
func (w *Watcher) requestRearm() {
	select {
	case w.signal <- struct{}{}:
	default:
	}
}

// rearmWorker re-arms the watches by path after events, waiting for a small
// quiet period first so a package mid-replacement is not re-armed half swapped.
func (w *Watcher) rearmWorker() {
	defer w.wg.Done()
	for {
		select {
		case <-w.done:
			return
		case <-w.signal:
		}
		if !w.sleep(w.rearmQuiet) {
			return
		}
		w.rearm()
	}
}

// rearm re-adds every watch. Removing first makes the add re-resolve the path,
// which is what re-mounts a watch retired by an atomic rename; paths that do
// not exist yet (a package half replaced) are retried with backoff, as Swift
// retries its re-arm task while paths are missing.
func (w *Watcher) rearm() {
	for attempt := 0; attempt < w.rearmRetries; attempt++ {
		allArmed := true
		for i, path := range w.watchedPaths() {
			// Not-watched paths report an error on Remove; ignored on purpose.
			_ = w.fs.Remove(path)
			if _, err := os.Stat(path); err != nil {
				// Missing manifest.json or images mid-swap: retry shortly. The
				// package folder itself was stat'ed at Start and stays required.
				allArmed = false
				continue
			}
			if err := w.fs.Add(path); err != nil {
				allArmed = false
				_ = i
			}
		}
		if allArmed {
			return
		}
		if !w.sleep(w.rearmRetry) {
			return
		}
	}
	// Out of retries: the watches that did land keep the watcher alive (the
	// package folder watch covers the whole package), matching Swift, which
	// gives up its re-arm task after 20 tries as well.
}

func (w *Watcher) sleep(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-w.done:
		return false
	case <-timer.C:
		return true
	}
}

func (w *Watcher) isStopped() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stopped
}
