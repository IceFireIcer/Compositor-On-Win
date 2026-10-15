package project

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// exrMagic is an OpenEXR file's first four bytes.
var exrMagic = [4]byte{0x76, 0x2f, 0x31, 0x01}

// exrHeader is the part of an EXR header the package store needs to enforce
// the pixel limits without decoding the image (ticket 70 / ADR-0007): the
// data window's extent.
type exrHeader struct {
	Width  int
	Height int
}

// parseEXRHeader reads the magic, the version field and the attribute list up
// to its terminating empty name, picking out dataWindow. Full decoding is the
// HDR codec's job; like parsePNGHeader this only has to be strict enough that
// absurd dimensions cannot slip through as valid. The data window, not the
// display window, is the pixel extent a layer stores.
func parseEXRHeader(data []byte) (exrHeader, error) {
	if len(data) < 8 || !bytes.Equal(data[:4], exrMagic[:]) {
		return exrHeader{}, fmt.Errorf("不是有效的 EXR：签名不符")
	}
	// The version field is little-endian; its low byte is the format version,
	// 2 being the only one in use. The remaining bytes are feature flags
	// (long names, deep data) this reader does not need.
	if data[4] != 2 {
		return exrHeader{}, fmt.Errorf("不是有效的 EXR：版本 %d 不受支持", data[4])
	}
	pos := 8
	for {
		name, ok := exrCString(data, &pos)
		if !ok {
			return exrHeader{}, fmt.Errorf("不是有效的 EXR：属性列表未终止")
		}
		if name == "" {
			break // the empty name ends the header
		}
		typ, ok := exrCString(data, &pos)
		if !ok {
			return exrHeader{}, fmt.Errorf("不是有效的 EXR：属性 %q 的类型未终止", name)
		}
		if pos+4 > len(data) {
			return exrHeader{}, fmt.Errorf("不是有效的 EXR：属性 %q 的长度缺失", name)
		}
		size := int(int32(binary.LittleEndian.Uint32(data[pos:])))
		pos += 4
		if size < 0 || pos+size > len(data) {
			return exrHeader{}, fmt.Errorf("不是有效的 EXR：属性 %q 的长度 %d 超出文件", name, size)
		}
		if name == "dataWindow" {
			if typ != "box2i" || size < 16 {
				return exrHeader{}, fmt.Errorf("不是有效的 EXR：dataWindow 类型 %q 长度 %d", typ, size)
			}
			xMin := int(int32(binary.LittleEndian.Uint32(data[pos:])))
			yMin := int(int32(binary.LittleEndian.Uint32(data[pos+4:])))
			xMax := int(int32(binary.LittleEndian.Uint32(data[pos+8:])))
			yMax := int(int32(binary.LittleEndian.Uint32(data[pos+12:])))
			if xMax < xMin || yMax < yMin {
				return exrHeader{}, fmt.Errorf("不是有效的 EXR：dataWindow 为空")
			}
			return exrHeader{Width: xMax - xMin + 1, Height: yMax - yMin + 1}, nil
		}
		pos += size
	}
	return exrHeader{}, fmt.Errorf("不是有效的 EXR：缺少 dataWindow")
}

// exrCString reads one null-terminated string, advancing pos past it.
func exrCString(data []byte, pos *int) (string, bool) {
	if *pos >= len(data) {
		return "", false
	}
	end := bytes.IndexByte(data[*pos:], 0)
	if end < 0 {
		return "", false
	}
	s := string(data[*pos : *pos+end])
	*pos += end + 1
	return s, true
}
