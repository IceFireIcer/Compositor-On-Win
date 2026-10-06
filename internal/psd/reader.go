package psd

// The reader (PSDReader.swift): file header, image resources, layer records,
// channel decode, and the bottom-to-top assembly with group dividers.

import (
	crand "crypto/rand"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
)

type cursor struct {
	data   []byte
	offset int
}

func (c *cursor) need(count int) bool {
	return c.offset >= 0 && c.offset+count <= len(c.data)
}

func (c *cursor) skip(count int) error {
	if count < 0 || !c.need(count) {
		return ErrTruncated
	}
	c.offset += count
	return nil
}

func (c *cursor) u8() (uint8, error) {
	if !c.need(1) {
		return 0, ErrTruncated
	}
	v := c.data[c.offset]
	c.offset++
	return v, nil
}

func (c *cursor) u16() (uint16, error) {
	if !c.need(2) {
		return 0, ErrTruncated
	}
	v := uint16(c.data[c.offset])<<8 | uint16(c.data[c.offset+1])
	c.offset += 2
	return v, nil
}

func (c *cursor) i16() (int16, error) {
	v, err := c.u16()
	return int16(v), err
}

func (c *cursor) u32() (uint32, error) {
	if !c.need(4) {
		return 0, ErrTruncated
	}
	v := uint32(c.data[c.offset])<<24 | uint32(c.data[c.offset+1])<<16 |
		uint32(c.data[c.offset+2])<<8 | uint32(c.data[c.offset+3])
	c.offset += 4
	return v, nil
}

func (c *cursor) u64() (uint64, error) {
	if !c.need(8) {
		return 0, ErrTruncated
	}
	var v uint64
	for i := 0; i < 8; i++ {
		v = v<<8 | uint64(c.data[c.offset+i])
	}
	c.offset += 8
	return v, nil
}

func (c *cursor) i32() (int32, error) {
	v, err := c.u32()
	return int32(v), err
}

func (c *cursor) bytes(count int) ([]byte, error) {
	if !c.need(count) {
		return nil, ErrTruncated
	}
	out := c.data[c.offset : c.offset+count]
	c.offset += count
	return out, nil
}

func (c *cursor) string4() (string, error) {
	b, err := c.bytes(4)
	if err != nil {
		return "", err
	}
	for _, v := range b {
		if v < 32 || v > 126 {
			return "", ErrTruncated
		}
	}
	return string(b), nil
}

type rawLayer struct {
	name                 string
	top, left            int
	bottom, right        int
	sourceTop, srcLeft   int
	sourceBottom, srcRt  int
	opacity              uint8
	fill                 uint8
	clipping             bool
	hidden               bool
	blendKey             string
	channels             []channelRef
	extra                map[string][]byte
	maskTop, maskLeft    int
	maskBottom, maskRt   int
	srcMaskTop, srcMaskL int
	srcMaskB, srcMaskR   int
	maskDefault          uint8
	maskDisabled         bool
	maskLinked           bool
	maskFromRender       bool
	hasMask              bool
	section              int
	image                *render.Bitmap
	maskImage            []uint8
	imageCrop            psdCrop
	maskCrop             psdCrop
	cropped              bool
}

type channelRef struct {
	id     int
	length int
}

type psdCrop struct{ x, y, width, height int }

var psbLargeAdditionalInfoKeys = map[string]bool{
	"LMsk": true, "Lr16": true, "Lr32": true, "Layr": true, "Mt16": true,
	"Mt32": true, "Mtrn": true, "Alph": true, "FMsk": true, "lnk2": true,
	"FEid": true, "FXid": true, "PxSD": true,
}

// UnpackedChannelIDs: transparency, R, G, B and the user mask; spot and
// other extra IDs are skipped before decode.
var unpackedChannelIDs = map[int]bool{-1: true, 0: true, 1: true, 2: true, -2: true}

// Read parses a Photoshop file (PSDReader.read). remainingPixels bounds the
// pixel budget; layers that overshoot it are cropped to the canvas before
// being rejected.
func Read(data []byte, remainingPixels int) (Document, error) {
	var doc Document
	c := &cursor{data: data}
	if magic, err := c.bytes(4); err != nil || string(magic) != "8BPS" {
		return doc, ErrNotPhotoshop
	}
	version, err := c.u16()
	if err != nil {
		return doc, err
	}
	if version != 1 && version != 2 {
		return doc, ErrUnsupportedVersion
	}
	isPSB := version == 2
	if err := c.skip(6); err != nil {
		return doc, err
	}
	if _, err := c.u16(); err != nil { // channels of the composite
		return doc, err
	}
	canvasHeight64, err := c.u32()
	if err != nil {
		return doc, err
	}
	canvasWidth64, err := c.u32()
	if err != nil {
		return doc, err
	}
	depth, err := c.u16()
	if err != nil {
		return doc, err
	}
	mode, err := c.u16()
	if err != nil {
		return doc, err
	}
	canvasWidth, canvasHeight := int(canvasWidth64), int(canvasHeight64)
	if canvasWidth < 1 || canvasWidth > domain.MaxSide || canvasHeight < 1 ||
		canvasHeight > domain.MaxSide || canvasWidth*canvasHeight > domain.MaxSurfacePixels {
		return doc, ErrTooLarge
	}
	if depth != 8 {
		return doc, ErrUnsupportedDepth
	}
	if mode != 3 {
		return doc, ErrUnsupportedColorMode
	}
	if skipLen, err := c.u32(); err != nil {
		return doc, err
	} else if err := c.skip(int(skipLen)); err != nil { // color mode data
		return doc, err
	}
	resourcesLength64, err := c.u32()
	if err != nil {
		return doc, err
	}
	resourcesEnd := c.offset + int(resourcesLength64)
	resolution := 72.0
	for c.offset+12 <= resourcesEnd {
		signature, err := c.string4()
		if err != nil {
			return doc, err
		}
		if signature != "8BIM" {
			break
		}
		if _, err := c.u16(); err != nil { // resource id
			return doc, err
		}
		nameLength64, err := c.u8()
		if err != nil {
			return doc, err
		}
		nameLength := int(nameLength64)
		if err := c.skip(nameLength); err != nil {
			return doc, err
		}
		if (nameLength+1)%2 == 1 {
			if err := c.skip(1); err != nil {
				return doc, err
			}
		}
		length, err := c.u32()
		if err != nil {
			return doc, err
		}
		dataStart := c.offset
		if int(length) >= 4 && c.need(4) {
			// Resource 1005 is the resolution (16.16 fixed point).
			if read16, err := c.u16(); err == nil {
				_ = read16
				c.offset -= 2
				hi, err2 := c.u32()
				if err2 == nil && int(length) >= 4 {
					r := float64(hi) / 65536
					if r != r || r < 1 {
						r = 72
					}
					resolution = min(9600, max(1, r))
				}
			}
		}
		c.offset = dataStart + int(length)
		if length%2 == 1 {
			if err := c.skip(1); err != nil {
				return doc, err
			}
		}
	}
	c.offset = resourcesEnd

	layerSection, err := checkedLength(c, isPSB)
	if err != nil {
		return doc, err
	}
	layerSectionEnd := c.offset + layerSection
	if layerSection < 4 {
		return Document{Width: canvasWidth, Height: canvasHeight, Resolution: resolution, Layers: nil}, nil
	}
	if _, err := checkedLength(c, isPSB); err != nil { // layer info length
		return doc, err
	}
	rawCount, err := c.i16()
	if err != nil {
		return doc, err
	}
	count := int(rawCount)
	if count < 0 {
		count = -count
	}
	if count > 10_000 {
		return doc, ErrTooLarge
	}
	raw := make([]rawLayer, 0, count)
	for i := 0; i < count; i++ {
		rec, err := readRecord(c, isPSB)
		if err != nil {
			return doc, err
		}
		raw = append(raw, rec)
	}
	if !fitsBudget(raw, remainingPixels) {
		for i := range raw {
			cropToCanvas(&raw[i], canvasWidth, canvasHeight)
		}
		if !fitsBudget(raw, remainingPixels) {
			return doc, ErrTooLarge
		}
	}
	usedPixels := 0
	for i := range raw {
		if err := decodeChannels(c, &raw[i], remainingPixels-usedPixels, isPSB); err != nil {
			return doc, err
		}
		if raw[i].image != nil {
			usedPixels += raw[i].image.W * raw[i].image.H
		}
	}
	c.offset = layerSectionEnd
	return assemble(raw, canvasWidth, canvasHeight, resolution, remainingPixels-usedPixels)
}

func checkedLength(c *cursor, isPSB bool) (int, error) {
	var v uint64
	var err error
	if isPSB {
		v, err = c.u64()
	} else {
		var v32 uint32
		v32, err = c.u32()
		v = uint64(v32)
	}
	if err != nil {
		return 0, err
	}
	if v > uint64(maxInt) {
		return 0, ErrTooLarge
	}
	return int(v), nil
}

const maxInt = int(^uint(0) >> 1)

func readRecord(c *cursor, isPSB bool) (rawLayer, error) {
	// Swift defaults: full fill and an opaque mask default.
	layer := rawLayer{fill: 255, maskDefault: 255}
	var err error
	if layer.top, err = i32(c); err != nil {
		return layer, err
	}
	if layer.left, err = i32(c); err != nil {
		return layer, err
	}
	if layer.bottom, err = i32(c); err != nil {
		return layer, err
	}
	if layer.right, err = i32(c); err != nil {
		return layer, err
	}
	layer.sourceTop, layer.srcLeft = layer.top, layer.left
	layer.sourceBottom, layer.srcRt = layer.bottom, layer.right
	channelCount, err := c.u16()
	if err != nil {
		return layer, err
	}
	if channelCount > 56 {
		return layer, ErrTooLarge
	}
	for i := 0; i < int(channelCount); i++ {
		id, err := c.i16()
		if err != nil {
			return layer, err
		}
		length, err := checkedLength(c, isPSB)
		if err != nil {
			return layer, err
		}
		layer.channels = append(layer.channels, channelRef{id: int(id), length: length})
	}
	sig, err := c.string4()
	if err != nil {
		return layer, err
	}
	if sig != "8BIM" {
		return layer, ErrTruncated
	}
	if layer.blendKey, err = c.string4(); err != nil {
		return layer, err
	}
	if layer.opacity, err = c.u8(); err != nil {
		return layer, err
	}
	clipping, err := c.u8()
	if err != nil {
		return layer, err
	}
	layer.clipping = clipping != 0
	flags, err := c.u8()
	if err != nil {
		return layer, err
	}
	layer.hidden = flags&2 != 0
	if err := c.skip(1); err != nil {
		return layer, err
	}
	extraLength64, err := c.u32()
	if err != nil {
		return layer, err
	}
	extraEnd := c.offset + int(extraLength64)
	maskLength64, err := c.u32()
	if err != nil {
		return layer, err
	}
	maskEnd := c.offset + int(maskLength64)
	if maskLength64 >= 20 {
		layer.hasMask = true
		if layer.maskTop, err = i32(c); err != nil {
			return layer, err
		}
		if layer.maskLeft, err = i32(c); err != nil {
			return layer, err
		}
		if layer.maskBottom, err = i32(c); err != nil {
			return layer, err
		}
		if layer.maskRt, err = i32(c); err != nil {
			return layer, err
		}
		layer.srcMaskTop, layer.srcMaskL = layer.maskTop, layer.maskLeft
		layer.srcMaskB, layer.srcMaskR = layer.maskBottom, layer.maskRt
		if layer.maskDefault, err = c.u8(); err != nil {
			return layer, err
		}
		maskFlags, err := c.u8()
		if err != nil {
			return layer, err
		}
		layer.maskDisabled = maskFlags&2 != 0
		layer.maskLinked = maskFlags&1 == 0
		layer.maskFromRender = maskFlags&8 != 0
	}
	c.offset = maskEnd
	ranges, err := c.u32()
	if err != nil {
		return layer, err
	}
	if err := c.skip(int(ranges)); err != nil {
		return layer, err
	}
	nameCount64, err := c.u8()
	if err != nil {
		return layer, err
	}
	nameCount := int(nameCount64)
	nameBytes, err := c.bytes(nameCount)
	if err != nil {
		return layer, err
	}
	layer.name = pascalName(nameBytes)
	namePad := (4 - ((nameCount + 1) % 4)) % 4
	if err := c.skip(namePad); err != nil {
		return layer, err
	}
	layer.extra = map[string][]byte{}
	for c.offset+12 <= extraEnd {
		signature, err := c.string4()
		if err != nil {
			return layer, err
		}
		if signature != "8BIM" && signature != "8B64" {
			break
		}
		key, err := c.string4()
		if err != nil {
			return layer, err
		}
		var length int
		if signature == "8B64" || (isPSB && psbLargeAdditionalInfoKeys[key]) {
			if !c.need(8) {
				break
			}
			var v uint64
			v, err = c.u64()
			if v > uint64(maxInt) {
				return layer, ErrTooLarge
			}
			length = int(v)
		} else {
			var v uint32
			v, err = c.u32()
			length = int(v)
		}
		if err != nil {
			return layer, err
		}
		payload, err := c.bytes(length)
		if err != nil {
			return layer, err
		}
		if length%2 == 1 {
			if err := c.skip(1); err != nil {
				return layer, err
			}
		}
		layer.extra[key] = payload
		if key == "luni" {
			if unicode := unicodeName(payload); unicode != "" {
				layer.name = unicode
			}
		}
		if key == "iOpa" && len(payload) > 0 {
			layer.fill = payload[0]
		}
		if (key == "lsct" || key == "lsdk") && len(payload) >= 4 {
			layer.section = int(uint32(payload[0])<<24 | uint32(payload[1])<<16 |
				uint32(payload[2])<<8 | uint32(payload[3]))
		}
	}
	c.offset = extraEnd
	return layer, nil
}

// pascalName decodes the Pascal name: macOS Roman via direct bytes, falling
// back to a UTF-8 pass (Go strings are byte sequences; Photoshop's Roman
// high bytes map onto the same single bytes).
func pascalName(b []byte) string {
	out := make([]byte, len(b))
	copy(out, b)
	return string(out)
}

func unicodeName(data []byte) string {
	if len(data) < 4 {
		return ""
	}
	count := int(uint32(data[0])<<24 | uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3]))
	if count <= 0 || len(data) < 4+count*2 {
		return ""
	}
	units := make([]uint16, count)
	for i := 0; i < count; i++ {
		units[i] = uint16(data[4+i*2])<<8 | uint16(data[5+i*2])
	}
	// UTF-16 → UTF-8, trimming trailing NULs.
	runes := make([]rune, 0, count)
	for i := 0; i < count; i++ {
		u := units[i]
		if u >= 0xD800 && u < 0xDC00 && i+1 < count {
			lo := units[i+1]
			if lo >= 0xDC00 && lo < 0xE000 {
				runes = append(runes, rune(0x10000+(uint32(u)-0xD800)<<10+(uint32(lo)-0xDC00)))
				i++
				continue
			}
		}
		runes = append(runes, rune(u))
	}
	out := ""
	for _, r := range runes {
		if r != 0 {
			out += string(r)
		}
	}
	return out
}

func fitsBudget(layers []rawLayer, remainingPixels int) bool {
	usedPixels := 0
	for _, layer := range layers {
		width := max(0, layer.right-layer.left)
		height := max(0, layer.bottom-layer.top)
		maskWidth := max(0, layer.maskRt-layer.maskLeft)
		maskHeight := max(0, layer.maskBottom-layer.maskTop)
		if !fitsBudgetOne(width, height, maskWidth, maskHeight, layer.hasMask, remainingPixels-usedPixels) {
			return false
		}
		if width > 0 && height > 0 {
			usedPixels += width * height
		}
	}
	return true
}

func fitsBudgetOne(width, height, maskWidth, maskHeight int, hasMask bool, remainingPixels int) bool {
	budget := max(0, remainingPixels)
	if width > 0 && height > 0 &&
		!(width <= domain.MaxSide && height <= domain.MaxSide && width*height <= budget) {
		return false
	}
	if hasMask && maskWidth > 0 && maskHeight > 0 &&
		!(maskWidth <= domain.MaxSide && maskHeight <= domain.MaxSide && maskWidth*maskHeight <= budget) {
		return false
	}
	return true
}

func cropToCanvas(layer *rawLayer, canvasWidth, canvasHeight int) {
	imageCrop := cropRect(layer.left, layer.top, layer.right, layer.bottom, canvasWidth, canvasHeight)
	if imageCrop.x != 0 || imageCrop.y != 0 || imageCrop.width != layer.right-layer.left || imageCrop.height != layer.bottom-layer.top {
		layer.left += imageCrop.x
		layer.top += imageCrop.y
		layer.right = layer.left + imageCrop.width
		layer.bottom = layer.top + imageCrop.height
		layer.imageCrop = imageCrop
		layer.cropped = true
	}
	if !layer.hasMask {
		return
	}
	maskCrop := cropRect(layer.maskLeft, layer.maskTop, layer.maskRt, layer.maskBottom, canvasWidth, canvasHeight)
	if maskCrop.x != 0 || maskCrop.y != 0 || maskCrop.width != layer.maskRt-layer.maskLeft || maskCrop.height != layer.maskBottom-layer.maskTop {
		layer.maskLeft += maskCrop.x
		layer.maskTop += maskCrop.y
		layer.maskRt = layer.maskLeft + maskCrop.width
		layer.maskBottom = layer.maskTop + maskCrop.height
		layer.maskCrop = maskCrop
		layer.cropped = true
	}
}

func cropRect(left, top, right, bottom, canvasWidth, canvasHeight int) psdCrop {
	cl := min(canvasWidth, max(0, left))
	ct := min(canvasHeight, max(0, top))
	cr := max(cl, min(canvasWidth, right))
	cb := max(ct, min(canvasHeight, bottom))
	return psdCrop{x: cl - left, y: ct - top, width: cr - cl, height: cb - ct}
}

func decodeChannels(c *cursor, layer *rawLayer, remainingPixels int, isPSB bool) error {
	planes := map[int][]uint8{}
	width := max(0, layer.right-layer.left)
	height := max(0, layer.bottom-layer.top)
	maskWidth := max(0, layer.maskRt-layer.maskLeft)
	maskHeight := max(0, layer.maskBottom-layer.maskTop)
	if !fitsBudgetOne(width, height, maskWidth, maskHeight, layer.hasMask, remainingPixels) {
		return ErrTooLarge
	}
	sourceWidth := max(0, layer.srcRt-layer.srcLeft)
	sourceHeight := max(0, layer.sourceBottom-layer.sourceTop)
	sourceMaskWidth := max(0, layer.srcMaskR-layer.srcMaskL)
	sourceMaskHeight := max(0, layer.srcMaskB-layer.srcMaskTop)
	for _, channel := range layer.channels {
		start := c.offset
		// Advance past this channel before the next iteration — Go defers
		// run at function exit, unlike Swift's per-iteration defer here.
		advance := func() { c.offset = start + max(0, channel.length) }
		if !unpackedChannelIDs[channel.id] || channel.length < 2 {
			advance()
			continue
		}
		compression, err := c.u16()
		if err != nil {
			return err
		}
		payload, err := c.bytes(channel.length - 2)
		if err != nil {
			return err
		}
		advance()
		isMask := channel.id == -2
		sourceW, sourceH, targetW, targetH := sourceWidth, sourceHeight, width, height
		var crop *psdCrop
		if layer.imageCrop.width != 0 || layer.imageCrop.height != 0 || layer.cropped {
			crop = &layer.imageCrop
		}
		if isMask {
			sourceW, sourceH, targetW, targetH = sourceMaskWidth, sourceMaskHeight, maskWidth, maskHeight
			if layer.maskCrop.width != 0 || layer.maskCrop.height != 0 || layer.cropped {
				crop = &layer.maskCrop
			}
		}
		if targetW > 0 && targetH > 0 {
			plane, err := channelDecode(int(compression), sourceW, sourceH, payload, isPSB, crop)
			if err != nil {
				return err
			}
			planes[channel.id] = plane
		}
	}
	if layer.hasMask && maskWidth > 0 && maskHeight > 0 {
		if gray, ok := planes[-2]; ok && len(gray) >= maskWidth*maskHeight {
			layer.maskImage = gray[:maskWidth*maskHeight]
		}
	}
	if width <= 0 || height <= 0 {
		return nil
	}
	opaque := make([]uint8, width*height)
	for i := range opaque {
		opaque[i] = 255
	}
	black := make([]uint8, width*height)
	red := orPlane(planes, 0, black)
	green := orPlane(planes, 1, black)
	blue := orPlane(planes, 2, black)
	alpha := orPlane(planes, -1, opaque)
	if len(red) < width*height || len(green) < width*height || len(blue) < width*height || len(alpha) < width*height {
		return ErrTruncated
	}
	bmp := render.NewBitmap(width, height)
	for i := 0; i < width*height; i++ {
		a := alpha[i]
		bmp.Pix[i*4] = uint8((uint16(red[i])*uint16(a) + 127) / 255)
		bmp.Pix[i*4+1] = uint8((uint16(green[i])*uint16(a) + 127) / 255)
		bmp.Pix[i*4+2] = uint8((uint16(blue[i])*uint16(a) + 127) / 255)
		bmp.Pix[i*4+3] = a
	}
	layer.image = bmp
	return nil
}

func orPlane(planes map[int][]uint8, id int, fallback []uint8) []uint8 {
	if p, ok := planes[id]; ok {
		return p
	}
	return fallback
}

func i32(c *cursor) (int, error) {
	v, err := c.i32()
	return int(v), err
}

// channelDecode ports PSDChannelCoder.decode: compression 0 raw and 1
// PackBits, with the optional crop applied during unpacking.
func channelDecode(compression int, width, height int, data []byte, largeDocument bool, crop *psdCrop) ([]uint8, error) {
	if width <= 0 || height <= 0 {
		return []uint8{}, nil
	}
	if crop == nil {
		expected := width * height
		switch compression {
		case 0:
			if len(data) < expected {
				return nil, ErrTruncated
			}
			return data[:expected], nil
		case 1:
			return unpackRLE(width, height, data, largeDocument, nil)
		default:
			return nil, ErrUnsupportedCompress
		}
	}
	if crop.x < 0 || crop.y < 0 || crop.width < 0 || crop.height < 0 ||
		crop.x+crop.width > width || crop.y+crop.height > height {
		return nil, ErrTruncated
	}
	if crop.width <= 0 || crop.height <= 0 {
		return []uint8{}, nil
	}
	switch compression {
	case 0:
		expected := width * height
		if len(data) < expected {
			return nil, ErrTruncated
		}
		plane := make([]uint8, crop.width*crop.height)
		for row := 0; row < crop.height; row++ {
			sourceStart := (crop.y+row)*width + crop.x
			copy(plane[row*crop.width:(row+1)*crop.width], data[sourceStart:sourceStart+crop.width])
		}
		return plane, nil
	case 1:
		return unpackRLE(width, height, data, largeDocument, crop)
	default:
		return nil, ErrUnsupportedCompress
	}
}

// unpackRLE ports PackBits: per-row byte counts (2 bytes, 4 in PSB), then
// literal (0..127: n+1 bytes) and run (−127..−1: 1−n copies) packets; −128
// is a no-op. When crop is set only the cropped rows are materialized.
func unpackRLE(width, height int, data []byte, largeDocument bool, crop *psdCrop) ([]uint8, error) {
	offset := 0
	next := func() (uint8, error) {
		if offset >= len(data) {
			return 0, ErrTruncated
		}
		v := data[offset]
		offset++
		return v, nil
	}
	counts := make([]int, height)
	for row := 0; row < height; row++ {
		if largeDocument {
			a, err := next()
			if err != nil {
				return nil, err
			}
			b, err := next()
			if err != nil {
				return nil, err
			}
			cc, err := next()
			if err != nil {
				return nil, err
			}
			d, err := next()
			if err != nil {
				return nil, err
			}
			counts[row] = int(uint32(a)<<24 | uint32(b)<<16 | uint32(cc)<<8 | uint32(d))
		} else {
			hi, err := next()
			if err != nil {
				return nil, err
			}
			lo, err := next()
			if err != nil {
				return nil, err
			}
			counts[row] = int(hi)<<8 | int(lo)
		}
	}
	if crop == nil {
		plane := make([]uint8, width*height)
		for row := 0; row < height; row++ {
			end := offset + counts[row]
			if end > len(data) {
				return nil, ErrTruncated
			}
			written := 0
			for written < width {
				if offset >= end {
					return nil, ErrTruncated
				}
				n := int8(data[offset])
				offset++
				if n >= 0 {
					count := int(n) + 1
					if written+count > width || offset+count > end {
						return nil, ErrTruncated
					}
					copy(plane[row*width+written:row*width+written+count], data[offset:offset+count])
					offset += count
					written += count
				} else if n != -128 {
					count := 1 - int(n)
					if written+count > width || offset >= end {
						return nil, ErrTruncated
					}
					value := data[offset]
					offset++
					for i := 0; i < count; i++ {
						plane[row*width+written+i] = value
					}
					written += count
				}
			}
			offset = end
		}
		return plane, nil
	}
	plane := make([]uint8, crop.width*crop.height)
	rowBuffer := make([]uint8, width)
	for row := 0; row < height; row++ {
		end := offset + counts[row]
		if end > len(data) {
			return nil, ErrTruncated
		}
		if row < crop.y || row >= crop.y+crop.height {
			offset = end
			continue
		}
		written := 0
		for written < width {
			if offset >= end {
				return nil, ErrTruncated
			}
			n := int8(data[offset])
			offset++
			if n >= 0 {
				count := int(n) + 1
				if written+count > width || offset+count > end {
					return nil, ErrTruncated
				}
				copy(rowBuffer[written:written+count], data[offset:offset+count])
				offset += count
				written += count
			} else if n != -128 {
				count := 1 - int(n)
				if written+count > width || offset >= end {
					return nil, ErrTruncated
				}
				value := data[offset]
				offset++
				for i := 0; i < count; i++ {
					rowBuffer[written+i] = value
				}
				written += count
			}
		}
		copy(plane[(row-crop.y)*crop.width:(row-crop.y+1)*crop.width], rowBuffer[crop.x:crop.x+crop.width])
		offset = end
	}
	return plane, nil
}

// assemble ports PSDReader.assemble: group dividers wrap their children,
// opacity folds the fill amount (except effect layers), unsupported blends
// keep their key for the report, and adjustments parse from the extras.
func assemble(raw []rawLayer, canvasWidth, canvasHeight int, resolution float64, remainingPixels int) (Document, error) {
	doc := Document{Width: canvasWidth, Height: canvasHeight, Resolution: resolution, Layers: []Record{}}
	var groups []string
	remaining := max(0, remainingPixels)
	for _, layer := range raw {
		// Photoshop stores groups bottom-to-top: type 3 divider, children,
		// then the folder (type 1/2).
		if layer.section == 3 {
			groups = append(groups, newID())
			continue
		}
		isGroup := layer.section == 1 || layer.section == 2
		id := newID()
		if isGroup && len(groups) > 0 {
			id = groups[len(groups)-1]
			groups = groups[:len(groups)-1]
		}
		record := Record{
			ID: id, Name: orDefault(layer.name, "Layer"),
			IsGroup:   isGroup,
			IsVisible: !layer.hidden,
			BlendKey:  layer.blendKey,
			Clipping:  layer.clipping,
			Left:      layer.left, Top: layer.top,
			Width: max(0, layer.right-layer.left), Height: max(0, layer.bottom-layer.top),
			MaskDefault: layer.maskDefault,
			MaskEnabled: !layer.maskDisabled,
			MaskLinked:  layer.maskLinked,
			Image:       nil,
		}
		if isGroup {
			record.Width, record.Height = canvasWidth, canvasHeight
			record.Left, record.Top = 0, 0
			if layer.blendKey == "pass" || layer.blendKey == "norm" {
				record.BlendKey = "pass"
			}
		}
		if len(groups) > 0 {
			record.ParentID = groups[len(groups)-1]
		}
		record.Kind = layerKind(layer, isGroup)
		record.CroppedToCanvas = layer.cropped
		hasEffects := record.Kind == KindEffects || layer.extra["lfx2"] != nil ||
			layer.extra["lrFX"] != nil || layer.extra["lmfx"] != nil
		if hasEffects {
			record.Opacity = float64(layer.opacity) / 255
		} else {
			record.Opacity = (float64(layer.opacity) / 255) * (float64(layer.fill) / 255)
		}
		record.Image = nil
		if !isGroup {
			record.Image = layer.image
		}
		if !isGroup && (layer.extra["vmsk"] != nil || layer.extra["vsms"] != nil || layer.extra["vogk"] != nil) {
			// Live vector re-rendering is not ported; the layer keeps the
			// raster pixels Photoshop stores and the builder adds the note.
			record.Kind = KindVector
		}
		if layer.maskFromRender {
			record.Mask = nil
		} else {
			record.Mask = layer.maskImage
		}
		record.MaskLeft, record.MaskTop = layer.maskLeft, layer.maskTop
		record.MaskWidth = max(0, layer.maskRt-layer.maskLeft)
		record.MaskHeight = max(0, layer.maskBottom-layer.maskTop)
		if !isGroup {
			if adj := parseAdjustment(layer.extra); adj != nil {
				record.Adjustment = adj
			}
		}
		if record.Adjustment != nil {
			record.Kind = KindAdjustment
		}
		doc.Layers = append(doc.Layers, record)
	}
	if len(groups) != 0 {
		return doc, ErrTruncated
	}
	_ = remaining
	return doc, nil
}

func layerKind(layer rawLayer, isGroup bool) LayerKind {
	if isGroup {
		return KindGroup
	}
	if hasAny(layer.extra, "TySh", "tySh", "txt2") {
		return KindText
	}
	if hasAny(layer.extra, "vmsk", "vsms", "vogk") {
		return KindVector
	}
	if hasAny(layer.extra, "SoLd", "SoLE") {
		return KindSmartObject
	}
	if hasAny(layer.extra, "lfx2", "lrFX", "lmfx") {
		return KindEffects
	}
	for key := range layer.extra {
		if adjustmentKeys[key] {
			return KindAdjustment
		}
	}
	return KindRaster
}

func hasAny(extra map[string][]byte, keys ...string) bool {
	for _, k := range keys {
		if extra[k] != nil {
			return true
		}
	}
	return false
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func newID() string {
	id := make([]byte, 16)
	if _, err := crand.Read(id); err != nil {
		panic(err)
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return formatUUID(id)
}

func formatUUID(b []byte) string {
	const hexDigits = "0123456789ABCDEF"
	out := make([]byte, 0, 36)
	for i, v := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out = append(out, '-')
		}
		out = append(out, hexDigits[v>>4], hexDigits[v&0xF])
	}
	return string(out)
}
