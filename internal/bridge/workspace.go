package bridge

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"compositor-win/internal/domain"
)

// Document is the scaffold-stage document descriptor: just enough for the
// shell (tabs, checkerboard, dirty marker). Ticket 04 replaces it with the
// full model; the ID/Name/Dirty semantics carry over.
type Document struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Resolution int    `json:"resolution"`
	Dirty      bool   `json:"dirty"`
}

// Snapshot is the full workspace state handed to the frontend after every
// mutation — commands return state instead of diffing, so the store stays a
// dumb mirror.
type Snapshot struct {
	Tabs     []Document `json:"tabs"`
	ActiveID string     `json:"activeId"`
}

// Workspace holds the open document tabs (single window, in-app tabs like
// the macOS ProjectWorkspace).
type Workspace struct {
	mu      sync.Mutex
	tabs    []Document
	active  string
	counter int
}

// NewWorkspace starts with no documents.
func NewWorkspace() *Workspace {
	return &Workspace{}
}

func newDocID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return "doc-" + hex.EncodeToString(b[:])
}

// NewDocument validates against the document limits, appends a tab and
// activates it. Name is 未命名, then 未命名 2, 未命名 3, …
func (w *Workspace) NewDocument(width, height, resolution int) (Snapshot, error) {
	if err := domain.ValidateNewDocument(width, height, resolution); err != nil {
		return Snapshot{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.counter++
	name := "未命名"
	if w.counter > 1 {
		name = fmt.Sprintf("未命名 %d", w.counter)
	}
	doc := Document{
		ID:         newDocID(),
		Name:       name,
		Width:      width,
		Height:     height,
		Resolution: resolution,
		Dirty:      true,
	}
	w.tabs = append(w.tabs, doc)
	w.active = doc.ID
	return w.snapshotLocked(), nil
}

// SelectTab activates an existing tab.
func (w *Workspace) SelectTab(id string) (Snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.hasLocked(id) {
		return Snapshot{}, fmt.Errorf("标签不存在: %s", id)
	}
	w.active = id
	return w.snapshotLocked(), nil
}

// CloseTab removes a tab. Closing the active tab activates its next
// neighbor (or the previous one at the end); closing the last tab leaves
// the workspace empty and returns to the welcome screen.
func (w *Workspace) CloseTab(id string) (Snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	index := -1
	for i, d := range w.tabs {
		if d.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return Snapshot{}, fmt.Errorf("标签不存在: %s", id)
	}
	w.tabs = append(w.tabs[:index], w.tabs[index+1:]...)
	switch {
	case w.active != id:
		// keep current selection
	case len(w.tabs) == 0:
		w.active = ""
	case index < len(w.tabs):
		w.active = w.tabs[index].ID
	default:
		w.active = w.tabs[len(w.tabs)-1].ID
	}
	return w.snapshotLocked(), nil
}

// Snapshot returns the current state (safe copy).
func (w *Workspace) Snapshot() Snapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.snapshotLocked()
}

func (w *Workspace) hasLocked(id string) bool {
	for _, d := range w.tabs {
		if d.ID == id {
			return true
		}
	}
	return false
}

func (w *Workspace) snapshotLocked() Snapshot {
	tabs := make([]Document, len(w.tabs))
	copy(tabs, w.tabs)
	return Snapshot{Tabs: tabs, ActiveID: w.active}
}
