package memory

import (
	"testing"
	"time"
)

// Mirror of TestWatcherStartSamplesImmediatelyAndStopsCleanly's synchronization shape.
func TestProbeRace2(t *testing.T) {
	status := Status{Total: 16 << 30, Available: 1 << 30}
	w := NewWatcher(func() Status { return status }, time.Hour, nil)
	seen := make(chan Level, 8)
	w.onLevel = func(level Level) { seen <- level }
	w.Start()
	<-seen
	status = Status{Total: 16 << 30, Available: 12 << 30}
	w.Step()
	<-seen
	w.Stop()
}
