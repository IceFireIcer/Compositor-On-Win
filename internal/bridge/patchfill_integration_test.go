package bridge

// Content-aware fill integration: the selection mask → fill path through the
// public filter endpoints, now backed by the multi-scale PatchMatch engine
// (render.PatchFill, the port of PatchPixels.c). The old single-scale
// content_fill only needed a 5 × 5 patch of donors, so a 3 × 3 layer could
// fill; PatchMatch matches 7 × 7 patches lying wholly on opaque, unselected
// pixels, so small layers and fully selected ones now fail with no source
// pixels, exactly as ContentFill.swift does when patch_fill returns 0.

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

// contentFillImportedFixture exercises the import placement and public filter
// endpoints with the same document-sized coverage payload emitted by
// selectionPayload in the UI.
func contentFillImportedFixture(t *testing.T, docW, docH, w, h int) (*Service, *session, string, domain.Transform) {
	t.Helper()
	svc, _ := newTestService(t, docW, docH)
	bmp := render.NewBitmap(w, h)
	for i := 0; i < len(bmp.Pix); i += 4 {
		copy(bmp.Pix[i:i+4], []byte{0, 180, 0, 255})
	}
	if _, err := svc.ws.commitImportedLayers([]pendingImportFile{{name: "Offset photo", bmp: bmp}}); err != nil {
		t.Fatal(err)
	}
	sess := activeSessionOf(t, svc)
	l, _, err := resolveFilterTarget(sess, "")
	if err != nil {
		t.Fatal(err)
	}
	return svc, sess, *l.ImageFile, l.Transform
}

func contentFillSelectionJSON(t *testing.T, sel filterSelection) string {
	t.Helper()
	raw, err := json.Marshal(sel)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// selectLayerRect marks the half-open layer rectangle [x0, x1) × [y0, y1) in a
// document-sized selection mask.
func selectLayerRect(sel *filterSelection, placement domain.Transform, x0, y0, x1, y1 int) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			sel.Mask[(int(placement.Origin[1])+y)*sel.W+int(placement.Origin[0])+x] = 255
		}
	}
}

// TestPatchFillOffsetFillsSelectedPixelsOnly: a red blemish under the
// selection is rebuilt from the surrounding surface, and nothing outside the
// selection moves.
func TestPatchFillOffsetFillsSelectedPixelsOnly(t *testing.T) {
	svc, sess, key, placement := contentFillImportedFixture(t, 64, 48, 32, 32)
	base := sess.bitmaps[key]
	for y := 12; y < 20; y++ {
		for x := 12; x < 20; x++ {
			copy(base.Pix[(y*32+x)*4:(y*32+x)*4+4], []byte{230, 20, 20, 255})
		}
	}
	original := base.Clone()
	sel := filterSelection{W: 64, H: 48, Mask: make([]byte, 64*48)}
	selectLayerRect(&sel, placement, 12, 12, 20, 20)
	if _, err := svc.ApplyFilter(FilterContentFill, "", "{}", 0, contentFillSelectionJSON(t, sel)); err != nil {
		t.Fatal(err)
	}
	got := sess.bitmaps[key]
	if got.W != 32 || got.H != 32 {
		t.Fatalf("fill changed layer extent: %dx%d", got.W, got.H)
	}
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			i := (y*32 + x) * 4
			if x < 12 || x >= 20 || y < 12 || y >= 20 {
				if !bytes.Equal(got.Pix[i:i+4], original.Pix[i:i+4]) {
					t.Fatalf("unselected pixel (%d,%d) changed: %v -> %v", x, y, original.Pix[i:i+4], got.Pix[i:i+4])
				}
				continue
			}
			if got.Pix[i] > 48 || got.Pix[i+1] < 150 || got.Pix[i+1] > 210 || got.Pix[i+2] > 48 || got.Pix[i+3] != 255 {
				t.Fatalf("selected pixel (%d,%d) is %v, not filled from the green surround", x, y, got.Pix[i:i+4])
			}
		}
	}
	l, _, _ := resolveFilterTarget(sess, "")
	if l.Transform != placement {
		t.Fatalf("fill moved imported layer: %+v -> %+v", placement, l.Transform)
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sess.bitmaps[key].Pix, original.Pix) {
		t.Fatal("undo did not restore the original pixels")
	}
}

// TestPatchFillOffsetIgnoresTransparentOutside: transparent pixels outside the
// hole are neither matched against nor copied from — the fill comes from the
// opaque square — and the layer is then trimmed to that square's alpha bounds
// (ApplyFilter's own rule), leaving the clear frame behind.
func TestPatchFillOffsetIgnoresTransparentOutside(t *testing.T) {
	const frame = 8
	svc, sess, key, placement := contentFillImportedFixture(t, 64, 64, 48, 48)
	base := sess.bitmaps[key]
	// An opaque green square with transparent magenta around it: a fill that
	// matched or copied the clear pixels would come out magenta.
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			i := (y*48 + x) * 4
			if x >= frame && x < 48-frame && y >= frame && y < 48-frame {
				copy(base.Pix[i:i+4], []byte{0, 180, 0, 255})
			} else {
				copy(base.Pix[i:i+4], []byte{255, 0, 255, 0})
			}
		}
	}
	original := base.Clone()
	sel := filterSelection{W: 64, H: 64, Mask: make([]byte, 64*64)}
	selectLayerRect(&sel, placement, frame, frame, frame+12, frame+12) // touching the square's corner
	if _, err := svc.ApplyFilter(FilterContentFill, "", "{}", 0, contentFillSelectionJSON(t, sel)); err != nil {
		t.Fatal(err)
	}
	got := sess.bitmaps[key]
	if got.W != 32 || got.H != 32 {
		t.Fatalf("fill changed layer extent: %dx%d, want the 32x32 opaque square", got.W, got.H)
	}
	for y := 0; y < got.H; y++ {
		for x := 0; x < got.W; x++ {
			i := (y*got.W + x) * 4
			if x < 12 && y < 12 {
				if got.Pix[i] > 48 || got.Pix[i+2] > 48 || got.Pix[i+3] != 255 {
					t.Fatalf("hole pixel (%d,%d) is %v; transparent magenta leaked into the fill", x, y, got.Pix[i:i+4])
				}
				continue
			}
			oi := ((y+frame)*48 + x + frame) * 4
			if !bytes.Equal(got.Pix[i:i+4], original.Pix[oi:oi+4]) {
				t.Fatalf("opaque pixel (%d,%d) outside the hole changed: %v -> %v",
					x, y, original.Pix[oi:oi+4], got.Pix[i:i+4])
			}
		}
	}
	l, _, _ := resolveFilterTarget(sess, "")
	wantT := placement
	wantT.Origin[0] += frame
	wantT.Origin[1] += frame
	wantT.Size[0], wantT.Size[1] = 32, 32
	if l.Transform != wantT {
		t.Fatalf("trim moved the layer wrongly: %+v, want %+v", l.Transform, wantT)
	}
}

// TestPatchFillOffsetAtCanvasEdge: a selection reaching the layer's edge still
// fills from the image on the other side, and the layer keeps its extent.
func TestPatchFillOffsetAtCanvasEdge(t *testing.T) {
	svc, sess, key, placement := contentFillImportedFixture(t, 64, 64, 32, 32)
	base := sess.bitmaps[key]
	// Green everywhere except a red strip along the right edge: the fill has
	// sources on one side only.
	for y := 0; y < 32; y++ {
		for x := 26; x < 32; x++ {
			copy(base.Pix[(y*32+x)*4:(y*32+x)*4+4], []byte{230, 20, 20, 255})
		}
	}
	original := base.Clone()
	sel := filterSelection{W: 64, H: 64, Mask: make([]byte, 64*64)}
	selectLayerRect(&sel, placement, 26, 0, 32, 32)
	if _, err := svc.ApplyFilter(FilterContentFill, "", "{}", 0, contentFillSelectionJSON(t, sel)); err != nil {
		t.Fatal(err)
	}
	got := sess.bitmaps[key]
	if got.W != 32 || got.H != 32 {
		t.Fatalf("fill changed layer extent: %dx%d", got.W, got.H)
	}
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			i := (y*32 + x) * 4
			if x < 26 {
				if !bytes.Equal(got.Pix[i:i+4], original.Pix[i:i+4]) {
					t.Fatalf("unselected pixel (%d,%d) changed: %v -> %v", x, y, original.Pix[i:i+4], got.Pix[i:i+4])
				}
				continue
			}
			if got.Pix[i] > 48 || got.Pix[i+1] < 150 || got.Pix[i+2] > 48 || got.Pix[i+3] != 255 {
				t.Fatalf("edge pixel (%d,%d) is %v, not filled from the green image", x, y, got.Pix[i:i+4])
			}
		}
	}
	l, _, _ := resolveFilterTarget(sess, "")
	if l.Transform != placement {
		t.Fatalf("edge fill moved the layer: %+v -> %+v", placement, l.Transform)
	}
}

// TestPatchFillOffsetNoSourcesPreservesDocument: without a 7 × 7 patch of
// opaque, unselected pixels the fill reports no source pixels and changes
// nothing at all.
func TestPatchFillOffsetNoSourcesPreservesDocument(t *testing.T) {
	cases := []struct {
		name    string
		docW    int
		docH    int
		w, h    int
		prepare func(t *testing.T, b *render.Bitmap, sel *filterSelection, placement domain.Transform)
	}{
		{
			name: "fully selected layer", docW: 64, docH: 48, w: 16, h: 16,
			prepare: func(t *testing.T, b *render.Bitmap, sel *filterSelection, placement domain.Transform) {
				selectLayerRect(sel, placement, 0, 0, 16, 16)
			},
		},
		{
			name: "3x3 layer has no 7x7 patch", docW: 16, docH: 16, w: 3, h: 3,
			prepare: func(t *testing.T, b *render.Bitmap, sel *filterSelection, placement domain.Transform) {
				selectLayerRect(sel, placement, 1, 1, 2, 2)
			},
		},
		{
			name: "only a 6x6 donor patch", docW: 64, docH: 48, w: 16, h: 16,
			prepare: func(t *testing.T, b *render.Bitmap, sel *filterSelection, placement domain.Transform) {
				selectLayerRect(sel, placement, 0, 0, 16, 16)
				for y := 5; y < 11; y++ {
					for x := 5; x < 11; x++ {
						copy(b.Pix[(y*16+x)*4:(y*16+x)*4+4], []byte{255, 0, 0, 255})
						sel.Mask[(int(placement.Origin[1])+y)*sel.W+int(placement.Origin[0])+x] = 0
					}
				}
			},
		},
	}
	for _, mode := range []string{"apply", "commit"} {
		for _, tc := range cases {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				svc, sess, key, placement := contentFillImportedFixture(t, tc.docW, tc.docH, tc.w, tc.h)
				base := sess.bitmaps[key]
				sel := filterSelection{W: tc.docW, H: tc.docH, Mask: make([]byte, tc.docW*tc.docH)}
				tc.prepare(t, base, &sel, placement)
				original := base.Clone()
				historyRev, displayRev, journalLen := sess.hist.Revision(), sess.rev, len(sess.rasterJournal)
				dirty := svc.ws.tabs[0].Dirty
				payload := contentFillSelectionJSON(t, sel)
				var err error
				if mode == "apply" {
					_, err = svc.ApplyFilter(FilterContentFill, "", "{}", 0, payload)
				} else {
					if _, err := svc.BeginFilterEdit(FilterContentFill, "", "{}", 0, payload); err != nil {
						t.Fatal(err)
					}
					_, err = svc.CommitFilter("{}")
				}
				if err == nil || !strings.Contains(err.Error(), "没有可用源像素") {
					t.Fatalf("want no-source failure, got %v", err)
				}
				l, _, _ := resolveFilterTarget(sess, "")
				if !bytes.Equal(sess.bitmaps[key].Pix, original.Pix) || l.Transform != placement ||
					sess.hist.Revision() != historyRev || sess.rev != displayRev ||
					len(sess.rasterJournal) != journalLen || svc.ws.tabs[0].Dirty != dirty {
					t.Fatal("no-source failure changed pixels, placement, history, display revision, journal or dirty state")
				}
				if mode == "commit" {
					if sess.filter == nil {
						t.Fatal("failed commit discarded the edit session")
					}
					if _, err := svc.CancelFilterEdit(); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

// TestPatchFillOffsetCoverageAndUnselectedBytes: coverage × opacity is the
// blend the selection asks for, and a nonzero coverage byte marks a target,
// not a donor.
func TestPatchFillOffsetCoverageAndUnselectedBytes(t *testing.T) {
	for _, coverage := range []byte{1, 128, 255} {
		t.Run(strconv.Itoa(int(coverage)), func(t *testing.T) {
			svc, sess, key, placement := contentFillImportedFixture(t, 64, 48, 16, 16)
			base := sess.bitmaps[key]
			p := 8*16 + 8
			copy(base.Pix[p*4:p*4+4], []byte{255, 0, 0, 255})
			original := base.Clone()
			sel := filterSelection{W: 64, H: 48, Mask: make([]byte, 64*48)}
			x, y := int(placement.Origin[0])+8, int(placement.Origin[1])+8
			sel.Mask[y*64+x] = coverage
			payload := contentFillSelectionJSON(t, sel)
			if _, err := svc.ApplyFilter(FilterContentFill, "", "{}", 0, payload); err != nil {
				t.Fatal(err)
			}
			got := sess.bitmaps[key]
			// PixelAdjust.blend: coverage*filtered + (1-coverage)*original,
			// rounded to RGBA8.
			wantGreen := byte((180*int(coverage) + 127) / 255)
			copy(original.Pix[p*4:p*4+4], []byte{255 - coverage, wantGreen, 0, 255})
			if got.W != original.W || got.H != original.H || !bytes.Equal(got.Pix, original.Pix) {
				t.Fatalf("coverage %d: only the selected pixel may change, want %v, got %v",
					coverage, original.Pix[p*4:p*4+4], got.Pix[p*4:p*4+4])
			}
			if sel.Mask[y*64+x] != coverage {
				t.Fatal("fill mutated the selection byte")
			}
		})
	}
}

func TestPatchFillOffsetEmptyCoverageIsNoOp(t *testing.T) {
	svc, sess, key, placement := contentFillImportedFixture(t, 64, 48, 16, 16)
	original := sess.bitmaps[key].Clone()
	rev := sess.hist.Revision()
	sel := filterSelection{W: 64, H: 48, Mask: make([]byte, 64*48)}
	if _, err := svc.ApplyFilter(FilterContentFill, "", "{}", 0, contentFillSelectionJSON(t, sel)); err != nil {
		t.Fatal(err)
	}
	l, _, _ := resolveFilterTarget(sess, "")
	if !bytes.Equal(sess.bitmaps[key].Pix, original.Pix) || l.Transform != placement || sess.hist.Revision() != rev {
		t.Fatal("empty coverage changed the document")
	}
}

// TestPatchFillIgnoresTheMaskPayloadPadding: the frontend sends a
// document-sized mask whose zero padding is storage, not selection. Filling
// the same layer pixel in two documents of different sizes must therefore give
// the same pixels — the grid may only grow to the coverage's own bounds, never
// to the payload rect (whose size the document decides).
func TestPatchFillIgnoresTheMaskPayloadPadding(t *testing.T) {
	fill := func(docW, docH int) []byte {
		svc, sess, key, placement := contentFillImportedFixture(t, docW, docH, 16, 16)
		base := sess.bitmaps[key]
		// Nonuniform donors, so a different search grid really shows.
		for p := 0; p < 16*16; p++ {
			base.Pix[p*4], base.Pix[p*4+1], base.Pix[p*4+2] = byte(p*37), byte(p*73), byte(p*19)
		}
		sel := filterSelection{W: docW, H: docH, Mask: make([]byte, docW*docH)}
		selectLayerRect(&sel, placement, 3, 3, 4, 4)
		if _, err := svc.ApplyFilter(FilterContentFill, "", "{}", 0, contentFillSelectionJSON(t, sel)); err != nil {
			t.Fatalf("doc %dx%d: %v", docW, docH, err)
		}
		_, bmp, err := resolveFilterTarget(sess, "")
		if err != nil {
			t.Fatal(err)
		}
		return append([]byte(nil), bmp.Pix...)
	}
	wide, narrow := fill(64, 48), fill(32, 24)
	if !bytes.Equal(wide, narrow) {
		t.Fatal("the fill depends on the document-sized mask's padding: same layer pixel, different result")
	}
}

// Optional differential audit against the repository's copied C kernel.
// Normal regression runs need no native compiler; set the variable to a
// freshly compiled tests/golden/c driver to repeat the comparison. The driver's
// "heal" command with mode 0 and opacity 1 is patch_fill followed by the exact
// coverage × opacity blend, so it must agree byte for byte with the bridge's
// PatchFill path at the same seed.
func TestPatchFillOffsetCReference(t *testing.T) {
	driver := os.Getenv("COMPOSITOR_PATCHFILL_C_DRIVER")
	if driver == "" {
		t.Skip("set COMPOSITOR_PATCHFILL_C_DRIVER to run the C differential audit")
	}
	svc, sess, key, placement := contentFillImportedFixture(t, 64, 48, 32, 32)
	base := sess.bitmaps[key]
	// Nonuniform donors also check the fixed search seed and patch matching
	// rather than only the output color.
	for p := 0; p < 32*32; p++ {
		base.Pix[p*4], base.Pix[p*4+1], base.Pix[p*4+2] = byte(p*37), byte(p*73), byte(p*19)
	}
	mask := make([]byte, 32*32)
	for y := 12; y < 20; y++ {
		for x := 12; x < 20; x++ {
			mask[y*32+x] = 255
		}
	}
	dir := t.TempDir()
	input, output := filepath.Join(dir, "input.raw"), filepath.Join(dir, "output.raw")
	maskPath := filepath.Join(dir, "mask.raw")
	if err := os.WriteFile(input, base.Pix, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(maskPath, mask, 0600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(driver, "heal", input, output, "32", "32", maskPath, "1", "0", "0").CombinedOutput(); err != nil {
		t.Fatalf("C reference failed: %v: %s", err, out)
	}
	want, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	sel := filterSelection{W: 64, H: 48, Mask: make([]byte, 64*48)}
	selectLayerRect(&sel, placement, 12, 12, 20, 20)
	if _, err := svc.ApplyFilter(FilterContentFill, "", "{}", 0, contentFillSelectionJSON(t, sel)); err != nil {
		t.Fatal(err)
	}
	if got := sess.bitmaps[key]; got.W != 32 || got.H != 32 || !bytes.Equal(got.Pix, want) {
		t.Fatalf("imported layer differs from the C reference: got %dx%d", got.W, got.H)
	}
}
