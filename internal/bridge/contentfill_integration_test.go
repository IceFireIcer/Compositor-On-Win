package bridge

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

// Exercise the import placement and public filter endpoints with the same
// document-sized coverage payload emitted by selectionPayload in the UI.
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

func TestContentFillOffsetSmallImportedLayer(t *testing.T) {
	// ContentFill.c uses radius 0 for a 3x3 layer. A one-pixel selection
	// leaves eight opaque donors. Zero padding in a document-sized mask
	// must not enlarge the working grid and change that donor requirement.
	for _, mode := range []string{"apply", "commit"} {
		t.Run(mode, func(t *testing.T) {
			svc, sess, key, placement := contentFillImportedFixture(t, 16, 16, 3, 3)
			base := sess.bitmaps[key]
			copy(base.Pix[16:20], []byte{255, 0, 0, 255})
			original := base.Clone()
			sel := filterSelection{W: 16, H: 16, Mask: make([]byte, 16*16)}
			x, y := int(placement.Origin[0])+1, int(placement.Origin[1])+1
			sel.Mask[y*sel.W+x] = 255
			payload := contentFillSelectionJSON(t, sel)
			if mode == "apply" {
				if _, err := svc.ApplyFilter(FilterContentFill, "", "{}", 0, payload); err != nil {
					t.Fatalf("one selected pixel with eight unselected opaque donors must fill: %v", err)
				}
			} else {
				if _, err := svc.BeginFilterEdit(FilterContentFill, "", "{}", 0, payload); err != nil {
					t.Fatal(err)
				}
				if _, err := svc.CommitFilter("{}"); err != nil {
					t.Fatalf("one selected pixel with eight unselected opaque donors must fill: %v", err)
				}
			}
			got := sess.bitmaps[key]
			if got.W != 3 || got.H != 3 {
				t.Fatalf("fill changed layer extent: %dx%d", got.W, got.H)
			}
			for p := 0; p < 9; p++ {
				want := original.Pix[p*4 : p*4+4]
				if p == 4 {
					want = []byte{0, 180, 0, 255}
				}
				if !bytes.Equal(got.Pix[p*4:p*4+4], want) {
					t.Fatalf("pixel %d: got %v, want %v (only center is selected)", p, got.Pix[p*4:p*4+4], want)
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
		})
	}
}

func TestContentFillOffsetTightSelectionControl(t *testing.T) {
	svc, sess, key, placement := contentFillImportedFixture(t, 16, 16, 3, 3)
	copy(sess.bitmaps[key].Pix[16:20], []byte{255, 0, 0, 255})
	sel := filterSelection{X: int(placement.Origin[0]) + 1, Y: int(placement.Origin[1]) + 1, W: 1, H: 1, Mask: []byte{255}}
	if _, err := svc.ApplyFilter(FilterContentFill, "", "{}", 0, contentFillSelectionJSON(t, sel)); err != nil {
		t.Fatal(err)
	}
	if got := sess.bitmaps[key].Pix[16:20]; !bytes.Equal(got, []byte{0, 180, 0, 255}) {
		t.Fatalf("tight selection did not fill center from green donors: %v", got)
	}
}

// Optional differential audit against the repository's existing copied C
// driver. Normal regression runs need no native compiler. Set the variable
// to a freshly compiled tests/golden/c driver to repeat the C comparison.
func TestContentFillOffsetCReference(t *testing.T) {
	driver := os.Getenv("COMPOSITOR_CONTENTFILL_C_DRIVER")
	if driver == "" {
		t.Skip("set COMPOSITOR_CONTENTFILL_C_DRIVER to run the C differential audit")
	}
	for _, side := range []int{3, 16} {
		t.Run(strconv.Itoa(side), func(t *testing.T) {
			svc, sess, key, placement := contentFillImportedFixture(t, 64, 48, side, side)
			base := sess.bitmaps[key]
			// Nonuniform donors also check the fixed C search seed and
			// patch matching rather than only the output color.
			for p := 0; p < side*side; p++ {
				base.Pix[p*4], base.Pix[p*4+1], base.Pix[p*4+2] = byte(p*37), byte(p*73), byte(p*19)
			}
			center := (side/2*side + side/2) * 4
			copy(base.Pix[center:center+4], []byte{255, 0, 0, 255})
			mask := make([]byte, side*side)
			mask[center/4] = 255
			dir := t.TempDir()
			input, output, maskPath := filepath.Join(dir, "input.raw"), filepath.Join(dir, "output.raw"), filepath.Join(dir, "mask.raw")
			if err := os.WriteFile(input, base.Pix, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(maskPath, mask, 0600); err != nil {
				t.Fatal(err)
			}
			if out, err := exec.Command(driver, "content_fill", input, output, strconv.Itoa(side), strconv.Itoa(side), maskPath).CombinedOutput(); err != nil {
				t.Fatalf("C reference failed: %v: %s", err, out)
			}
			want, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			sel := filterSelection{W: 64, H: 48, Mask: make([]byte, 64*48)}
			x, y := int(placement.Origin[0])+side/2, int(placement.Origin[1])+side/2
			sel.Mask[y*sel.W+x] = 255
			if _, err := svc.ApplyFilter(FilterContentFill, "", "{}", 0, contentFillSelectionJSON(t, sel)); err != nil {
				t.Fatal(err)
			}
			if got := sess.bitmaps[key]; got.W != side || got.H != side || !bytes.Equal(got.Pix, want) {
				t.Fatalf("offset imported layer differs from C reference: got %dx%d", got.W, got.H)
			}
		})
	}
}

func TestContentFillOffsetGridUsesCoverageBounds(t *testing.T) {
	_, sess, key, placement := contentFillImportedFixture(t, 64, 48, 16, 16)
	sel := filterSelection{W: 64, H: 48, Mask: make([]byte, 64*48)}
	sel.Mask[20*64+28] = 128
	fs, err := prepareFilterGrid(FilterContentFill, sess.bitmaps[key], placement, &filterParams{}, &sel)
	if err != nil {
		t.Fatal(err)
	}
	if fs.grown.W != 16 || fs.grown.H != 16 || fs.grownT != placement {
		t.Fatalf("zero mask padding must not grow or move the layer: grid=%dx%d transform=%+v, want 16x16 %+v", fs.grown.W, fs.grown.H, fs.grownT, placement)
	}
	if sel.Mask[20*64+28] != 128 {
		t.Fatal("computing bounds changed the coverage byte")
	}
}

func TestContentFillOffsetCoverageAndUnselectedBytes(t *testing.T) {
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
			// rounded to RGBA8. Nonzero coverage remains a target, not a donor.
			wantGreen := byte((180*int(coverage) + 127) / 255)
			copy(original.Pix[p*4:p*4+4], []byte{255 - coverage, wantGreen, 0, 255})
			if got.W != original.W || got.H != original.H || !bytes.Equal(got.Pix, original.Pix) {
				t.Fatalf("coverage %d: only the selected pixel may change, want %v, got %v", coverage, original.Pix[p*4:p*4+4], got.Pix[p*4:p*4+4])
			}
			if sel.Mask[y*64+x] != coverage {
				t.Fatal("fill mutated the selection byte")
			}
		})
	}
}

func TestContentFillOffsetNoDonorsPreservesDocument(t *testing.T) {
	for _, mode := range []string{"apply", "commit"} {
		for _, donors := range []string{"none", "thin-border", "transparent", "partial-alpha"} {
			t.Run(mode+"/"+donors, func(t *testing.T) {
				svc, sess, key, placement := contentFillImportedFixture(t, 64, 48, 16, 16)
				base := sess.bitmaps[key]
				sel := filterSelection{W: 64, H: 48, Mask: make([]byte, 64*48)}
				for y := 0; y < 16; y++ {
					for x := 0; x < 16; x++ {
						selected := donors == "none" || x >= 1 && x < 15 && y >= 1 && y < 15
						if selected {
							sel.Mask[(int(placement.Origin[1])+y)*64+int(placement.Origin[0])+x] = 255
						} else if donors == "transparent" {
							copy(base.Pix[(y*16+x)*4:(y*16+x)*4+4], []byte{0, 0, 0, 0})
						} else if donors == "partial-alpha" {
							copy(base.Pix[(y*16+x)*4:(y*16+x)*4+4], []byte{0, 90, 0, 128})
						}
					}
				}
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
					t.Fatalf("want no-donor failure, got %v", err)
				}
				l, _, _ := resolveFilterTarget(sess, "")
				if !bytes.Equal(sess.bitmaps[key].Pix, original.Pix) || l.Transform != placement || sess.hist.Revision() != historyRev || sess.rev != displayRev || len(sess.rasterJournal) != journalLen || svc.ws.tabs[0].Dirty != dirty {
					t.Fatal("no-donor failure changed pixels, placement, history, display revision, journal or dirty state")
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

func TestContentFillOffsetNearlyWholeLayerUsesOnlyUnselectedDonors(t *testing.T) {
	svc, sess, key, placement := contentFillImportedFixture(t, 64, 48, 16, 16)
	base := sess.bitmaps[key]
	sel := filterSelection{W: 64, H: 48, Mask: make([]byte, 64*48)}
	// A subject mask selecting the photo except its red center legitimately
	// fills the selected green texture from the remaining red 6x6 patch.
	// Reversing polarity would violate ContentFill.c's donor semantics.
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			if x >= 5 && x < 11 && y >= 5 && y < 11 {
				copy(base.Pix[(y*16+x)*4:(y*16+x)*4+4], []byte{255, 0, 0, 255})
			} else {
				sel.Mask[(int(placement.Origin[1])+y)*64+int(placement.Origin[0])+x] = 255
			}
		}
	}
	if _, err := svc.ApplyFilter(FilterContentFill, "", "{}", 0, contentFillSelectionJSON(t, sel)); err != nil {
		t.Fatal(err)
	}
	got := sess.bitmaps[key]
	if got.W != 16 || got.H != 16 {
		t.Fatalf("unexpected extent %dx%d", got.W, got.H)
	}
	for p := 0; p < 16*16; p++ {
		if !bytes.Equal(got.Pix[p*4:p*4+4], []byte{255, 0, 0, 255}) {
			t.Fatalf("pixel %d was not filled from the only unselected donor patch: %v", p, got.Pix[p*4:p*4+4])
		}
	}
}

func TestContentFillOffsetExtendsOnlyToSelectedPixels(t *testing.T) {
	svc, sess, key, placement := contentFillImportedFixture(t, 64, 48, 16, 16)
	sel := filterSelection{W: 64, H: 48, Mask: make([]byte, 64*48)}
	for y := 4; y < 12; y++ {
		for x := 12; x < 20; x++ {
			sel.Mask[(int(placement.Origin[1])+y)*64+int(placement.Origin[0])+x] = 255
		}
	}
	if _, err := svc.ApplyFilter(FilterContentFill, "", "{}", 0, contentFillSelectionJSON(t, sel)); err != nil {
		t.Fatal(err)
	}
	got := sess.bitmaps[key]
	if got.W != 20 || got.H != 16 {
		t.Fatalf("want 20x16 extended layer, got %dx%d", got.W, got.H)
	}
	for y := 0; y < got.H; y++ {
		for x := 0; x < got.W; x++ {
			want := []byte{0, 0, 0, 0}
			if x < 16 || y >= 4 && y < 12 {
				want = []byte{0, 180, 0, 255}
			}
			if !bytes.Equal(got.Pix[(y*got.W+x)*4:(y*got.W+x)*4+4], want) {
				t.Fatalf("pixel (%d,%d): unselected transparency must remain clear", x, y)
			}
		}
	}
	l, _, _ := resolveFilterTarget(sess, "")
	wantT := placement
	wantT.Size[0] = 20
	if l.Transform != wantT {
		t.Fatalf("extension moved the source: %+v, want %+v", l.Transform, wantT)
	}
}

func TestContentFillOffsetEmptyCoverageIsNoOp(t *testing.T) {
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
