// Package winclip puts bitmaps on the Windows clipboard. Ticket 42 ships
// Copy Merged (⇧⌘C) on this seam; the richer clipboard surface (copy
// layer/pixels, paste, drag-drop) lands with ticket 48. The GlobalAlloc/
// GlobalLock/SetClipboardData work lives in clipbridge.cpp — Win32 handle
// patterns that go vet's unsafeptr check cannot express; the Go side stays
// plain byte slices.
package winclip

/*
#include "clipbridge.h"
*/
import "C"

import (
	"errors"
	"runtime"
	"unsafe"
)

// PutBitmap copies a premultiplied RGBA bitmap to the clipboard as a
// 32bpp top-down CF_DIB with straight (un-premultiplied) BGRA pixels.
// The call blocks for microseconds and holds the clipboard only for the
// data hand-off.
func PutBitmap(w, h int, pix []uint8) error {
	if runtime.GOOS != "windows" {
		return errors.New("系统剪贴板仅在 Windows 可用")
	}
	if w <= 0 || h <= 0 || len(pix) < w*h*4 {
		return errors.New("剪贴板位图为空")
	}
	// BITMAPINFOHEADER + straight BGRA pixels, top-down via negative height.
	dib := make([]uint8, 40+w*h*4)
	wr := func(off int, v uint32) {
		dib[off] = byte(v)
		dib[off+1] = byte(v >> 8)
		dib[off+2] = byte(v >> 16)
		dib[off+3] = byte(v >> 24)
	}
	wr(0, 40)
	wr(4, uint32(w))
	wr(8, uint32(-h)) // negative: rows run top-down like every other bitmap here
	wr(12, 1)         // planes
	wr(14, 32)        // bpp
	wr(16, 0)         // BI_RGB
	wr(20, uint32(w*h*4))
	for i := 0; i < w*h; i++ {
		a := uint32(pix[i*4+3])
		src := pix[i*4 : i*4+4]
		dst := dib[40+i*4 : 40+i*4+4]
		if a == 0 {
			continue // straight BGRA zero
		}
		dst[0] = uint8(uint32(src[2]) * 255 / a) // B
		dst[1] = uint8(uint32(src[1]) * 255 / a) // G
		dst[2] = uint8(uint32(src[0]) * 255 / a) // R
		dst[3] = uint8(a)
	}

	var data []byte
	if len(dib) > 0 {
		data = dib
	} else {
		data = []byte{0}
	}
	switch C.clip_put_dib((*C.uchar)(unsafe.Pointer(&data[0])), C.ulong(len(dib))) {
	case 0:
		return nil
	case 1:
		return errors.New("打开剪贴板失败")
	default:
		return errors.New("写入剪贴板失败")
	}
}
