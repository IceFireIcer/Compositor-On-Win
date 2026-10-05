package watch

import (
	"sync"
	"time"
)

// Defaults for Coordinator, ported from Swift's ProjectController+ExternalChanges
// backoff: retries are spaced 250ms * 2^n and stop after a handful of attempts,
// so a busy-wait against a writer that never finishes always ends.
const (
	DefaultBackoffBase = 250 * time.Millisecond
	DefaultMaxAttempts = 8
)

// Coordinator keeps a project's content digest in step with its package on disk.
// The watcher reports raw events; the coordinator decides whether they mean
// anything: it compares content digests (Compute), waits out a writer that is
// still mid-write with an exponential backoff, and only then reports one stable
// digest via OnStable. Content that merely gets rewritten with the same bytes
// never reports.
//
// Typical wiring, mirroring Swift's ProjectController:
//
//	coordinator.Remember()            // after open / after save
//	watcher.Start(dir, coordinator.Notify)
//	coordinator.OnStable = func(digest string) { ... reload in place ... }
//
// OnStable is called from the coordinator's own goroutine, one call at a time;
// the digest handed over is the settled package content.
type Coordinator struct {
	// Base is the first backoff sleep; each unsettled check doubles it.
	// Set before the first Notify; zero or negative means DefaultBackoffBase.
	Base time.Duration
	// MaxAttempts caps the backoff: after this many unsettled checks the cycle
	// ends in a terminal state instead of retrying forever.
	// Set before the first Notify; zero or negative means DefaultMaxAttempts.
	MaxAttempts int
	// Digest computes the package digest; injectable for tests. Defaults to
	// Compute. May be swapped between cycles; tests do so under c.mu.
	Digest func(dir string) (string, error)
	// OnStable reports one settled external change. Nil is allowed.
	OnStable func(digest string)

	dir string

	mu        sync.Mutex
	timer     *time.Timer
	stopped   bool
	checking  bool
	pending   bool
	attempt   int
	havePrev  bool
	prev      string
	hasLast   bool
	lastKnown string
}

// NewCoordinator returns a coordinator for the package at dir with the default
// backoff; configure Base, MaxAttempts and Digest before the first Notify.
func NewCoordinator(dir string) *Coordinator {
	return &Coordinator{
		Base:        DefaultBackoffBase,
		MaxAttempts: DefaultMaxAttempts,
		Digest:      Compute,
		dir:         dir,
	}
}

// Remember records the package as it is now, so the next check compares against
// it. Call after a successful open and after every save, so the digest on disk
// is always the one last read or written. On error the baseline is cleared and
// the next change reports unconditionally.
func (c *Coordinator) Remember() error {
	digest, err := c.digestFn()(c.dir)
	c.mu.Lock()
	if err == nil {
		c.lastKnown, c.hasLast = digest, true
	} else {
		c.lastKnown, c.hasLast = "", false
	}
	c.mu.Unlock()
	return err
}

// Notify schedules a check for an external change. It never blocks: the check
// runs on its own goroutine, and a Notify that arrives while a check is running
// merges into that cycle, which re-examines the package until its content
// settles, then reports once. After Stop, Notify does nothing.
func (c *Coordinator) Notify() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.pending = true
	if c.checking {
		c.mu.Unlock()
		return
	}
	c.checking = true
	c.mu.Unlock()

	go c.step()
}

// Stop ends any running cycle and cancels a scheduled backoff. Once it returns,
// OnStable will not fire again. Stop is safe to call more than once.
func (c *Coordinator) Stop() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.stopped = true
	timer := c.timer
	c.timer = nil
	c.checking = false
	c.pending = false
	c.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
}

// step runs one check of the cycle: merge queued Notify calls, compare the
// digest against the previous poll, and either report (content settled on
// something new), drop (content unchanged), back off (writer still at work), or
// give up (attempt cap). It re-invokes itself via a timer while backing off and
// via afterSettled while work remains.
func (c *Coordinator) step() {
	c.mu.Lock()
	if c.stopped {
		c.endCycleLocked()
		c.mu.Unlock()
		return
	}
	c.pending = false // merge whatever queued up into this poll
	c.mu.Unlock()

	digest, err := c.digestFn()(c.dir)

	c.mu.Lock()
	if c.stopped {
		c.endCycleLocked()
		c.mu.Unlock()
		return
	}

	if err != nil {
		// A package caught half written yields an error: break the settle
		// chain and try again after a backoff.
		c.havePrev = false
	} else {
		settled := c.havePrev && digest == c.prev
		c.prev, c.havePrev = digest, true
		if settled {
			changed := !c.hasLast || digest != c.lastKnown
			if changed {
				c.lastKnown, c.hasLast = digest, true
			}
			c.attempt = 0
			c.mu.Unlock()
			if changed {
				c.fire(digest)
			}
			c.afterSettled()
			return
		}
	}

	// Unsettled (the digest keeps changing, or it errors): back off, bounded.
	c.attempt++
	if c.attempt > c.maxAttempts() {
		// Terminal state: the writer never settled. Report the last digest seen
		// as a best effort so the change is not lost, then end; the next event
		// on disk starts a fresh cycle. Without a single usable digest there is
		// nothing to report.
		final, have := c.prev, c.havePrev
		report := have && (!c.hasLast || final != c.lastKnown)
		if report {
			c.lastKnown, c.hasLast = final, true
		}
		c.endCycleLocked()
		c.mu.Unlock()
		if report {
			c.fire(final)
		}
		return
	}
	delay := c.base() * (1 << uint(c.attempt-1))
	c.timer = time.AfterFunc(delay, c.step)
	c.mu.Unlock()
}

// afterSettled continues the cycle when changes queued up while reporting, or
// ends it. The continuation runs on a fresh goroutine so OnStable callbacks
// cannot recurse unboundedly by calling Notify.
func (c *Coordinator) afterSettled() {
	c.mu.Lock()
	if c.stopped {
		c.endCycleLocked()
		c.mu.Unlock()
		return
	}
	if c.pending {
		c.mu.Unlock()
		go c.step()
		return
	}
	c.endCycleLocked()
	c.mu.Unlock()
}

func (c *Coordinator) endCycleLocked() {
	c.checking = false
	c.pending = false
	c.attempt = 0
	c.havePrev = false
}

func (c *Coordinator) fire(digest string) {
	if c.OnStable != nil {
		c.OnStable(digest)
	}
}

func (c *Coordinator) base() time.Duration {
	if c.Base <= 0 {
		return DefaultBackoffBase
	}
	return c.Base
}

func (c *Coordinator) maxAttempts() int {
	if c.MaxAttempts <= 0 {
		return DefaultMaxAttempts
	}
	return c.MaxAttempts
}

// digestFn returns the configured digest function, defaulting to Compute. The
// field is read under the lock so tests can swap it between cycles.
func (c *Coordinator) digestFn() func(dir string) (string, error) {
	c.mu.Lock()
	fn := c.Digest
	c.mu.Unlock()
	if fn != nil {
		return fn
	}
	return Compute
}
