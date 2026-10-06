package psd

// PSDFixture ported: builds tiny Photoshop files (PSD and PSB) for reader
// tests. Not part of the app; Compositor does not write PSD.

import (
	"bytes"
	"encoding/binary"
	"math"

	"compositor-win/internal/render"
)

type fixtureRecord struct {
	id       string
	name     string
	parentID string
	isGroup  bool
	visible  bool
	opacity  float64
	blendKey string
	clipping bool
	left     int
	top      int
	width    int
	height   int
	image    *render.Bitmap
	mask     []uint8
	maskW    int
	maskH    int
}

type fixtureDoc struct {
	width      int
	height     int
	resolution float64
	layers     []fixtureRecord
}

type buf struct{ data []byte }

func (b *buf) u8(v uint8)   { b.data = append(b.data, v) }
func (b *buf) u16(v uint16) { b.data = binary.BigEndian.AppendUint16(b.data, v) }
func (b *buf) i16(v int16)  { b.u16(uint16(v)) }
func (b *buf) u32(v uint32) { b.data = binary.BigEndian.AppendUint32(b.data, v) }
func (b *buf) u64(v uint64) { b.data = binary.BigEndian.AppendUint64(b.data, v) }
func (b *buf) i32(v int32)  { b.u32(uint32(v)) }
func (b *buf) f64(v float64) {
	var tmp [8]byte
	binary.BigEndian.PutUint64(tmp[:], math.Float64bits(v))
	b.data = append(b.data, tmp[:]...)
}
func (b *buf) raw(v []byte) { b.data = append(b.data, v...) }
func (b *buf) str(v string) { b.data = append(b.data, v...) }

func solidBitmap(w, h int, r, g, bl, a uint8) *render.Bitmap {
	bmp := render.NewBitmap(w, h)
	af := float64(a) / 255
	for i := 0; i < w*h; i++ {
		bmp.Pix[i*4] = uint8(float64(r)/255*af*255 + 0.5)
		bmp.Pix[i*4+1] = uint8(float64(g)/255*af*255 + 0.5)
		bmp.Pix[i*4+2] = uint8(float64(bl)/255*af*255 + 0.5)
		bmp.Pix[i*4+3] = a
	}
	return bmp
}

// grayPlane extracts a bitmap's gray levels (masks are gray).
func grayPlane(gray []uint8, w, h int) []uint8 {
	out := make([]uint8, w*h)
	copy(out, gray[:w*h])
	return out
}

// recordPlanes: straight R/G/B/A planes from a premultiplied bitmap.
func recordPlanes(bmp *render.Bitmap) (r, g, b, a []uint8) {
	n := bmp.W * bmp.H
	r = make([]uint8, n)
	g = make([]uint8, n)
	b = make([]uint8, n)
	a = make([]uint8, n)
	for i := 0; i < n; i++ {
		alpha := bmp.Pix[i*4+3]
		a[i] = alpha
		if alpha == 0 {
			continue
		}
		r[i] = uint8(min(255, (int(bmp.Pix[i*4])*255+int(alpha)/2)/int(alpha)))
		g[i] = uint8(min(255, (int(bmp.Pix[i*4+1])*255+int(alpha)/2)/int(alpha)))
		b[i] = uint8(min(255, (int(bmp.Pix[i*4+2])*255+int(alpha)/2)/int(alpha)))
	}
	return
}

func packBits(row []uint8) []byte {
	out := []byte{}
	i := 0
	for i < len(row) {
		if i+1 < len(row) && row[i] == row[i+1] {
			run := 2
			for i+run < len(row) && row[i+run] == row[i] && run < 128 {
				run++
			}
			out = append(out, byte(int8(1-run)), row[i])
			i += run
		} else {
			start := i
			i++
			for i < len(row) && i-start < 128 {
				if i+1 < len(row) && row[i] == row[i+1] {
					break
				}
				i++
			}
			out = append(out, byte(i-start-1))
			out = append(out, row[start:i]...)
		}
	}
	return out
}

// encodeRLE returns (compression=1 payload): per-row counts + packed rows.
func encodeRLE(plane []uint8, width, height int, large bool) []byte {
	counts := []byte{}
	packed := []byte{}
	for row := 0; row < height; row++ {
		rowData := packBits(plane[row*width : (row+1)*width])
		if large {
			counts = append(counts, byte(len(rowData)>>24), byte(len(rowData)>>16))
		}
		counts = append(counts, byte(len(rowData)>>8), byte(len(rowData)))
		packed = append(packed, rowData...)
	}
	return append(counts, packed...)
}

func channelPayload(plane []uint8, width, height int, large bool) []byte {
	payload := append([]byte{0, 1}, encodeRLE(plane, width, height, large)...)
	return payload
}

func rawChannel(pixels []byte) []byte {
	return append([]byte{0, 0}, pixels...)
}

func emptyChannels() [][2]any {
	// Transparency, R, G, B — the Swift fixture's four zero-length planes.
	return [][2]any{
		{int16(-1), []byte{0, 0}}, {int16(0), []byte{0, 0}},
		{int16(1), []byte{0, 0}}, {int16(2), []byte{0, 0}},
	}
}

type preparedLayer struct {
	record   fixtureRecord
	divider  bool
	channels []struct {
		id      int16
		payload []byte
	}
	top, left, bottom, right int
	maskTop, maskLeft        int
	maskBottom, maskRight    int
	extras                   map[string][]byte
}

func fixtureLayer(rec fixtureRecord, large bool, extras map[string][]byte) preparedLayer {
	width, height := 0, 0
	if rec.image != nil {
		width, height = rec.image.W, rec.image.H
	}
	var channels []struct {
		id      int16
		payload []byte
	}
	if rec.image != nil && width > 0 && height > 0 {
		r, g, b, a := recordPlanes(rec.image)
		for _, ch := range []struct {
			id    int16
			plane []uint8
		}{{-1, a}, {0, r}, {1, g}, {2, b}} {
			channels = append(channels, struct {
				id      int16
				payload []byte
			}{ch.id, channelPayload(ch.plane, width, height, large)})
		}
	} else {
		for _, p := range emptyChannels() {
			channels = append(channels, struct {
				id      int16
				payload []byte
			}{p[0].(int16), p[1].([]byte)})
		}
	}
	maskBottom, maskRight := 0, 0
	if rec.mask != nil {
		plane := grayPlane(rec.mask, rec.maskW, rec.maskH)
		channels = append(channels, struct {
			id      int16
			payload []byte
		}{-2, channelPayload(plane, rec.maskW, rec.maskH, large)})
		maskBottom, maskRight = rec.maskH, rec.maskW
	}
	return preparedLayer{
		record: rec, channels: channels,
		top: rec.top, left: rec.left, bottom: rec.top + height, right: rec.left + width,
		maskTop: rec.top, maskLeft: rec.left, maskBottom: maskBottom + rec.top, maskRight: maskRight + rec.left,
		extras: extras,
	}
}

func fixtureEmptyLayer(rec fixtureRecord, section int, large bool) preparedLayer {
	var channels []struct {
		id      int16
		payload []byte
	}
	for _, p := range emptyChannels() {
		channels = append(channels, struct {
			id      int16
			payload []byte
		}{p[0].(int16), p[1].([]byte)})
	}
	maskBottom, maskRight := 0, 0
	if rec.mask != nil {
		plane := grayPlane(rec.mask, rec.maskW, rec.maskH)
		channels = append(channels, struct {
			id      int16
			payload []byte
		}{-2, channelPayload(plane, rec.maskW, rec.maskH, large)})
		maskBottom, maskRight = rec.maskH, rec.maskW
	}
	return preparedLayer{
		record: rec, divider: section == 3, channels: channels,
		maskBottom: maskBottom, maskRight: maskRight,
	}
}

// fixtureData builds the file (PSDFixture.data). additionalLayerInfo/key
// and extras mirror the Swift helper.
func fixtureData(doc fixtureDoc, composite *render.Bitmap, large bool, additionalKey string, additionalPayload []byte, extras map[string]map[string][]byte) []byte {
	file := &buf{}
	file.str("8BPS")
	if large {
		file.u16(2)
	} else {
		file.u16(1)
	}
	file.raw(make([]byte, 6))
	file.u16(4)
	file.u32(uint32(doc.height))
	file.u32(uint32(doc.width))
	file.u16(8)
	file.u16(3)
	file.u32(0)
	resources := resolutionResource(doc.resolution)
	file.u32(uint32(len(resources)))
	file.raw(resources)
	layers := fixtureLayerSection(doc, large, additionalKey, additionalPayload, extras)
	if large {
		file.u64(uint64(len(layers)))
	} else {
		file.u32(uint32(len(layers)))
	}
	file.raw(layers)
	appendComposite(file, composite, doc.width, doc.height, large)
	return file.data
}

func fixtureLayerSection(doc fixtureDoc, large bool, additionalKey string, additionalPayload []byte, extras map[string]map[string][]byte) []byte {
	var prepared []preparedLayer
	var emit func(parent string)
	emit = func(parent string) {
		// File order is bottom-to-top; groups are type 3, children, then 1/2.
		for _, rec := range doc.layers {
			if rec.parentID != parent {
				continue
			}
			if rec.isGroup {
				divider := fixtureRecord{id: newID(), name: "</Layer group>", blendKey: "norm", isGroup: true, visible: true, opacity: 1}
				prepared = append(prepared, fixtureEmptyLayer(divider, 3, large))
				emit(rec.id)
				group := rec
				if group.blendKey == "" {
					group.blendKey = "norm"
				}
				prepared = append(prepared, fixtureEmptyLayer(group, 1, large))
			} else {
				prepared = append(prepared, fixtureLayer(rec, large, extras[rec.id]))
			}
		}
	}
	emit("")
	records := &buf{}
	records.i16(int16(len(prepared)))
	payloads := &buf{}
	for _, item := range prepared {
		writeFixtureRecord(records, item, large, additionalKey, additionalPayload)
		for _, channel := range item.channels {
			payloads.raw(channel.payload)
		}
	}
	info := &buf{}
	if large {
		info.u64(0)
	} else {
		info.u32(0)
	}
	info.raw(records.data)
	info.raw(payloads.data)
	if len(info.data)%2 == 1 {
		info.u8(0)
	}
	lengthFieldBytes := 4
	if large {
		lengthFieldBytes = 8
	}
	layerBytes := len(info.data) - lengthFieldBytes
	var length []byte
	if large {
		length = binary.BigEndian.AppendUint64(nil, uint64(layerBytes))
	} else {
		length = binary.BigEndian.AppendUint32(nil, uint32(layerBytes))
	}
	copy(info.data[:lengthFieldBytes], length)
	section := &buf{}
	section.raw(info.data)
	section.u32(0)
	return section.data
}

func writeFixtureRecord(out *buf, item preparedLayer, large bool, additionalKey string, additionalPayload []byte) {
	rec := item.record
	out.i32(int32(item.top))
	out.i32(int32(item.left))
	out.i32(int32(item.bottom))
	out.i32(int32(item.right))
	out.u16(uint16(len(item.channels)))
	for _, channel := range item.channels {
		out.i16(channel.id)
		if large {
			out.u64(uint64(len(channel.payload)))
		} else {
			out.u32(uint32(len(channel.payload)))
		}
	}
	out.str("8BIM")
	blend := rec.blendKey + "    "
	if blend == "    " {
		blend = "norm    "
	}
	out.str(blend[:4])
	out.u8(uint8(math.Round(clamp01(rec.opacity) * 255)))
	if rec.clipping {
		out.u8(1)
	} else {
		out.u8(0)
	}
	if rec.visible {
		out.u8(0)
	} else {
		out.u8(2)
	}
	out.u8(0)
	extra := fixtureExtra(item, large, additionalKey, additionalPayload)
	out.u32(uint32(len(extra.data)))
	out.raw(extra.data)
}

func clamp01(v float64) float64 { return math.Min(1, math.Max(0, v)) }

func fixtureExtra(item preparedLayer, large bool, additionalKey string, additionalPayload []byte) *buf {
	extra := &buf{}
	if item.record.mask != nil && item.maskRight > item.maskLeft && item.maskBottom > item.maskTop {
		extra.u32(20)
		extra.i32(int32(item.maskTop))
		extra.i32(int32(item.maskLeft))
		extra.i32(int32(item.maskBottom))
		extra.i32(int32(item.maskRight))
		extra.u8(255)
		var flags uint8
		if !item.record.maskLinkedFixture() {
			flags |= 1
		}
		extra.u8(flags)
		extra.u16(0)
	} else {
		extra.u32(0)
	}
	extra.u32(0)
	name := item.record.name
	if len(name) > 255 {
		name = name[:255]
	}
	extra.u8(uint8(len(name)))
	extra.str(name)
	pascal := 1 + len(name)
	pad := (4 - (pascal % 4)) % 4
	extra.raw(make([]byte, pad))
	if additionalKey != "" {
		writeFixtureAdditional(extra, additionalKey, additionalPayload, large)
	}
	writeFixtureAdditional(extra, "luni", luni(item.record.name), large)
	if item.record.isGroup || item.divider {
		section := uint32(1)
		if item.divider {
			section = 3
		}
		payload := []byte{0, 0, 0, byte(section)}
		payload = append(payload, []byte("8BIM")...)
		blend := item.record.blendKey
		if item.divider {
			blend = "norm"
		}
		if blend == "" {
			blend = "norm"
		}
		blendPadded := blend + "    "
		payload = append(payload, []byte(blendPadded[:4])...)
		writeFixtureAdditional(extra, "lsct", payload, large)
	}
	for _, key := range sortedKeys(item.extras) {
		writeFixtureAdditional(extra, key, item.extras[key], large)
	}
	return extra
}

func (r fixtureRecord) maskLinkedFixture() bool { return true }

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func writeFixtureAdditional(out *buf, key string, payload []byte, large bool) {
	out.str("8BIM")
	out.str(key)
	if large && psbLargeAdditionalInfoKeys[key] {
		out.u64(uint64(len(payload)))
	} else {
		out.u32(uint32(len(payload)))
	}
	out.raw(payload)
	if len(payload)%2 == 1 {
		out.u8(0)
	}
}

func luni(name string) []byte {
	units := []uint16{}
	for _, r := range name {
		if r > 0xFFFF {
			r1 := 0xD800 + ((r - 0x10000) >> 10)
			r2 := 0xDC00 + ((r - 0x10000) & 0x3FF)
			units = append(units, uint16(r1), uint16(r2))
		} else {
			units = append(units, uint16(r))
		}
	}
	out := &buf{}
	out.u32(uint32(len(units)))
	for _, u := range units {
		out.u16(u)
	}
	return out.data
}

func resolutionResource(resolution float64) []byte {
	res := &buf{}
	res.str("8BIM")
	res.u16(1005)
	res.u8(0)
	res.u8(0)
	res.u32(16)
	fixed := uint32(math.Round(math.Min(9600, math.Max(1, resolution)) * 65536))
	res.u32(fixed)
	res.u16(1)
	res.u16(1)
	res.u32(fixed)
	res.u16(1)
	res.u16(1)
	return res.data
}

func appendComposite(file *buf, image *render.Bitmap, width, height int, large bool) {
	// Draw the composite into the canvas-sized grid (BrushRaster.draw
	// stretches it): nearest-neighbor is exact for the fixtures' flat fills.
	canvas := render.NewBitmap(width, height)
	for y := 0; y < height; y++ {
		sy := min(image.H-1, y*image.H/height)
		for x := 0; x < width; x++ {
			sx := min(image.W-1, x*image.W/width)
			si := (sy*image.W + sx) * 4
			oi := (y*width + x) * 4
			copy(canvas.Pix[oi:oi+4], image.Pix[si:si+4])
		}
	}
	r, g, b, a := recordPlanes(canvas)
	planes := [][]uint8{r, g, b, a}
	file.u16(1)
	counts := []byte{}
	packed := []byte{}
	for _, plane := range planes {
		encoded := encodeRLE(plane, width, height, large)
		countBytes := height * 2
		if large {
			countBytes = height * 4
		}
		counts = append(counts, encoded[:countBytes]...)
		packed = append(packed, encoded[countBytes:]...)
	}
	file.raw(counts)
	file.raw(packed)
}

var _ = bytes.Equal
