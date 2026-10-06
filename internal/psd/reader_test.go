package psd

// The ported test families: PSDRoundTripTests (ordering/blends/groups/
// masks/clipping/limits), PSBImportTests (large-document parity), PSDAdjustment
// Tests (levl/curv/hue2 and mask placement) and CropToCanvasImportTests
// (oversized layers cropped to the canvas).

import (
	"bytes"
	"strings"
	"testing"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

func build(t *testing.T, doc fixtureDoc, composite *render.Bitmap) []byte {
	t.Helper()
	return fixtureData(doc, composite, false, "", nil, nil)
}

func buildPSB(t *testing.T, doc fixtureDoc, composite *render.Bitmap) []byte {
	t.Helper()
	return fixtureData(doc, composite, true, "", nil, nil)
}

func mustRead(t *testing.T, data []byte, remaining int) Document {
	t.Helper()
	doc, err := Read(data, remaining)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return doc
}

// rgbaPixels flattens a record's image (premultiplied RGBA).
func rgbaPixels(b *render.Bitmap) []uint8 {
	out := make([]uint8, len(b.Pix))
	copy(out, b.Pix)
	return out
}

func TestRoundTripLayersOrderVisibilityOpacityAndBlend(t *testing.T) {
	red := solidBitmap(2, 2, 255, 0, 0, 255)
	blue := solidBitmap(2, 2, 0, 0, 255, 255)
	composite := solidBitmap(4, 4, 0, 0, 0, 0)
	bottom := fixtureRecord{id: "L1", name: "Red", visible: true, opacity: 0.5, blendKey: "mul ", left: 0, top: 0, width: 2, height: 2, image: red}
	top := fixtureRecord{id: "L2", name: "Blue", visible: false, opacity: 1, blendKey: "norm", left: 2, top: 0, width: 2, height: 2, image: blue}
	data := build(t, fixtureDoc{width: 4, height: 4, resolution: 144, layers: []fixtureRecord{bottom, top}}, composite)
	if !bytes.HasPrefix(data, []byte("8BPS")) {
		t.Fatal("magic missing")
	}
	doc := mustRead(t, data, 1<<30)
	if doc.Width != 4 || doc.Height != 4 || doc.Resolution != 144 {
		t.Fatalf("header = %dx%d @%v", doc.Width, doc.Height, doc.Resolution)
	}
	if len(doc.Layers) != 2 || doc.Layers[0].Name != "Red" || doc.Layers[1].Name != "Blue" {
		t.Fatalf("layers = %+v", doc.Layers)
	}
	if !doc.Layers[0].IsVisible || doc.Layers[1].IsVisible {
		t.Fatal("visibility mismatch")
	}
	if v := doc.Layers[0].Opacity; v < 0.49 || v > 0.51 {
		t.Fatalf("opacity = %v, want ≈0.5", v)
	}
	if doc.Layers[0].BlendKey != "mul " {
		t.Fatalf("blend key = %q", doc.Layers[0].BlendKey)
	}
	if doc.Layers[0].Image == nil || doc.Layers[0].Image.W != 2 {
		t.Fatal("layer image missing")
	}
	imported, err := Build(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Conversions) != 0 {
		t.Fatalf("unexpected conversions: %+v", imported.Conversions)
	}
	if *imported.Document.Layers[0].BlendMode != "Multiply" {
		t.Fatalf("imported blend = %v", *imported.Document.Layers[0].BlendMode)
	}
	if imported.Document.Layers[1].IsVisible {
		t.Fatal("imported visibility mismatch")
	}
}

func TestRoundTripGroupsMasksAndClipping(t *testing.T) {
	fill := solidBitmap(2, 2, 0, 255, 0, 255)
	clipped := solidBitmap(2, 2, 255, 255, 0, 255)
	mask := make([]uint8, 4)
	for i := range mask {
		mask[i] = 255
	}
	composite := solidBitmap(4, 4, 0, 0, 0, 0)
	groupID := "G1"
	group := fixtureRecord{id: groupID, name: "Stack", isGroup: true, visible: true, opacity: 1, blendKey: "pass"}
	base := fixtureRecord{id: "B1", parentID: groupID, name: "Base", visible: true, opacity: 1, blendKey: "norm", left: 0, top: 0, width: 2, height: 2, image: fill, mask: mask, maskW: 2, maskH: 2}
	child := fixtureRecord{id: "C1", parentID: groupID, name: "Clipped", visible: true, opacity: 1, blendKey: "norm", clipping: true, left: 0, top: 0, width: 2, height: 2, image: clipped}
	imported, err := Build(mustRead(t, build(t, fixtureDoc{width: 4, height: 4, resolution: 72, layers: []fixtureRecord{group, base, child}}, composite), 1<<30))
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Conversions) != 0 {
		t.Fatalf("unexpected conversions: %+v", imported.Conversions)
	}
	var folderID, baseID, childID string
	var baseMask, childClip *string
	for _, l := range imported.Document.Layers {
		switch l.Name {
		case "Stack":
			folderID = l.ID
			if !l.IsGroupLayer() {
				t.Fatal("Stack must be a group")
			}
		case "Base":
			baseID = l.ID
			baseMask = l.MaskFile
		case "Clipped":
			childID = l.ID
			childClip = l.MaskSourceID
		}
	}
	if folderID == "" || baseID == "" || childID == "" {
		t.Fatal("layers missing")
	}
	for _, l := range imported.Document.Layers {
		if l.ParentID == nil || *l.ParentID != folderID {
			if l.Name != "Stack" {
				t.Fatalf("layer %s parent = %v", l.Name, l.ParentID)
			}
		}
	}
	if baseMask == nil {
		t.Fatal("Base mask missing")
	}
	if childClip == nil || *childClip != baseID {
		t.Fatalf("clip source = %v, want %s", childClip, baseID)
	}
}

func TestImportedGroupsFollowPhotoshopLsctOrder(t *testing.T) {
	fill := solidBitmap(2, 2, 0, 255, 0, 255)
	composite := solidBitmap(4, 4, 0, 0, 0, 0)
	group := fixtureRecord{id: "G1", name: "Stack", isGroup: true, visible: true, opacity: 1, blendKey: "pass"}
	child := fixtureRecord{id: "B1", parentID: "G1", name: "Base", visible: true, opacity: 1, blendKey: "norm", left: 0, top: 0, width: 2, height: 2, image: fill}
	data := build(t, fixtureDoc{width: 4, height: 4, resolution: 72, layers: []fixtureRecord{group, child}}, composite)
	types := lsctTypes(t, data)
	if len(types) != 2 || types[0] != 3 || types[1] != 1 {
		t.Fatalf("lsct types = %v, want [3 1]", types)
	}
	doc := mustRead(t, data, 1<<30)
	var folder *Record
	for i := range doc.Layers {
		if doc.Layers[i].IsGroup {
			folder = &doc.Layers[i]
		}
	}
	if folder == nil || folder.Name != "Stack" {
		t.Fatal("folder missing")
	}
	for _, l := range doc.Layers {
		if l.Name == "Base" && l.ParentID != folder.ID {
			t.Fatalf("Base parent = %q, want %q", l.ParentID, folder.ID)
		}
	}
}

// lsctTypes scans the layer section for lsct payload section bytes.
func lsctTypes(t *testing.T, data []byte) []int {
	t.Helper()
	var out []int
	for i := 0; i+8 <= len(data); i++ {
		if string(data[i:i+4]) == "lsct" && i+12 <= len(data) {
			// "lsct" key (4) + length u32 (4) + payload: [0,0,0,section]
			out = append(out, int(data[i+11]))
		}
	}
	return out
}

func TestOversizedLayerBoundsAreRejected(t *testing.T) {
	fill := solidBitmap(2, 2, 255, 0, 0, 255)
	layer := fixtureRecord{id: "L1", name: "Huge", visible: true, opacity: 1, blendKey: "norm", left: 0, top: 0, width: 2, height: 2, image: fill}
	composite := solidBitmap(2, 2, 0, 0, 0, 0)
	// Declare a giant layer extent in the record while keeping a small plane.
	doc := fixtureDoc{width: 8, height: 8, resolution: 72, layers: []fixtureRecord{layer}}
	data := build(t, doc, composite)
	// Patch the record's bottom/right: the layer record starts after the
	// file header+resources; the first record's rect is right after the
	// layer section's two length fields and the count.
	patched := patchFirstLayerRect(t, data, 0, 0, 30000, 30000)
	if _, err := Read(patched, 50); err != ErrTooLarge {
		t.Fatalf("oversized layer: err = %v, want too large", err)
	}
	// Over-budget pixels (20×20 = 400 > 50) are rejected as well.
	fill20 := solidBitmap(20, 20, 255, 0, 0, 255)
	layer20 := fixtureRecord{id: "L1", name: "Huge", visible: true, opacity: 1, blendKey: "norm", left: 0, top: 0, width: 20, height: 20, image: fill20}
	data20 := build(t, fixtureDoc{width: 20, height: 20, resolution: 72, layers: []fixtureRecord{layer20}}, fill20)
	if _, err := Read(data20, 50); err != ErrTooLarge {
		t.Fatalf("over budget: err = %v, want too large", err)
	}
}

// patchFirstLayerRect finds the layer section and rewrites the first
// record's rectangle.
func patchFirstLayerRect(t *testing.T, data []byte, top, left, bottom, right int) []byte {
	t.Helper()
	out := append([]byte(nil), data...)
	// Find "8BIM" of the first layer record's blend signature preceded by
	// the rect; simpler: locate the layer-count position: header(26) +
	// color mode length(4) + resources length(4) + resources.
	offset := 26
	colorLen := int(be32(out[offset:]))
	offset += 4 + colorLen
	resLen := int(be32(out[offset:]))
	offset += 4 + resLen
	// layer section length (4) + layer info length (4) + count (2)
	offset += 4 + 4 + 2
	// rect: top, left, bottom, right
	be32Put(out[offset:], uint32(int32(top)))
	be32Put(out[offset+4:], uint32(int32(left)))
	be32Put(out[offset+8:], uint32(int32(bottom)))
	be32Put(out[offset+12:], uint32(int32(right)))
	return out
}

func be32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func be32Put(b []byte, v uint32) {
	b[0], b[1], b[2], b[3] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
}

func TestUnusedSpotChannelsAreSkippedBeforeDecode(t *testing.T) {

	pixels := []byte{255, 255, 255, 255}
	var channels []struct {
		id      int16
		payload []byte
	}
	for _, id := range []int16{-1, 0, 1, 2} {
		channels = append(channels, struct {
			id      int16
			payload []byte
		}{id, rawChannel(pixels)})
	}
	bogus := []byte{0, 99, 0, 0}
	for id := int16(3); id <= 54; id++ {
		channels = append(channels, struct {
			id      int16
			payload []byte
		}{id, bogus})
	}
	data := layerFile(2, 2, channels)
	doc := mustRead(t, data, 1<<30)
	if len(doc.Layers) != 1 || doc.Layers[0].Image == nil ||
		doc.Layers[0].Image.W != 2 || doc.Layers[0].Image.H != 2 {
		t.Fatalf("layers = %+v", doc.Layers)
	}
}

func TestUnsupportedCompressionOnColorChannelsIsStillRejected(t *testing.T) {

	pixels := []byte{255, 255, 255, 255}
	channels := []struct {
		id      int16
		payload []byte
	}{
		{-1, rawChannel(pixels)},
		{0, []byte{0, 99, 0, 0}},
		{1, rawChannel(pixels)},
		{2, rawChannel(pixels)},
	}
	{
	}
	if _, err := Read(layerFile(2, 2, channels), 1<<30); err != ErrUnsupportedCompress {
		t.Fatalf("err = %v, want unsupported compression", err)
	}
}

// layerFile builds a minimal one-layer file with explicit channels
// (PSDRoundTripTests' layerFile helper).
func layerFile(width, height int, channels []struct {
	id      int16
	payload []byte
}) []byte {
	item := preparedLayer{
		record:   fixtureRecord{name: "Layer", visible: true, opacity: 1, blendKey: "norm"},
		channels: channels,
		top:      0, left: 0, bottom: height, right: width,
	}
	records := &buf{}
	records.i16(1)
	writeFixtureRecord(records, item, false, "", nil)
	payloads := &buf{}
	for _, channel := range channels {
		payloads.raw(channel.payload)
	}
	info := &buf{}
	info.u32(0)
	info.raw(records.data)
	info.raw(payloads.data)
	if len(info.data)%2 == 1 {
		info.u8(0)
	}
	layerBytes := len(info.data) - 4
	be32Put(info.data[:4], uint32(layerBytes))

	file := &buf{}
	file.str("8BPS")
	file.u16(1)
	file.raw(make([]byte, 6))
	file.u16(4)
	file.u32(uint32(height))
	file.u32(uint32(width))
	file.u16(8)
	file.u16(3)
	file.u32(0)
	file.u32(0)                      // empty resources
	file.u32(uint32(len(info.data))) // layer section length
	file.raw(info.data)
	file.u32(0)
	file.u16(0) // no composite
	return file.data
}

func TestMatchesRequiresPhotoshopMagic(t *testing.T) {
	if Matches([]byte{0xFF, 0xD8, 0xFF, 0xE0}) {
		t.Fatal("JPEG must not match")
	}
	if !Matches([]byte("8BPS")) {
		t.Fatal("8BPS must match")
	}
}

func TestUnsupportedBlendProducesConversionReport(t *testing.T) {
	fill := solidBitmap(2, 2, 255, 0, 0, 255)
	layer := fixtureRecord{id: "L1", name: "Dissolved", visible: true, opacity: 1, blendKey: "diss", left: 0, top: 0, width: 2, height: 2, image: fill}
	imported, err := Build(mustRead(t, build(t, fixtureDoc{width: 2, height: 2, resolution: 72, layers: []fixtureRecord{layer}}, fill), 1<<30))
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Conversions) == 0 {
		t.Fatal("expected a conversion report")
	}
	found := false
	for _, c := range imported.Conversions {
		if c.LayerName == "Dissolved" && strings.Contains(c.Message, "diss") {
			found = true
		}
	}
	if !found {
		t.Fatalf("conversions = %+v", imported.Conversions)
	}
	if *imported.Document.Layers[0].BlendMode != "Normal" {
		t.Fatalf("blend = %v, want Normal", *imported.Document.Layers[0].BlendMode)
	}
}

func TestSoftLightImportsWithoutConversion(t *testing.T) {
	fill := solidBitmap(2, 2, 255, 0, 0, 255)
	layer := fixtureRecord{id: "L1", name: "Soft", visible: true, opacity: 1, blendKey: "sLit", left: 0, top: 0, width: 2, height: 2, image: fill}
	imported, err := Build(mustRead(t, build(t, fixtureDoc{width: 2, height: 2, resolution: 72, layers: []fixtureRecord{layer}}, fill), 1<<30))
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Conversions) != 0 {
		t.Fatalf("unexpected conversions: %+v", imported.Conversions)
	}
	if *imported.Document.Layers[0].BlendMode != "Soft Light" {
		t.Fatalf("blend = %v", *imported.Document.Layers[0].BlendMode)
	}
}

func TestFolderOpacityImportsOntoTheFolder(t *testing.T) {
	fill := solidBitmap(2, 2, 0, 255, 0, 255)
	group := fixtureRecord{id: "G1", name: "Stack", isGroup: true, visible: true, opacity: 0.5, blendKey: "pass"}
	child := fixtureRecord{id: "B1", parentID: "G1", name: "Base", visible: true, opacity: 1, blendKey: "norm", left: 0, top: 0, width: 2, height: 2, image: fill}
	imported, err := Build(mustRead(t, build(t, fixtureDoc{width: 4, height: 4, resolution: 72, layers: []fixtureRecord{group, child}}, fill), 1<<30))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range imported.Document.Layers {
		if l.Name == "Stack" {
			if l.Opacity == nil || *l.Opacity < 0.49 || *l.Opacity > 0.51 {
				t.Fatalf("folder opacity = %v, want ≈0.5", l.Opacity)
			}
		}
	}
	if len(imported.Conversions) != 0 {
		t.Fatalf("unexpected conversions: %+v", imported.Conversions)
	}
}

func header(version, depth, mode uint16, width, height uint32) []byte {
	file := &buf{}
	file.str("8BPS")
	file.u16(version)
	file.raw(make([]byte, 6))
	file.u16(4)
	file.u32(height)
	file.u32(width)
	file.u16(depth)
	file.u16(mode)
	file.u32(0)
	file.u32(0)
	return file.data
}

func TestUnsupportedHeadersAreRejected(t *testing.T) {
	if _, err := Read(header(3, 8, 3, 10, 10), 1<<30); err != ErrUnsupportedVersion {
		t.Fatalf("version 3: %v", err)
	}
	if _, err := Read(header(1, 8, 4, 10, 10), 1<<30); err != ErrUnsupportedColorMode {
		t.Fatalf("CMYK: %v", err)
	}
	if _, err := Read(header(1, 16, 3, 10, 10), 1<<30); err != ErrUnsupportedDepth {
		t.Fatalf("16-bit: %v", err)
	}
	if _, err := Read(header(1, 8, 3, 30001, 10), 1<<30); err != ErrTooLarge {
		t.Fatalf("oversized canvas: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PSB

func TestPSBRoundTripMatchesPSDLayerContent(t *testing.T) {
	red := solidBitmap(3, 2, 255, 0, 0, 255)
	green := solidBitmap(2, 3, 0, 255, 0, 255)
	blue := solidBitmap(2, 2, 0, 0, 255, 255)
	mask := make([]uint8, 4)
	for i := range mask {
		mask[i] = 128
	}
	composite := solidBitmap(5, 5, 0, 0, 0, 255)
	bottom := fixtureRecord{id: "L1", name: "Red", visible: true, opacity: 1, blendKey: "norm", left: 0, top: 0, width: 3, height: 2, image: red}
	middle := fixtureRecord{id: "L2", name: "Green", visible: true, opacity: 1, blendKey: "norm", left: 1, top: 1, width: 2, height: 3, image: green}
	top := fixtureRecord{id: "L3", name: "Blue mask", visible: true, opacity: 1, blendKey: "norm", left: 2, top: 2, width: 2, height: 2, image: blue, mask: mask, maskW: 2, maskH: 2}
	source := fixtureDoc{width: 5, height: 5, resolution: 144, layers: []fixtureRecord{bottom, middle, top}}

	psd := mustRead(t, build(t, source, composite), 1<<30)
	psb := mustRead(t, buildPSB(t, source, composite), 1<<30)
	if len(psb.Layers) != len(psd.Layers) {
		t.Fatalf("layer count %d vs %d", len(psb.Layers), len(psd.Layers))
	}
	for i := range psd.Layers {
		if psb.Layers[i].Name != psd.Layers[i].Name {
			t.Fatalf("name %d: %q vs %q", i, psb.Layers[i].Name, psd.Layers[i].Name)
		}
		if psb.Layers[i].Left != psd.Layers[i].Left || psb.Layers[i].Top != psd.Layers[i].Top ||
			psb.Layers[i].Width != psd.Layers[i].Width || psb.Layers[i].Height != psd.Layers[i].Height {
			t.Fatalf("bounds %d differ", i)
		}
		if !bytes.Equal(rgbaPixels(psb.Layers[i].Image), rgbaPixels(psd.Layers[i].Image)) {
			t.Fatalf("pixels %d differ", i)
		}
	}
	if !bytes.Equal(psb.Layers[2].Mask, psd.Layers[2].Mask) {
		t.Fatal("mask pixels differ")
	}
}

func TestPSBExceedingCanvasLimitIsRejected(t *testing.T) {
	image := solidBitmap(2, 2, 255, 0, 0, 255)
	layer := fixtureRecord{id: "L1", name: "Large", visible: true, opacity: 1, blendKey: "norm", left: 0, top: 0, width: 2, height: 2, image: image}
	data := buildPSB(t, fixtureDoc{width: 2, height: 2, resolution: 72, layers: []fixtureRecord{layer}}, image)
	// Canvas height at offset 14..18 (magic 4 + version 2 + reserved 6 +
	// channels 2); set it to 0x7531 = 30001 > MaxSide.
	be32Put(data[14:], 0x7531)
	if _, err := Read(data, 1<<30); err != ErrTooLarge {
		t.Fatalf("err = %v, want too large", err)
	}
}

func TestPSBLargeAdditionalInfoBlockDoesNotHideUnicodeName(t *testing.T) {
	image := solidBitmap(2, 2, 255, 0, 0, 255)
	layer := fixtureRecord{id: "L1", name: "Café layer", visible: true, opacity: 1, blendKey: "norm", left: 0, top: 0, width: 2, height: 2, image: image}
	data := fixtureData(fixtureDoc{width: 2, height: 2, resolution: 72, layers: []fixtureRecord{layer}}, image, true, "LMsk", []byte{1, 2, 3}, nil)
	doc := mustRead(t, data, 1<<30)
	if len(doc.Layers) != 1 || doc.Layers[0].Name != "Café layer" {
		t.Fatalf("layers = %+v", doc.Layers)
	}
}

// ---------------------------------------------------------------------------
// Adjustments

func shorts(values []int) []byte {
	out := []byte{}
	for _, v := range values {
		out = append(out, byte(uint16(int16(v))>>8), byte(uint16(int16(v))&0xFF))
	}
	return out
}

func TestLevelsGammaIsInHundredths(t *testing.T) {
	data := shorts([]int{2, 2, 254, 0, 255, 100})
	untouched := []int{0, 255, 0, 255, 100}
	for i := 0; i < 3; i++ {
		data = append(data, shorts(untouched)...)
	}
	for len(data) < 292 {
		data = append(data, 0)
	}
	parsed := parseLevels(data)
	if parsed == nil {
		t.Fatal("levels parse failed")
	}
	r0 := parsed.Adjustment.Levels.Ranges[0]
	if r0.Black != 2 || r0.Gamma != 1 || r0.White != 254 || r0.OutputBlack != 0 || r0.OutputWhite != 255 {
		t.Fatalf("range 0 = %+v", r0)
	}
	for ch := 1; ch <= 3; ch++ {
		if parsed.Adjustment.Levels.Ranges[ch].Gamma != 1 {
			t.Fatalf("range %d gamma = %v", ch, parsed.Adjustment.Levels.Ranges[ch].Gamma)
		}
	}
}

func TestHueSaturationReadsMasterAndEachRange(t *testing.T) {
	data := shorts([]int{2})
	data = append(data, 0, 0) // colorize off + pad
	data = append(data, shorts([]int{23, 25, 0, 5, 4, 0, 315, 345, 15, 45, 0, -30, 10})...)
	data = append(data, shorts(make([]int, 7*5))...)
	parsed := parseHue(data)
	if parsed == nil {
		t.Fatal("hue parse failed")
	}
	jsonText := string(parsed.Adjustment.HSVSettings)
	if !strings.Contains(jsonText, "\"Master\"") || !strings.Contains(jsonText, "\"Reds\"") {
		t.Fatalf("hsvSettings missing ranges: %s", jsonText)
	}
	if !strings.Contains(jsonText, "\"colorize\":false") {
		t.Fatalf("colorize should be off: %s", jsonText)
	}
	if !strings.Contains(jsonText, "\"saturation\":-30") {
		t.Fatalf("Reds saturation missing: %s", jsonText)
	}
	if !strings.Contains(jsonText, "315") || !strings.Contains(jsonText, "345") {
		t.Fatalf("Reds band missing: %s", jsonText)
	}

	colorized := append([]byte(nil), data...)
	colorized[2] = 1
	parsed2 := parseHue(colorized)
	if parsed2 == nil || !strings.Contains(string(parsed2.Adjustment.HSVSettings), "\"colorize\":true") {
		t.Fatalf("colorize on parse failed: %v", parsed2)
	}
	if !strings.Contains(string(parsed2.Adjustment.HSVSettings), "\"hue\":23") {
		t.Fatalf("colorize master values missing: %s", parsed2.Adjustment.HSVSettings)
	}
}

func TestMaskPatchSitsWhereItIsOnTheCanvas(t *testing.T) {
	// A 2×2 white patch at (3, 1) on a 6×4 canvas, default 0, on an
	// adjustment layer (covers the canvas as a whole).
	patch := make([]uint8, 4)
	for i := range patch {
		patch[i] = 255
	}
	record := Record{
		ID: "A1", Name: "Levels", Width: 6, Height: 4,
		Mask: patch, MaskWidth: 2, MaskHeight: 2,
		MaskLeft: 3, MaskTop: 1, MaskDefault: 0,
		Adjustment: &ParsedAdjustment{},
	}
	mask := maskOnLayerGrid(record, domainLayerStub(), 6, 4)
	if mask == nil || mask.W != 6 || mask.H != 4 {
		t.Fatalf("mask = %+v", mask)
	}
	rows := make([]string, 4)
	for y := 0; y < 4; y++ {
		row := ""
		for x := 0; x < 6; x++ {
			if mask.Pix[(y*6+x)*4] > 127 {
				row += "#"
			} else {
				row += "."
			}
		}
		rows[y] = row
	}
	want := []string{"......", "...##.", "...##.", "......"}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("rows = %v, want %v", rows, want)
		}
	}
}

func domainLayerStub() domain.Layer {
	// An adjustment layer covers the canvas (no pixels of its own).
	return domain.Layer{ID: "A1", Name: "Levels"}
}

// ---------------------------------------------------------------------------
// Crop to canvas

func rgbaImagePixels(width, height int) *render.Bitmap {
	bmp := render.NewBitmap(width, height)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := (y*width + x) * 4
			bmp.Pix[i] = uint8(x*30 + y)
			bmp.Pix[i+1] = uint8(y * 50)
			bmp.Pix[i+2] = uint8(255 - x*20)
			bmp.Pix[i+3] = 255
		}
	}
	return bmp
}

func grayImage(size int) []uint8 {
	out := make([]uint8, size*size)
	for i := range out {
		out[i] = uint8(i * 10)
	}
	return out
}

func cropDocument() (fixtureDoc, *render.Bitmap) {
	image := rgbaImagePixels(6, 3)
	composite := rgbaImagePixels(4, 4)
	layer := fixtureRecord{
		id: "L1", name: "Overhang", visible: true, opacity: 1, blendKey: "norm",
		left: -2, top: 1, width: 6, height: 3, image: image,
		mask: grayImage(6), maskW: 6, maskH: 3,
	}
	return fixtureDoc{width: 4, height: 4, resolution: 72, layers: []fixtureRecord{layer}}, composite
}

func TestFittingDocumentKeepsOversizedLayerPixels(t *testing.T) {
	doc, composite := cropDocument()
	parsed := mustRead(t, build(t, doc, composite), 100)
	layer := parsed.Layers[0]
	if layer.Left != -2 || layer.Top != 1 || layer.Width != 6 || layer.Height != 3 {
		t.Fatalf("bounds = %d,%d %dx%d", layer.Left, layer.Top, layer.Width, layer.Height)
	}
	if layer.CroppedToCanvas {
		t.Fatal("must not be cropped")
	}
	if !bytes.Equal(rgbaPixels(layer.Image), rgbaPixels(doc.layers[0].image)) {
		t.Fatal("pixels changed")
	}
	imported, err := Build(parsed)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range imported.Conversions {
		if strings.Contains(c.Message, "裁剪") {
			t.Fatalf("unexpected crop note: %+v", c)
		}
	}
}

func TestOverBudgetDocumentCropsImageAndMaskToCanvas(t *testing.T) {
	doc, composite := cropDocument()
	parsed := mustRead(t, build(t, doc, composite), 12)
	layer := parsed.Layers[0]
	if layer.Left != 0 || layer.Top != 1 || layer.Width != 4 || layer.Height != 3 {
		t.Fatalf("bounds = %d,%d %dx%d, want 0,1 4x3", layer.Left, layer.Top, layer.Width, layer.Height)
	}
	if !layer.CroppedToCanvas {
		t.Fatal("must be cropped")
	}
	if layer.Image.W != 4 || layer.Image.H != 3 {
		t.Fatalf("image = %dx%d", layer.Image.W, layer.Image.H)
	}
	sourcePixels := rgbaPixels(doc.layers[0].image)
	var expected []uint8
	for row := 0; row < 3; row++ {
		expected = append(expected, sourcePixels[(row*6+2)*4:(row*6+6)*4]...)
	}
	if !bytes.Equal(rgbaPixels(layer.Image), expected) {
		t.Fatal("cropped pixels mismatch")
	}
	if layer.MaskWidth != 4 || layer.MaskHeight != 3 {
		t.Fatalf("mask = %dx%d", layer.MaskWidth, layer.MaskHeight)
	}
	imported, err := Build(parsed)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range imported.Conversions {
		if strings.Contains(c.Message, "裁剪") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the crop note: %+v", imported.Conversions)
	}
}
