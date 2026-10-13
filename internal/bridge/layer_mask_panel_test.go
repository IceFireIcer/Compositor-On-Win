package bridge

import (
	"reflect"
	"testing"
)

func TestPlaceLayerNestsExistingFolderAndMovesSubtreeWithUndo(t *testing.T) {
	f := newFixture(t, 2, 2)
	base := f.pixel("base")
	folder := f.group("folder")
	child := f.pixel("child")
	f.under(t, child, folder)
	other := f.group("other")
	f.active(child)
	svc := f.service(t)
	tab := f.doc.DocumentID
	before := rawDocJSON(t, svc)
	out, err := svc.PlaceLayer(tab, folder, other, "inside")
	if err != nil {
		t.Fatal(err)
	}
	doc := parseEnvelope(t, out).Doc
	if len(doc.Layers) != 4 || parentIDOf(layerAt(doc, folder)) != other || parentIDOf(layerAt(doc, child)) != folder {
		t.Fatalf("nest created a wrapper or lost descendants: %+v", doc.Layers)
	}
	if !reflect.DeepEqual(panelOrder(doc), []string{other, folder, child, base}) {
		t.Fatalf("panel order = %v", panelOrder(doc))
	}
	if *doc.ActiveLayerID != child {
		t.Fatal("placement changed edit target")
	}
	if _, err = svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if rawDocJSON(t, svc) != before {
		t.Fatal("one undo did not restore entire subtree")
	}
	if _, err = svc.Redo(); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.PlaceLayer(tab, folder, base, "before"); err != nil {
		t.Fatal(err)
	}
	doc = mustDocumentSnapshot(t, svc).Doc
	if parentIDOf(layerAt(doc, folder)) != "" || parentIDOf(layerAt(doc, child)) != folder {
		t.Fatal("move out lost hierarchy")
	}
	if !reflect.DeepEqual(panelOrder(doc), []string{other, folder, child, base}) {
		t.Fatalf("move out order = %v", panelOrder(doc))
	}
}

func TestPlaceLayerReordersPixelsAndRepairsClipping(t *testing.T) {
	f := newFixture(t, 1, 1)
	red, blue := f.pixel("red"), f.pixel("blue")
	f.fill(t, red, 0, 0, 1, 1, 255, 0, 0)
	f.fill(t, blue, 0, 0, 1, 1, 0, 0, 255)
	f.active(blue)
	svc := f.service(t)
	if got := renderedPixelAt(t, svc, f.doc.DocumentID, 0, 0); got != [4]uint8{0, 0, 255, 255} {
		t.Fatal(got)
	}
	if _, err := svc.PlaceLayer(f.doc.DocumentID, red, blue, "before"); err != nil {
		t.Fatal(err)
	}
	if got := renderedPixelAt(t, svc, f.doc.DocumentID, 0, 0); got != [4]uint8{255, 0, 0, 255} {
		t.Fatal(got)
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := renderedPixelAt(t, svc, f.doc.DocumentID, 0, 0); got != [4]uint8{0, 0, 255, 255} {
		t.Fatal(got)
	}
	if _, err := svc.LayerOp("setClippingMask", payloadJSON(t, map[string]any{"id": blue, "clip": true})); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PlaceLayer(f.doc.DocumentID, blue, red, "after"); err != nil {
		t.Fatal(err)
	}
	if layerAt(mustDocumentSnapshot(t, svc).Doc, blue).MaskSourceID != nil {
		t.Fatal("detached clipping not released")
	}
}

func TestPlaceLayerRejectsCycleBadTargetBusyWithoutMutationAndNoOpKeepsRedo(t *testing.T) {
	f := newFixture(t, 1, 1)
	base, folder, child := f.pixel("base"), f.group("folder"), f.group("child")
	f.under(t, child, folder)
	f.active(base)
	svc := f.service(t)
	tab := f.doc.DocumentID
	for _, args := range [][4]string{{tab, folder, child, "inside"}, {tab, base, folder, "bad"}, {tab, folder, base, "inside"}, {"gone", base, folder, "before"}} {
		before, _ := svc.DocumentSnapshot()
		if _, err := svc.PlaceLayer(args[0], args[1], args[2], args[3]); err == nil {
			t.Fatalf("accepted %v", args)
		}
		after, _ := svc.DocumentSnapshot()
		if before != after {
			t.Fatal("rejected drop changed snapshot")
		}
	}
	if _, err := svc.PlaceLayer(tab, base, folder, "before"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	before, _ := svc.DocumentSnapshot()
	if _, err := svc.PlaceLayer(tab, base, folder, "after"); err != nil {
		t.Fatal(err)
	}
	after, _ := svc.DocumentSnapshot()
	if before != after {
		t.Fatal("no-op changed revision")
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	if panelOrder(mustDocumentSnapshot(t, svc).Doc)[0] != base {
		t.Fatal("no-op discarded redo")
	}
	svc.ws.sessions[tab].saving = true
	if _, err := svc.PlaceLayer(tab, base, folder, "after"); err != errDocumentBusy {
		t.Fatalf("busy error = %v", err)
	}
}
