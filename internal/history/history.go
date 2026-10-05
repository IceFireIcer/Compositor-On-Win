// Package history implements the document's snapshot-based undo/redo history.
//
// It ports reference/Swift/Compositor/Document/DocumentHistory.swift (the
// authoritative semantics): value snapshots on two entry stacks, depth-counted
// nested transactions that merge into the outermost one, no-op commits that
// preserve the redo stack, menu names read off the stack tops, revision-UUID
// dirty tracking, and oldest-first trimming against an entry limit and a
// retained-byte budget.
package history

import (
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"reflect"

	"compositor-win/internal/domain"
)

const (
	// DefaultEntryLimit is DocumentHistory's default cap on past+future entries.
	DefaultEntryLimit = 100
	// DefaultRetainedByteLimit is the macOS build's 256 MB pixel budget.
	DefaultRetainedByteLimit int64 = 256 * 1024 * 1024
)

// RetainedEstimator estimates the pixel bytes a document uniquely retains.
//
// Go adaptation of DocumentHistory.retainedBytes: the Swift model shares
// immutable CGImages between the live document and the history snapshots and
// counts the image bytes not reachable from the live document, deduplicated
// per ObjectIdentifier. The Go domain model keeps pixels on disk as PNG
// assets (Layer.ImageFile) and holds no bitmaps in memory, so there is
// nothing structural to measure; the estimate is injected instead and
// defaults to zero, which leaves the entry limit as the only active bound.
// See History.retainedBytes for how the estimate is compared against the
// live document.
type RetainedEstimator func(domain.Document) int64

// Snapshot is the immutable state captured at one revision: the document, the
// active-layer selection and the revision UUID. Snapshots are deep copies —
// they never share slice or pointer storage with the caller's working
// document (see cloneDocument). A zero Document represents "no document".
type Snapshot struct {
	Document      domain.Document
	ActiveLayerID *string
	Revision      string
}

// entry is one undo step: what the document looked like before the named
// transaction and after it.
type entry struct {
	name   string
	before Snapshot
	after  Snapshot
}

// History is the undo/redo log of one open document. Create it with New or
// NewWithLimits; the zero value is not usable.
type History struct {
	// RetainedBytes estimates the pixel bytes a document uniquely retains.
	// nil (the default) estimates zero — the Go model keeps pixels in disk
	// PNGs, so without an estimator the entry limit is the only bound. See
	// RetainedEstimator for the Swift-to-Go adaptation notes.
	RetainedBytes RetainedEstimator

	past          []entry
	future        []entry
	revision      string
	savedRevision string
	pending       *Snapshot
	pendingName   string
	depth         int

	entryLimit        int
	retainedByteLimit int64
}

// New returns a History bounded by DefaultEntryLimit and
// DefaultRetainedByteLimit.
func New() *History {
	return NewWithLimits(DefaultEntryLimit, DefaultRetainedByteLimit)
}

// NewWithLimits returns a History with explicit bounds; negative values clamp
// to zero, mirroring the max(0, ...) in DocumentHistory.init.
func NewWithLimits(entryLimit int, retainedByteLimit int64) *History {
	if entryLimit < 0 {
		entryLimit = 0
	}
	if retainedByteLimit < 0 {
		retainedByteLimit = 0
	}
	h := &History{
		revision:          newRevision(),
		pendingName:       "Edit",
		entryLimit:        entryLimit,
		retainedByteLimit: retainedByteLimit,
	}
	h.savedRevision = h.revision
	return h
}

// CanUndo reports whether an undo is available. Undo and redo are blocked
// while a transaction is open (depth > 0), mirroring DocumentHistory.canUndo.
func (h *History) CanUndo() bool { return h.depth == 0 && len(h.past) > 0 }

// CanRedo reports whether a redo is available.
func (h *History) CanRedo() bool { return h.depth == 0 && len(h.future) > 0 }

// UndoName is the name of the entry undo would replay, or "" when the past
// stack is empty (undo/redo menu titles, DocumentHistory.undoName).
func (h *History) UndoName() string {
	if len(h.past) == 0 {
		return ""
	}
	return h.past[len(h.past)-1].name
}

// RedoName is the name of the entry redo would replay, or "" when the future
// stack is empty.
func (h *History) RedoName() string {
	if len(h.future) == 0 {
		return ""
	}
	return h.future[len(h.future)-1].name
}

// IsModified reports whether the current revision differs from the saved one.
func (h *History) IsModified() bool { return h.revision != h.savedRevision }

// UndoCount is the number of undoable entries (DocumentHistory.undoCount).
func (h *History) UndoCount() int { return len(h.past) }

// Revision is the current document revision, so a save can capture it now and
// finish later (DocumentHistory.currentRevision).
func (h *History) Revision() string { return h.revision }

// MarkSaved records the current revision as saved.
func (h *History) MarkSaved() { h.savedRevision = h.revision }

// MarkSavedRevision completes a save that captured an earlier revision:
// edits made while it was writing leave the document modified, and undoing
// back to that revision does not.
func (h *History) MarkSavedRevision(saved string) { h.savedRevision = saved }

// Reset clears all history and issues a fresh revision marked as saved.
func (h *History) Reset() {
	h.past = nil
	h.future = nil
	h.pending = nil
	h.depth = 0
	h.revision = newRevision()
	h.savedRevision = h.revision
}

// Begin opens a transaction named name. Only the outermost call captures a
// before-snapshot and records the name; nested calls just count depth and
// merge into the outer transaction, exactly like DocumentHistory.begin.
func (h *History) Begin(name string, document domain.Document, activeLayerID *string) {
	if h.depth == 0 {
		h.pending = &Snapshot{
			Document:      cloneDocument(document),
			ActiveLayerID: copyString(activeLayerID),
			Revision:      h.revision,
		}
		h.pendingName = name
	}
	h.depth++
}

// End closes the innermost open transaction. When the outermost transaction
// closes, the whole edit commits as one entry — unless the document ended
// unchanged, in which case nothing is recorded, the revision stays put and
// the redo stack is preserved (selection, navigation and no-op edits must not
// destroy redo history). Nested ends never commit and never clear redo.
func (h *History) End(document domain.Document, activeLayerID *string) {
	if h.depth <= 0 {
		return
	}
	h.depth--
	if h.depth != 0 || h.pending == nil {
		return
	}
	before := *h.pending
	h.pending = nil
	// No-op commit: no entry, no revision bump, redo stack untouched.
	if documentsEqual(before.Document, document) {
		return
	}
	h.revision = newRevision()
	h.past = append(h.past, entry{
		name:   h.pendingName,
		before: before,
		after: Snapshot{
			Document:      cloneDocument(document),
			ActiveLayerID: copyString(activeLayerID),
			Revision:      h.revision,
		},
	})
	h.future = nil
	h.trim(document)
}

// Undo moves the newest past entry onto the future stack and returns its
// before-snapshot; the current revision rewinds to it.
func (h *History) Undo() (Snapshot, bool) {
	if !h.CanUndo() {
		return Snapshot{}, false
	}
	n := len(h.past) - 1
	e := h.past[n]
	h.past[n] = entry{}
	h.past = h.past[:n]
	h.future = append(h.future, e)
	h.revision = e.before.Revision
	h.trim(e.before.Document)
	return cloneSnapshot(e.before), true
}

// Redo moves the newest future entry back onto the past stack and returns its
// after-snapshot; the current revision advances to it.
func (h *History) Redo() (Snapshot, bool) {
	if !h.CanRedo() {
		return Snapshot{}, false
	}
	n := len(h.future) - 1
	e := h.future[n]
	h.future[n] = entry{}
	h.future = h.future[:n]
	h.past = append(h.past, e)
	h.revision = e.after.Revision
	h.trim(e.after.Document)
	return cloneSnapshot(e.after), true
}

// retainedBytes estimates the bytes held only by history, excluding what the
// live document accounts for — the Go adaptation of
// DocumentHistory.retainedBytes(current:). Swift deduplicates images per
// ObjectIdentifier and skips every image the live document references. With a
// black-box per-document estimator, exact identity dedup is impossible, so
// each history snapshot contributes max(0, estimate(snapshot) −
// estimate(current)): snapshots whose assets are all shared with the live
// document contribute nothing, and snapshots retaining assets the live
// document dropped count their surplus in full. Summing per snapshot may
// overcount assets shared between history snapshots, which only makes trim
// run earlier than Swift's exact accounting — a conservative direction.
func (h *History) retainedBytes(current domain.Document) int64 {
	if h.RetainedBytes == nil {
		return 0
	}
	live := h.RetainedBytes(current)
	var total int64
	for i := range h.past {
		total += retainedOver(h.past[i], h.RetainedBytes, live)
	}
	for i := range h.future {
		total += retainedOver(h.future[i], h.RetainedBytes, live)
	}
	return total
}

// retainedOver sums one entry's per-snapshot surplus over the live estimate.
func retainedOver(e entry, estimate RetainedEstimator, live int64) int64 {
	var total int64
	if b := estimate(e.before.Document) - live; b > 0 {
		total += b
	}
	if a := estimate(e.after.Document) - live; a > 0 {
		total += a
	}
	return total
}

// trim evicts oldest-first while over the entry or byte budget, mirroring
// DocumentHistory.trim: past drains before future, and evicting from the
// front of future keeps the remaining redo chain replayable in order.
func (h *History) trim(current domain.Document) {
	for len(h.past)+len(h.future) > h.entryLimit || h.retainedBytes(current) > h.retainedByteLimit {
		if len(h.past) > 0 {
			h.past = dropFirst(h.past)
		} else if len(h.future) > 0 {
			h.future = dropFirst(h.future)
		} else {
			break
		}
	}
}

// dropFirst removes the oldest entry and zeroes the vacated tail slot so the
// evicted documents stop being referenced by the backing array.
func dropFirst(entries []entry) []entry {
	n := copy(entries, entries[1:])
	entries[n] = entry{}
	return entries[:n]
}

// cloneDocument deep-copies a document so history snapshots stay immune to
// later mutation of the caller's working copy. Swift gets immutable value
// snapshots for free from CanvasDocument's copy-on-write value semantics; the
// Go manifest model shares slice and pointer storage across struct copies, so
// every snapshot boundary (begin, end, undo, redo) makes a defensive copy.
// The domain model is pure manifest data — every field is JSON-tagged and
// encoding/json round-trips float64 bit-exactly and json.RawMessage verbatim —
// so a JSON round-trip is a faithful deep clone that automatically covers
// fields added to the domain later.
func cloneDocument(d domain.Document) domain.Document {
	b, err := json.Marshal(d)
	if err != nil {
		// Unreachable for the JSON-only manifest model; keep the value
		// rather than panicking inside an undo path.
		return d
	}
	var out domain.Document
	if err := json.Unmarshal(b, &out); err != nil {
		return d
	}
	return out
}

// cloneSnapshot copies a snapshot handed to the caller so returned documents
// share no storage with history-internal entries.
func cloneSnapshot(s Snapshot) Snapshot {
	return Snapshot{
		Document:      cloneDocument(s.Document),
		ActiveLayerID: copyString(s.ActiveLayerID),
		Revision:      s.Revision,
	}
}

// copyString detaches a selection pointer so snapshots own their own string.
func copyString(p *string) *string {
	if p == nil {
		return nil
	}
	s := *p
	return &s
}

// documentsEqual reports value equality of two documents, mirroring the
// CanvasDocument != comparison that gates no-op commits in
// DocumentHistory.end. reflect.DeepEqual treats nil and empty slices as
// different, so the top-level layer and guide lists are normalized first: an
// edit that only changes nil to empty (or an empty guide list to no guides)
// is still a no-op. Deeper nil-vs-empty slice pairs inside adjustment or
// effect payloads would still compare unequal — such payloads are only
// produced by the manifest reader, which keeps one canonical shape.
func documentsEqual(a, b domain.Document) bool {
	return reflect.DeepEqual(normalizeDocument(a), normalizeDocument(b))
}

// normalizeDocument canonicalizes nil/empty shape of the two top-level lists.
func normalizeDocument(d domain.Document) domain.Document {
	if d.Layers == nil {
		d.Layers = []domain.Layer{}
	}
	if d.GuidesList != nil && len(*d.GuidesList) == 0 {
		d.GuidesList = nil
	}
	return d
}

// newRevision returns a random RFC 4122 version-4 UUID string, the stand-in
// for Swift's UUID(); the history package is pinned to the standard library,
// so there is no third-party uuid dependency here.
func newRevision() string {
	var b [16]byte
	if _, err := crand.Read(b[:]); err != nil {
		// crypto/rand never fails on supported platforms; duplicating a
		// revision would silently corrupt dirty tracking, so fail loudly.
		panic(fmt.Sprintf("history: crypto/rand unavailable: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
