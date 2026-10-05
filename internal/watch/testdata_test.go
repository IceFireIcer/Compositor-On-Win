package watch

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Fixtures for a minimal .comp package (see docs/writing-comp-files.md): images/
// holds the layer PNGs, manifest.json is written last via an atomic rename.
const (
	manifestV1 = `{"format":"com.compositor.project","version":11,"width":100,"height":50,"layers":[]}`
	manifestV2 = `{"format":"com.compositor.project","version":11,"width":100,"height":50,"layers":[{"id":"A","name":"Edited"}]}`

	pngOne   = "png-bytes-v1"
	pngTwo   = "png-bytes-v2-with-more-bytes"
	pngThree = "png-bytes-v3"
)

// setupProject creates a package on disk in a fresh temp dir and returns its path.
func setupProject(t *testing.T, manifest string, images map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "images"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range images {
		if err := os.WriteFile(filepath.Join(dir, "images", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeManifestAtomically(t, dir, manifest)
	return dir
}

// writeManifestAtomically follows the writer contract: write a sibling temp
// file, then rename it over manifest.json so readers never see a partial file.
func writeManifestAtomically(t *testing.T, dir, manifest string) {
	t.Helper()
	tmp := filepath.Join(dir, ".manifest.json.tmp")
	if err := os.WriteFile(tmp, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, filepath.Join(dir, "manifest.json")); err != nil {
		t.Fatal(err)
	}
}

// counter counts watcher callbacks.
type counter struct {
	mu sync.Mutex
	n  int
}

func (c *counter) bump() {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

func (c *counter) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// recorder counts OnStable callbacks and remembers the last digest.
type recorder struct {
	mu   sync.Mutex
	n    int
	last string
}

func (r *recorder) record(digest string) {
	r.mu.Lock()
	r.n++
	r.last = digest
	r.mu.Unlock()
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func (r *recorder) digest() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

// waitFor polls cond until it holds or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(msg)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// setDigest swaps a coordinator's injected digest function between cycles, under
// the coordinator's lock, so -race sees a proper happens-before edge.
func setDigest(c *Coordinator, fn func(string) (string, error)) {
	c.mu.Lock()
	c.Digest = fn
	c.mu.Unlock()
}
