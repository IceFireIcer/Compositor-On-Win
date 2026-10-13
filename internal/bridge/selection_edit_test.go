package bridge

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

type selectionEditResult struct {
	TabID     string `json:"tabId"`
	Selection struct {
		X    int    `json:"x"`
		Y    int    `json:"y"`
		W    int    `json:"w"`
		H    int    `json:"h"`
		Mask string `json:"mask"`
	} `json:"selection"`
}

func decodeSelectionEdit(t *testing.T, raw string) (selectionEditResult, []uint8) {
	t.Helper()
	var result selectionEditResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("selection response is not JSON: %v", err)
	}
	mask, err := base64.StdEncoding.DecodeString(result.Selection.Mask)
	if err != nil {
		t.Fatalf("selection mask is not base64: %v", err)
	}
	return result, mask
}

func setSelectionTestLayer(t *testing.T, svc *Service, transform domain.Transform, source *render.Bitmap, mask *render.Bitmap) string {
	t.Helper()
	svc.ws.mu.Lock()
	defer svc.ws.mu.Unlock()
	sess := svc.ws.activeSessionLocked()
	if sess == nil || len(sess.doc.Layers) == 0 {
		t.Fatal("missing test layer")
	}
	layer := &sess.doc.Layers[0]
	layer.Transform = transform
	layer.ImageFile = stringPtr("selection-test.png")
	sess.bitmaps[*layer.ImageFile] = source
	if mask != nil {
		layer.MaskFile = stringPtr("selection-test-mask.png")
		sess.bitmaps[*layer.MaskFile] = mask
	}
	return layer.ID
}

func stringPtr(value string) *string { return &value }

func TestLoadLayerSelectionUsesDocumentSpaceTransformForPixelsAndMask(t *testing.T) {
	svc, tabID := newTestService(t, 8, 8)
	source := render.NewBitmap(2, 2)
	// A single opaque source pixel at (0,0) becomes document pixel (3,1)
	// after the 90-degree clockwise transform around (3,2).
	source.Pix[3] = 255
	mask := render.NewBitmap(2, 2)
	mask.Pix[0] = 0
	mask.Pix[4] = 128
	mask.Pix[8] = 255
	mask.Pix[12] = 64
	for i := 0; i < len(mask.Pix); i += 4 {
		mask.Pix[i+3] = 255
	}
	transform := domain.Transform{
		Origin:   [2]float64{2, 1},
		Size:     [2]float64{2, 2},
		Rotation: 90,
		Sampling: domain.SamplingNearest,
	}
	layerID := setSelectionTestLayer(t, svc, transform, source, mask)

	pixelsJSON, err := svc.LoadLayerSelection(tabID, layerID, "pixels")
	if err != nil {
		t.Fatal(err)
	}
	pixels, pixelMask := decodeSelectionEdit(t, pixelsJSON)
	if pixels.TabID != tabID || pixels.Selection.X != 0 || pixels.Selection.Y != 0 || pixels.Selection.W != 8 || pixels.Selection.H != 8 {
		t.Fatalf("pixel selection envelope = %+v", pixels)
	}
	if got := pixelMask[1*8+3]; got != 255 {
		t.Fatalf("transformed alpha at (3,1) = %d, want 255", got)
	}
	if got := pixelMask[1*8+2]; got != 0 {
		t.Fatalf("neighbor alpha at (2,1) = %d, want 0", got)
	}

	maskJSON, err := svc.LoadLayerSelection(tabID, layerID, "mask")
	if err != nil {
		t.Fatal(err)
	}
	_, maskCoverage := decodeSelectionEdit(t, maskJSON)
	// The same source (0,0) lands at (3,1): black mask selects fully.
	if got := maskCoverage[1*8+3]; got != 255 {
		t.Fatalf("inverted black mask at (3,1) = %d, want 255", got)
	}
}

func TestSelectionEditsAreReadOnlyAndPinnedToRequestedTab(t *testing.T) {
	ws := NewWorkspace()
	svc := NewService(ws)
	first, err := ws.NewDocument(4, 2, 72)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ws.NewDocument(4, 2, 72)
	if err != nil {
		t.Fatal(err)
	}
	// Make the requested first tab visibly red while the active second tab
	// remains the default white document.
	ws.mu.Lock()
	firstSession := ws.sessions[first.ActiveID]
	firstLayer := &firstSession.doc.Layers[0]
	firstBitmap := firstSession.bitmaps[*firstLayer.ImageFile]
	for i := 0; i < len(firstBitmap.Pix); i += 4 {
		firstBitmap.Pix[i], firstBitmap.Pix[i+1], firstBitmap.Pix[i+2], firstBitmap.Pix[i+3] = 255, 0, 0, 255
	}
	ws.mu.Unlock()

	beforeDoc, err := ws.DocumentJSON(first.ActiveID)
	if err != nil {
		t.Fatal(err)
	}
	ws.mu.Lock()
	beforeUndo := firstSession.hist.UndoCount()
	beforeHistoryRevision := firstSession.hist.Revision()
	ws.mu.Unlock()
	colorJSON, err := svc.SelectColorRange(first.ActiveID, `{"include":[[255,0,0]],"fuzziness":0}`)
	if err != nil {
		t.Fatal(err)
	}
	result, mask := decodeSelectionEdit(t, colorJSON)
	if result.TabID != first.ActiveID || mask[0] != 255 {
		t.Fatalf("requested background tab was not used: tab=%q mask=%v", result.TabID, mask)
	}
	afterDoc, err := ws.DocumentJSON(first.ActiveID)
	if err != nil {
		t.Fatal(err)
	}
	ws.mu.Lock()
	afterUndo := firstSession.hist.UndoCount()
	afterHistoryRevision := firstSession.hist.Revision()
	ws.mu.Unlock()
	if beforeDoc != afterDoc || beforeUndo != afterUndo || beforeHistoryRevision != afterHistoryRevision {
		t.Fatalf("color range mutated read-only state: doc %v -> %v, undo %d -> %d, history %q -> %q", beforeDoc, afterDoc, beforeUndo, afterUndo, beforeHistoryRevision, afterHistoryRevision)
	}

	// The active second tab is intentionally different; a correct tab-pinned
	// operation still returns the first tab's dimensions and pixels.
	if second.ActiveID == first.ActiveID {
		t.Fatal("test tabs unexpectedly share an ID")
	}
}

func TestSelectColorRangeUsesStraightSRGBIncludeExcludeAndFuzziness(t *testing.T) {
	svc, tabID := newTestService(t, 5, 1)
	sess := activeSessionOf(t, svc)
	layer := &sess.doc.Layers[0]
	bmp := sess.bitmaps[*layer.ImageFile]
	colors := [][3]uint8{
		{255, 0, 0},
		{250, 0, 0},
		{0, 255, 0},
		{0, 0, 255},
		{0, 0, 0},
	}
	for x, color := range colors {
		i := x * 4
		bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = color[0], color[1], color[2], 255
	}

	raw, err := svc.SelectColorRange(tabID, `{"include":[[255,0,0],[0,255,0]],"exclude":[[0,255,0]],"fuzziness":5}`)
	if err != nil {
		t.Fatal(err)
	}
	_, got := decodeSelectionEdit(t, raw)
	want := []uint8{255, 255, 0, 0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("include/exclude color mask = %v, want %v", got, want)
	}

	raw, err = svc.SelectColorRange(tabID, `{"include":[[255,0,0]],"fuzziness":5}`)
	if err != nil {
		t.Fatal(err)
	}
	_, got = decodeSelectionEdit(t, raw)
	want = []uint8{255, 255, 0, 0, 0}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fuzzy color mask = %v, want %v", got, want)
	}

	raw, err = svc.SelectColorRange(tabID, `{"include":[[255,0,0]],"fuzziness":0,"invert":true}`)
	if err != nil {
		t.Fatal(err)
	}
	_, got = decodeSelectionEdit(t, raw)
	want = []uint8{0, 255, 255, 255, 255}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("inverted color mask = %v, want %v", got, want)
	}
}

func TestSelectColorRangeRejectsInvalidSettings(t *testing.T) {
	svc, tabID := newTestService(t, 2, 2)
	for _, settings := range []string{
		`{"include":[[256,0,0]]}`,
		`{"include":[[0,0]]}`,
		`{"exclude":[[-1,0,0]]}`,
		`{"fuzziness":-1}`,
		`{"fuzziness":256}`,
	} {
		if _, err := svc.SelectColorRange(tabID, settings); err == nil {
			t.Fatalf("invalid settings accepted: %s", settings)
		}
	}
}

func TestSampleColorRangeUsesAlphaWeightedThreeByThreeComposite(t *testing.T) {
	svc, tabID := newTestService(t, 5, 5)
	sess := activeSessionOf(t, svc)
	bmp := firstLayerBitmap(t, sess)
	for i := range bmp.Pix {
		bmp.Pix[i] = 0
	}
	// The center is opaque red. Its eight neighbors are half-alpha green;
	// the expected result therefore differs from the center pixel and is
	// determined by premultiplied RGB divided by summed alpha.
	center := (2*5 + 2) * 4
	bmp.Pix[center], bmp.Pix[center+1], bmp.Pix[center+2], bmp.Pix[center+3] = 255, 0, 0, 255
	for y := 1; y <= 3; y++ {
		for x := 1; x <= 3; x++ {
			if x == 2 && y == 2 {
				continue
			}
			i := (y*5 + x) * 4
			bmp.Pix[i], bmp.Pix[i+1], bmp.Pix[i+2], bmp.Pix[i+3] = 0, 128, 0, 128
		}
	}

	centerJSON, err := svc.SampleCompositeColor(tabID, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	var centerReply struct {
		Color *struct {
			R float64 `json:"r"`
			G float64 `json:"g"`
			B float64 `json:"b"`
		} `json:"color"`
	}
	if err := json.Unmarshal([]byte(centerJSON), &centerReply); err != nil {
		t.Fatal(err)
	}
	if centerReply.Color == nil || centerReply.Color.R != 1 || centerReply.Color.G != 0 || centerReply.Color.B != 0 {
		t.Fatalf("center composite sample = %s", centerJSON)
	}

	raw, err := svc.SampleColorRange(tabID, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		TabID string `json:"tabId"`
		Color *struct {
			R uint8 `json:"r"`
			G uint8 `json:"g"`
			B uint8 `json:"b"`
		} `json:"color"`
	}
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.TabID != tabID || reply.Color == nil {
		t.Fatalf("color range sample envelope = %s", raw)
	}
	// sums alpha = 255 + 8*128 = 1279; sums RGB = (255, 1024, 0).
	if got := *reply.Color; got.R != 51 || got.G != 204 || got.B != 0 {
		t.Fatalf("3x3 alpha-weighted sample = %+v, want {51 204 0}", got)
	}
}

func TestSampleColorRangeTreatsOutOfBoundsAsTransparent(t *testing.T) {
	svc, tabID := newTestService(t, 2, 2)
	sess := activeSessionOf(t, svc)
	bmp := firstLayerBitmap(t, sess)
	for i := range bmp.Pix {
		bmp.Pix[i] = 0
	}
	// Only two in-bounds pixels contribute to the 3x3 sample at the corner;
	// the seven out-of-bounds positions are transparent.
	bmp.Pix[0], bmp.Pix[1], bmp.Pix[2], bmp.Pix[3] = 255, 0, 0, 255
	bmp.Pix[4], bmp.Pix[5], bmp.Pix[6], bmp.Pix[7] = 0, 0, 255, 255
	raw, err := svc.SampleColorRange(tabID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Color *struct {
			R uint8 `json:"r"`
			G uint8 `json:"g"`
			B uint8 `json:"b"`
		} `json:"color"`
	}
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Color == nil || reply.Color.R != 128 || reply.Color.G != 0 || reply.Color.B != 128 {
		t.Fatalf("edge sample = %s, want {128 0 128}", raw)
	}
}

func TestSelectionFeatherMaskUsesClampedEdgesAndGray8(t *testing.T) {
	mask := make([]uint8, 9*5)
	for y := 0; y < 5; y++ {
		for x := 0; x < 5; x++ {
			mask[y*9+x] = 255
		}
	}
	soft, err := selectionFeatherMask(mask, 9, 5, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(soft) != len(mask) {
		t.Fatalf("feather length = %d, want %d", len(soft), len(mask))
	}
	intermediate := false
	for _, value := range soft {
		if value > 0 && value < 255 {
			intermediate = true
			break
		}
	}
	if !intermediate {
		t.Fatalf("feather did not produce gray coverage: %v", soft)
	}
	// Replicated padding keeps a solid canvas-edge region from darkening.
	for y := 0; y < 5; y++ {
		if soft[y*9] < 200 {
			t.Fatalf("clamped edge coverage at row %d = %d", y, soft[y*9])
		}
	}

	hard, err := selectionFeatherMask(mask, 9, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(hard, mask) {
		t.Fatal("zero feather changed the mask")
	}
	for _, feather := range []float64{-1, 251, math.NaN(), math.Inf(1)} {
		if _, err := selectionFeatherMask(mask, 9, 5, feather); err == nil {
			t.Fatalf("invalid feather accepted: %v", feather)
		}
	}
}

func TestSelectionFeatherMaskLargeCanvasStaysGray8AndBounded(t *testing.T) {
	const width, height = 3840, 2160
	mask := make([]uint8, width*height)
	for y := 300; y < height-300; y++ {
		for x := 400; x < width-400; x++ {
			mask[y*width+x] = 255
		}
	}

	soft, err := selectionFeatherMask(mask, width, height, 250)
	if err != nil {
		t.Fatal(err)
	}
	if len(soft) != len(mask) {
		t.Fatalf("large feather length = %d, want %d", len(soft), len(mask))
	}
	if soft[0] != 0 || soft[(height/2)*width+width/2] < 250 {
		t.Fatalf("large feather coverage lost hard regions: corner=%d center=%d", soft[0], soft[(height/2)*width+width/2])
	}
	intermediate := false
	for _, value := range soft {
		if value > 0 && value < 255 {
			intermediate = true
			break
		}
	}
	if !intermediate {
		t.Fatal("large feather did not produce gray edge coverage")
	}
}

func BenchmarkSelectionFeatherMask4K(b *testing.B) {
	const width, height = 3840, 2160
	mask := make([]uint8, width*height)
	for y := 300; y < height-300; y++ {
		for x := 400; x < width-400; x++ {
			mask[y*width+x] = 255
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		soft, err := selectionFeatherMask(mask, width, height, 250)
		if err != nil || len(soft) != len(mask) {
			b.Fatalf("large feather failed: %v", err)
		}
	}
}
