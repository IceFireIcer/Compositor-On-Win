package bridge

import (
	"encoding/json"
	"testing"
)

func TestWandPicksTransformedLayerAndRejectsOutsideClick(t *testing.T) {
	svc, _ := newTestService(t, 8, 4)
	sess := activeSessionOf(t, svc)
	bmp := firstLayerBitmap(t, sess)
	paintRect(sess, 0, 0, 4, 4, 0)
	sess.doc.Layers[0].Transform.Origin[0] = 2
	raw, err := svc.SelectWandAt(sess.doc.DocumentID, 3, 1, 0, 0, true, false)
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Selection filterSelection `json:"selection"`
	}
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Selection.Mask[1*8+3] != 255 || reply.Selection.Mask[1*8+7] != 0 || reply.Selection.Mask[1*8] != 0 {
		t.Fatalf("wrong translated selection: %v", reply.Selection.Mask)
	}
	if _, err := svc.SelectWandAt(sess.doc.DocumentID, -1, 0, 0, 0, true, false); err == nil {
		t.Fatal("negative seed accepted")
	}
	if bmp.Pix[0] != 0 || sess.rev != 0 {
		t.Fatal("picking mutated the document")
	}
}

func TestCompositeEyedropperUnpremultipliesAndIgnoresTransparent(t *testing.T) {
	svc, _ := newTestService(t, 2, 1)
	sess := activeSessionOf(t, svc)
	copy(firstLayerBitmap(t, sess).Pix, []byte{64, 32, 0, 128, 0, 0, 0, 0})
	raw, err := svc.SampleCompositeColor(sess.doc.DocumentID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var reply struct{ Color *struct{ R, G, B, A float64 } }
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Color == nil || reply.Color.R != 128.0/255 || reply.Color.G != 64.0/255 {
		t.Fatalf("wrong straight color: %s", raw)
	}
	raw, err = svc.SampleCompositeColor(sess.doc.DocumentID, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(raw), &reply); err != nil || reply.Color != nil {
		t.Fatalf("transparent sample: %s, %v", raw, err)
	}
}
