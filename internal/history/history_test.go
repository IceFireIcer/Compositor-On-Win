package history

import (
	"fmt"
	"reflect"
	"testing"

	"compositor-win/internal/domain"
)

// newDoc builds a minimal valid manifest model for tests.
func newDoc(w, h int) domain.Document {
	return domain.Document{
		Format:     domain.FormatID,
		Version:    domain.FormatVersion,
		ColorSpace: domain.ColorSpaceSRGB,
		DocumentID: "test-doc",
		Width:      w,
		Height:     h,
		Layers:     []domain.Layer{},
	}
}

// rasterLayer builds a layer; an empty file means a blank layer without an
// asset (the Go stand-in for Swift's blank ImageLayer).
func rasterLayer(id, name, file string) domain.Layer {
	l := domain.Layer{ID: id, Name: name, IsVisible: true}
	if file != "" {
		l.ImageFile = strPtr(file)
	}
	return l
}

func strPtr(s string) *string { return &s }

func sameSelection(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// TestEveryLayerEditRoundTripsWithSelection ports HistoryTests'
// everyLayerEditRoundTripsWithSelection: every committed edit round-trips
// through undo/redo together with the active-layer selection.
func TestEveryLayerEditRoundTripsWithSelection(t *testing.T) {
	h := New()
	doc := domain.Document{} // zero Document means "no document open"
	var sel *string
	type state struct {
		doc domain.Document
		sel *string
	}
	states := []state{{doc: doc, sel: nil}}
	commit := func(name string, mutate func(d *domain.Document)) {
		h.Begin(name, doc, sel)
		mutate(&doc)
		h.End(doc, sel)
		// Defensive copies: the test's own states must not alias doc.
		states = append(states, state{doc: cloneDocument(doc), sel: copyString(sel)})
	}
	commit("New Document", func(d *domain.Document) { *d = newDoc(800, 600) })
	commit("Add Layer", func(d *domain.Document) {
		d.Layers = append(d.Layers, rasterLayer("layer-1", "Layer 1", ""))
		sel = strPtr("layer-1")
	})
	commit("Add Layer", func(d *domain.Document) {
		d.Layers = append(d.Layers, rasterLayer("layer-2", "Layer 2", ""))
		sel = strPtr("layer-2")
	})
	commit("Rename Layer", func(d *domain.Document) { d.Layers[1].Name = "Foreground" })
	commit("Toggle Visibility", func(d *domain.Document) { d.Layers[1].IsVisible = !d.Layers[1].IsVisible })
	commit("Reorder Layers", func(d *domain.Document) {
		first := d.Layers[0]
		d.Layers = append(d.Layers[1:], first)
	})
	commit("Move Layer", func(d *domain.Document) { d.Layers[0], d.Layers[1] = d.Layers[1], d.Layers[0] })
	commit("Delete Layer", func(d *domain.Document) {
		d.Layers = d.Layers[:1]
		sel = nil
	})

	sameState := func(got Snapshot, want state, label string) {
		t.Helper()
		if !documentsEqual(got.Document, want.doc) {
			t.Fatalf("%s: document mismatch\n got: %+v\nwant: %+v", label, got.Document, want.doc)
		}
		if !sameSelection(got.ActiveLayerID, want.sel) {
			t.Fatalf("%s: selection mismatch, got %v want %v", label, got.ActiveLayerID, want.sel)
		}
	}
	for i := len(states) - 2; i >= 0; i-- {
		if !h.CanUndo() {
			t.Fatalf("undo to step %d: CanUndo = false, want true", i)
		}
		snap, ok := h.Undo()
		if !ok {
			t.Fatalf("undo to step %d: Undo failed", i)
		}
		sameState(snap, states[i], fmt.Sprintf("undo to step %d", i))
	}
	if h.CanUndo() {
		t.Fatal("after full undo CanUndo = true, want false")
	}
	if h.UndoName() != "" {
		t.Fatalf("UndoName on an empty past = %q, want empty", h.UndoName())
	}
	for i := 1; i < len(states); i++ {
		if !h.CanRedo() {
			t.Fatalf("redo to step %d: CanRedo = false, want true", i)
		}
		snap, ok := h.Redo()
		if !ok {
			t.Fatalf("redo to step %d: Redo failed", i)
		}
		sameState(snap, states[i], fmt.Sprintf("redo to step %d", i))
	}
	if h.CanRedo() {
		t.Fatal("after full redo CanRedo = true, want false")
	}
	if h.RedoName() != "" {
		t.Fatalf("RedoName on an empty future = %q, want empty", h.RedoName())
	}
}

// TestNoOpEditsPreserveRedoAndDirtyFlag ports HistoryTests'
// navigationNoOpsAndSaveRevisionPreserveHistory (the viewport-zoom assertion
// is session state outside this package): no-op commits leave both stacks and
// the revision untouched, and undo/redo track the saved revision.
func TestNoOpEditsPreserveRedoAndDirtyFlag(t *testing.T) {
	h := New()
	h.Begin("New Document", domain.Document{}, nil)
	doc := newDoc(800, 600)
	doc.Layers = append(doc.Layers, rasterLayer("layer-1", "Layer 1", ""))
	sel := strPtr("layer-1")
	h.End(doc, sel)
	h.MarkSaved()
	if h.IsModified() {
		t.Fatal("freshly saved history must not be modified")
	}

	revBefore := h.Revision()
	h.Begin("Rename Layer", doc, sel)
	doc.Layers[0].Name = "Changed"
	h.End(doc, sel)
	if !h.IsModified() {
		t.Fatal("committed edit must mark the document modified")
	}
	if h.Revision() == revBefore {
		t.Fatal("committed edit must issue a new revision")
	}

	snap, ok := h.Undo()
	if !ok {
		t.Fatal("Undo failed")
	}
	if h.IsModified() {
		t.Fatal("undoing back to the saved revision must clear isModified")
	}
	if snap.Revision != h.Revision() {
		t.Fatalf("restored revision %q != current %q", snap.Revision, h.Revision())
	}
	count := h.UndoCount()

	// No-op commits: rename to the current name, a rejected whitespace-only
	// rename, and a single-layer reorder all end with an unchanged document.
	noOp := func(name string) {
		h.Begin(name, doc, sel)
		h.End(doc, sel)
	}
	noOp("Rename Layer")
	noOp("Rename Layer")
	noOp("Reorder Layers")
	if h.UndoCount() != count {
		t.Fatalf("no-op commits added entries: %d != %d", h.UndoCount(), count)
	}
	if h.Revision() != snap.Revision {
		t.Fatal("no-op commit changed the revision")
	}
	if !h.CanRedo() {
		t.Fatal("no-op commits must preserve the redo stack")
	}
	if h.RedoName() != "Rename Layer" {
		t.Fatalf("RedoName = %q, want %q", h.RedoName(), "Rename Layer")
	}

	redoSnap, ok := h.Redo()
	if !ok {
		t.Fatal("Redo failed")
	}
	if redoSnap.Document.Layers[0].Name != "Changed" {
		t.Fatalf("redo restored name %q, want %q", redoSnap.Document.Layers[0].Name, "Changed")
	}
	if !h.IsModified() {
		t.Fatal("redo must mark the document modified again")
	}

	if _, ok := h.Undo(); !ok {
		t.Fatal("second Undo failed")
	}
	h.Begin("Add Layer", doc, sel)
	doc.Layers = append(doc.Layers, rasterLayer("layer-2", "Layer 2", ""))
	h.End(doc, sel)
	if h.CanRedo() {
		t.Fatal("a committed edit must clear the redo stack")
	}
	if !h.IsModified() {
		t.Fatal("new edit must mark the document modified")
	}
}

// TestNestedTransactionsMergeAsOne ports HistoryTests'
// replacementCanvasAndNestedTransactionsUndoAsOne: nested transactions merge
// into the outermost one, keep its name, and commit as a single entry; a
// replacement canvas undoes and redoes as one step too.
func TestNestedTransactionsMergeAsOne(t *testing.T) {
	h := New()
	h.Begin("New Document", domain.Document{}, nil)
	doc := newDoc(100, 200)
	h.End(doc, nil)
	base := h.UndoCount() // the canvas creation itself is one entry

	revBefore := h.Revision()
	h.Begin("Layer Setup", doc, nil) // depth 1: captures the before-snapshot
	h.Begin("Add Layer", doc, nil)   // depth 2: inner, no capture
	doc.Layers = append(doc.Layers, rasterLayer("layer-1", "Layer 1", ""))
	h.End(doc, nil) // depth 1: merges into the outer transaction
	h.Begin("Add Layer", doc, nil)
	doc.Layers = append(doc.Layers, rasterLayer("layer-2", "Layer 2", ""))
	h.End(doc, nil) // depth 1
	if h.CanUndo() {
		t.Fatal("undo must be blocked while a transaction is open")
	}
	if h.UndoCount() != base {
		t.Fatalf("inner commits must not create entries, got %d, want %d", h.UndoCount(), base)
	}
	if h.Revision() != revBefore {
		t.Fatal("inner commits must not bump the revision")
	}
	h.End(doc, nil) // depth 0: the outermost transaction commits once
	if h.UndoCount() != base+1 {
		t.Fatalf("nested transactions must collapse to one entry, got %d, want %d", h.UndoCount(), base+1)
	}
	if h.UndoName() != "Layer Setup" {
		t.Fatalf("UndoName = %q, want the outermost name %q", h.UndoName(), "Layer Setup")
	}

	snap, ok := h.Undo()
	if !ok {
		t.Fatal("Undo failed")
	}
	if len(snap.Document.Layers) != 0 {
		t.Fatalf("undo must reach the created document with no layers, got %d", len(snap.Document.Layers))
	}
	if h.UndoName() != "New Document" {
		t.Fatalf("UndoName = %q, want the remaining outer entry %q", h.UndoName(), "New Document")
	}
	redoSnap, ok := h.Redo()
	if !ok {
		t.Fatal("Redo failed")
	}
	if len(redoSnap.Document.Layers) != 2 {
		t.Fatalf("redo must restore both layers, got %d", len(redoSnap.Document.Layers))
	}

	previous := cloneDocument(doc)
	h.Begin("New Document", doc, nil)
	doc = newDoc(300, 400)
	h.End(doc, nil)
	snap, ok = h.Undo()
	if !ok || !documentsEqual(snap.Document, previous) {
		t.Fatal("undo must restore the replaced document")
	}
	redoSnap, ok = h.Redo()
	if !ok || redoSnap.Document.Width != 300 || redoSnap.Document.Height != 400 {
		t.Fatalf("redo must restore the replacement canvas 300x400, got %+v", redoSnap.Document)
	}
}

// TestNoOpOuterCommitKeepsRedoAfterInnerChange pins the end-of-transaction
// subtleties around DocumentHistory.end: a nested end never clears the redo
// stack, and an outer commit whose document ended unchanged is a no-op that
// keeps redo history.
func TestNoOpOuterCommitKeepsRedoAfterInnerChange(t *testing.T) {
	h := New()
	doc0 := newDoc(100, 100)
	docA := cloneDocument(doc0)
	docA.Layers = append(docA.Layers, rasterLayer("layer-1", "Layer 1", ""))
	h.Begin("Add Layer", doc0, nil)
	h.End(docA, nil)
	if _, ok := h.Undo(); !ok {
		t.Fatal("Undo failed")
	}

	h.Begin("Outer", doc0, nil) // depth 1
	h.Begin("Inner", doc0, nil) // depth 2
	h.End(docA, nil)            // depth 1: inner change, redo stack must survive
	h.End(doc0, nil)            // depth 0: outer document ended unchanged — no-op
	if h.UndoCount() != 0 {
		t.Fatalf("reverted edit is a no-op, got %d entries", h.UndoCount())
	}
	if !h.CanRedo() {
		t.Fatal("no-op outer commit must preserve the redo stack")
	}
	snap, ok := h.Redo()
	if !ok || !documentsEqual(snap.Document, docA) {
		t.Fatal("redo must restore the state from before the no-op transaction")
	}
}

// TestUndoBlockedDuringTransactionAndUnbalancedCalls ports the portable part
// of HistoryTests' historyBlockedDuringImportsAndDialogs: undo/redo are gated
// while a transaction is open, and unbalanced End calls are inert.
func TestUndoBlockedDuringTransactionAndUnbalancedCalls(t *testing.T) {
	h := New()
	h.Begin("New Document", domain.Document{}, nil)
	doc := newDoc(10, 10)
	h.End(doc, nil)
	h.Begin("Add Layer", doc, nil)
	doc.Layers = append(doc.Layers, rasterLayer("layer-1", "Layer 1", ""))
	h.End(doc, nil)
	if !h.CanUndo() {
		t.Fatal("committed entries must be undoable")
	}

	rev := h.Revision()
	base := h.UndoCount()
	h.Begin("Batch", doc, nil)
	if h.CanUndo() {
		t.Fatal("undo must be blocked while a transaction is open")
	}
	if _, ok := h.Undo(); ok {
		t.Fatal("Undo during a transaction must fail")
	}
	if h.UndoCount() != base || h.Revision() != rev {
		t.Fatal("failed Undo during a transaction must not touch state")
	}
	h.Begin("Nested", doc, nil)
	doc.Layers[0].Name = "Changed"
	h.End(doc, nil) // inner end merges into "Batch"
	h.End(doc, nil) // outer end commits one entry
	if !h.CanUndo() || h.UndoName() != "Batch" {
		t.Fatalf("after commit CanUndo/UndoName = %v/%q, want true/Batch", h.CanUndo(), h.UndoName())
	}

	changed := cloneDocument(doc)
	changed.Layers = append(changed.Layers, rasterLayer("layer-2", "Layer 2", ""))
	h.End(changed, nil) // unbalanced end must be ignored
	if h.UndoCount() != base+1 {
		t.Fatalf("unbalanced End added an entry: %d", h.UndoCount())
	}
	if h.RedoName() != "" {
		t.Fatalf("unbalanced End must not touch history, RedoName = %q", h.RedoName())
	}

	fresh := New()
	fresh.End(doc, nil) // End without Begin must be inert
	if fresh.UndoCount() != 0 || fresh.CanUndo() || fresh.CanRedo() {
		t.Fatal("End without Begin must not change state")
	}
	fresh.Begin("Dangling", doc, nil)
	fresh.Reset()
	fresh.End(changed, nil) // end after reset must be inert too
	if fresh.UndoCount() != 0 || fresh.IsModified() || fresh.depth != 0 || fresh.pending != nil {
		t.Fatal("reset must clear pending transaction state")
	}
}

// TestBatchImportIsOneEntryAndFailuresAddNothing ports HistoryTests'
// batchImportIsOneEntryAndFailuresDoNotAddHistory (minus the image decoder):
// a many-layer import commits as one entry, a failed import adds nothing, and
// asset references survive the undo/redo round-trip.
func TestBatchImportIsOneEntryAndFailuresAddNothing(t *testing.T) {
	h := New()
	h.Begin("Import Images", domain.Document{}, nil)
	doc := newDoc(640, 480)
	doc.Layers = append(doc.Layers,
		rasterLayer("layer-1", "a", "a.png"),
		rasterLayer("layer-2", "b", "b.png"))
	sel := strPtr("layer-1")
	h.End(doc, sel)
	if h.UndoCount() != 1 {
		t.Fatalf("batch import must be one entry, got %d", h.UndoCount())
	}
	imported := cloneDocument(doc)

	rev := h.Revision()
	h.Begin("Import Images", doc, sel)
	h.End(doc, sel) // failed import: document unchanged
	if h.UndoCount() != 1 {
		t.Fatal("failed import must not add history")
	}
	if h.Revision() != rev {
		t.Fatal("failed import must not change the revision")
	}

	snap, ok := h.Undo()
	if !ok || !documentsEqual(snap.Document, domain.Document{}) {
		t.Fatal("undo past the import must reach the empty state")
	}
	redoSnap, ok := h.Redo()
	if !ok || !reflect.DeepEqual(redoSnap.Document, imported) {
		t.Fatal("redo must restore the imported document")
	}
	if !sameSelection(redoSnap.ActiveLayerID, sel) {
		t.Fatalf("redo must restore the selection, got %v", redoSnap.ActiveLayerID)
	}
	if redoSnap.Document.Layers[0].ImageFile == nil || *redoSnap.Document.Layers[0].ImageFile != "a.png" {
		t.Fatal("asset reference lost across undo/redo")
	}
}

// TestQueuedImportsHaveSeparateUndoEntries ports HistoryTests'
// queuedImportsHaveSeparateUndoEntries: sequential imports get one entry each.
func TestQueuedImportsHaveSeparateUndoEntries(t *testing.T) {
	h := New()
	doc := domain.Document{}
	for _, file := range []string{"first.png", "second.png"} {
		h.Begin("Import Images", doc, nil)
		if doc.Format == "" {
			doc = newDoc(64, 64)
		}
		doc.Layers = append(doc.Layers, rasterLayer(file, file, file))
		h.End(doc, nil)
	}
	if h.UndoCount() != 2 {
		t.Fatalf("queued imports need separate entries, got %d", h.UndoCount())
	}
	snap, ok := h.Undo()
	if !ok || len(snap.Document.Layers) != 1 {
		t.Fatalf("first undo must leave one layer, got %d", len(snap.Document.Layers))
	}
	snap, ok = h.Undo()
	if !ok || len(snap.Document.Layers) != 0 {
		t.Fatalf("second undo must reach the empty state, got %d layers", len(snap.Document.Layers))
	}
	if h.CanUndo() {
		t.Fatal("past stack must be empty")
	}
}

// TestHistoryBoundsEntriesAndUniqueRetainedPixels ports HistoryTests'
// historyBoundsEntriesAndUniqueRetainedPixels. Go adaptation: pixels are PNG
// assets on disk, so the injected estimator counts bytes per asset reference
// instead of walking CGImage buffers; snapshots sharing the live document's
// assets must contribute zero retained bytes, matching Swift's
// ObjectIdentifier-based exclusion of the live document.
func TestHistoryBoundsEntriesAndUniqueRetainedPixels(t *testing.T) {
	estimate := func(d domain.Document) int64 {
		var n int64
		for _, l := range d.Layers {
			if l.ImageFile != nil {
				n += 4096
			}
		}
		return n
	}
	h := NewWithLimits(2, 0)
	h.RetainedBytes = estimate
	doc := newDoc(64, 32)
	doc.Layers = append(doc.Layers, rasterLayer("layer-1", "Layer 1", "asset.png"))
	sel := strPtr("layer-1")
	for _, name := range []string{"A", "B", "C"} {
		h.Begin("Rename", doc, sel)
		doc.Layers[0].Name = name
		h.End(doc, sel)
	}
	if h.UndoCount() != 2 {
		t.Fatalf("entry limit must evict the oldest entries, got %d", h.UndoCount())
	}
	// All three transactions share the name "Rename"; undo replays the
	// before-states B then A. The evicted oldest entry (T_A) is observable by
	// what became unreachable: the initial "Layer 1" state.
	snap, ok := h.Undo()
	if !ok || snap.Document.Layers[0].Name != "B" {
		t.Fatalf("first undo must reach the state before the last edit, got %+v ok=%v", snap, ok)
	}
	snap, ok = h.Undo()
	if !ok || snap.Document.Layers[0].Name != "A" {
		t.Fatalf("second undo must reach the state before the middle edit, got %+v ok=%v", snap, ok)
	}
	if h.CanUndo() {
		t.Fatal("the oldest entry (A) must have been evicted")
	}
	if h.RedoName() != "Rename" {
		t.Fatalf("RedoName = %q, want %q", h.RedoName(), "Rename")
	}
	if _, ok := h.Redo(); !ok { // walk back to the newest state
		t.Fatal("Redo failed")
	}
	if _, ok := h.Redo(); !ok {
		t.Fatal("Redo failed")
	}
	if got := h.retainedBytes(doc); got != 0 {
		t.Fatalf("snapshots sharing the live asset must retain 0 bytes, got %d", got)
	}

	h.Begin("Delete", doc, sel)
	doc.Layers = nil
	h.End(doc, nil)
	if h.UndoCount() != 0 {
		t.Fatalf("byte overrun must evict every entry, got %d", h.UndoCount())
	}
	if got := h.retainedBytes(doc); got != 0 {
		t.Fatalf("empty history must retain 0 bytes, got %d", got)
	}
	if h.CanUndo() {
		t.Fatal("past stack must be empty after byte-driven eviction")
	}
}

// TestByteBudgetEvictionKeepsRedoChainReplayable pins the trim order that the
// ticket's "eviction must not break the redo chain" criterion refers to: byte
// eviction drains past first (Swift's trim order), then the oldest redo
// entries, leaving the surviving redo chain replayable in chronological order.
func TestByteBudgetEvictionKeepsRedoChainReplayable(t *testing.T) {
	estimate := func(d domain.Document) int64 {
		var n int64
		for _, l := range d.Layers {
			if l.ImageFile != nil {
				n += 1000
			}
		}
		return n
	}
	h := NewWithLimits(10, 1000)
	h.RetainedBytes = estimate
	buildDoc := func(prev domain.Document, file string) domain.Document {
		d := cloneDocument(prev)
		d.Layers = append(d.Layers, rasterLayer(file, file, file))
		return d
	}
	doc0 := domain.Document{}
	docA := buildDoc(doc0, "a.png")
	docB := buildDoc(docA, "b.png")
	docC := buildDoc(docB, "c.png")
	for _, step := range []struct {
		name          string
		before, after domain.Document
	}{{"A", doc0, docA}, {"B", docA, docB}, {"C", docB, docC}} {
		h.Begin(step.name, step.before, nil)
		h.End(step.after, nil)
	}

	// First undo: the redo tip alone sits exactly at the budget (strict >),
	// so nothing is evicted yet.
	if _, ok := h.Undo(); !ok {
		t.Fatal("first Undo failed")
	}
	if h.RedoName() != "C" {
		t.Fatalf("RedoName = %q, want %q", h.RedoName(), "C")
	}
	// Second undo: history-only bytes exceed the budget; eviction drains past
	// and then the oldest redo entry ("C"), keeping "B" replayable.
	if _, ok := h.Undo(); !ok {
		t.Fatal("second Undo failed")
	}
	if h.CanUndo() {
		t.Fatal("byte eviction must drain the past stack first")
	}
	if h.RedoName() != "B" {
		t.Fatalf("oldest redo entry must be evicted first, RedoName = %q", h.RedoName())
	}
	snap, ok := h.Redo()
	if !ok {
		t.Fatal("surviving redo entry must stay replayable")
	}
	if !reflect.DeepEqual(snap.Document, docB) {
		t.Fatal("redo must restore the surviving chain's state")
	}
	if h.CanRedo() {
		t.Fatal("redo chain must end after the surviving entry")
	}
}

// TestRevisionComparabilityAndDeferredSave ports the markSaved(_:) semantics:
// a save that captured an earlier revision leaves later edits modified, and
// undoing back to the saved revision does not.
func TestRevisionComparabilityAndDeferredSave(t *testing.T) {
	h := New()
	if h.Revision() == "" {
		t.Fatal("constructor must issue a revision")
	}
	if h.IsModified() {
		t.Fatal("fresh history must not be modified")
	}
	h.Begin("New Document", domain.Document{}, nil)
	doc := newDoc(10, 10)
	h.End(doc, nil)
	h.MarkSaved()
	if h.IsModified() {
		t.Fatal("MarkSaved must clear isModified")
	}

	preEdit := h.Revision()
	h.Begin("Add Layer", doc, nil)
	doc.Layers = append(doc.Layers, rasterLayer("layer-1", "Layer 1", ""))
	h.End(doc, nil)
	if !h.IsModified() || h.Revision() == preEdit {
		t.Fatal("commit must issue a new revision and mark the document modified")
	}

	h.MarkSavedRevision(preEdit)
	if !h.IsModified() {
		t.Fatal("edits newer than the deferred saved revision stay modified")
	}

	snap, ok := h.Undo()
	if !ok {
		t.Fatal("Undo failed")
	}
	if h.IsModified() {
		t.Fatal("undoing back to the saved revision clears isModified")
	}
	if snap.Revision != preEdit || h.Revision() != preEdit {
		t.Fatalf("restored revision = %q/%q, want %q", snap.Revision, h.Revision(), preEdit)
	}

	if _, ok := h.Redo(); !ok {
		t.Fatal("Redo failed")
	}
	if !h.IsModified() {
		t.Fatal("redo past the saved revision is modified")
	}

	h.Reset()
	if h.IsModified() || h.UndoCount() != 0 || h.CanRedo() {
		t.Fatal("reset must clear history and mark the fresh revision saved")
	}
}

// TestDefaultLimitsAndClamping pins the constructor defaults (100 entries /
// 256 MB), the nil-estimator default (byte accounting inert), and the
// max(0, ...) clamping of negative limits.
func TestDefaultLimitsAndClamping(t *testing.T) {
	h := New()
	if h.entryLimit != DefaultEntryLimit || h.retainedByteLimit != DefaultRetainedByteLimit {
		t.Fatalf("defaults = %d/%d, want %d/%d", h.entryLimit, h.retainedByteLimit, DefaultEntryLimit, DefaultRetainedByteLimit)
	}
	if h.RetainedBytes != nil {
		t.Fatal("default estimator must be unset (zero retained bytes)")
	}
	for i := 0; i < 3; i++ {
		h.Begin("Edit", domain.Document{}, nil)
		h.End(newDoc(1, 1), nil)
	}
	if h.UndoCount() != 3 {
		t.Fatalf("entries under the limit must be kept, got %d", h.UndoCount())
	}

	tight := NewWithLimits(-1, -5)
	if tight.entryLimit != 0 || tight.retainedByteLimit != 0 {
		t.Fatalf("negative limits must clamp to zero, got %d/%d", tight.entryLimit, tight.retainedByteLimit)
	}
	tight.Begin("Edit", domain.Document{}, nil)
	tight.End(newDoc(1, 1), nil)
	if tight.UndoCount() != 0 {
		t.Fatalf("zero entry limit must evict immediately, got %d", tight.UndoCount())
	}
}
