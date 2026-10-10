// Package winclip reads and writes the Windows clipboard's bitmaps. Ticket 42
// shipped Copy Merged (⇧⌘C) on the write side; ticket 48 adds the read side
// (PNG or CF_DIB in, premultiplied RGBA out) and the sequence number that
// tells the app whether anything copied since it last wrote. The
// GlobalAlloc/GlobalLock/SetClipboardData work lives in clipbridge.cpp —
// Win32 handle patterns that go vet's unsafeptr check cannot express; the Go
// side stays plain byte slices.
package winclip

/*
#include <stdlib.h>
#include "clipbridge.h"
*/
import "C"

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
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

	switch C.clip_put_dib((*C.uchar)(unsafe.Pointer(&dib[0])), C.ulong(len(dib))) {
	case 0:
		return nil
	case 1:
		return errors.New("打开剪贴板失败")
	default:
		return errors.New("写入剪贴板失败")
	}
}

// GetImage reads the clipboard's image as premultiplied RGBA8. The registered
// "PNG" format wins when present (lossless, carries alpha); CF_DIB is the
// fallback every Windows app can produce. Returns an error when the clipboard
// holds no readable image.
func GetImage() (*ClipBitmap, error) {
	if runtime.GOOS != "windows" {
		return nil, errors.New("系统剪贴板仅在 Windows 可用")
	}
	var pngErr error
	if data, err := readFormat("PNG"); err == nil {
		if bmp, err := pngToClipBitmap(data); err == nil {
			return bmp, nil
		} else {
			pngErr = err // 记住"有图但坏了"，别退化成"没有图像"
		}
	}
	data, err := readFormat("") // CF_DIB
	if err != nil {
		if pngErr != nil {
			return nil, pngErr
		}
		return nil, err
	}
	return dibToRGBA(data)
}

// Sequence is the clipboard sequence number: it advances whenever anything —
// this app or another — writes to the clipboard. The session remembers it
// beside the payload it copied, exactly as the original compares
// NSPasteboard.changeCount, so a foreign copy invalidates the app's own
// clipboard (and Paste falls back to whatever the system clipboard holds).
func Sequence() uint32 {
	if runtime.GOOS != "windows" {
		return 0
	}
	return uint32(C.clip_sequence())
}

// clipboardReadLimit bounds one clipboard read (a 30k×30k RGBA image is
// 3.6 GB, far past any real clipboard payload).
const clipboardReadLimit = 512 << 20

// readFormat fetches one clipboard format; name == "" means CF_DIB. The C
// side sizes the payload first (a too-small buffer reports the size needed),
// so the allocation happens Go-side.
func readFormat(name string) ([]byte, error) {
	var cname *C.char
	if name != "" {
		cname = C.CString(name)
		defer C.free(unsafe.Pointer(cname))
	}
	var needed C.ulong
	switch C.clip_get_data(cname, nil, 0, &needed) {
	case 0:
		return nil, nil // empty payload
	case 2:
		if name == "" {
			return nil, errors.New("剪贴板里没有位图")
		}
		return nil, fmt.Errorf("剪贴板里没有 %s 格式", name)
	case 1:
		return nil, errors.New("打开剪贴板失败")
	}
	size := int(needed)
	if size <= 0 || size > clipboardReadLimit {
		return nil, fmt.Errorf("剪贴板图像过大（%d 字节）", size)
	}
	buf := make([]uint8, size)
	var written C.ulong
	switch C.clip_get_data(cname, (*C.uchar)(unsafe.Pointer(&buf[0])), C.ulong(size), &written) {
	case 0:
		return buf[:int(written)], nil
	case 1:
		return nil, errors.New("打开剪贴板失败")
	case 3:
		return nil, errors.New("剪贴板在读取过程中发生了变化")
	default:
		return nil, errors.New("剪贴板里没有位图")
	}
}

// pngToClipBitmap converts PNG bytes to the premultiplied working format.
// color.Color.RGBA() already returns premultiplied 16-bit values, so >>8 is
// the pipeline's 8-bit premultiplied spelling.
func pngToClipBitmap(data []byte) (*ClipBitmap, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("剪贴板 PNG 无法解码: %w", err)
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || w > 30_000 || h > 30_000 {
		return nil, fmt.Errorf("剪贴板图像尺寸非法（%d×%d）", w, h)
	}
	out := &ClipBitmap{W: w, H: h, Pix: make([]uint8, w*h*4)}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			i := (y*w + x) * 4
			out.Pix[i] = uint8(r >> 8)
			out.Pix[i+1] = uint8(g >> 8)
			out.Pix[i+2] = uint8(bl >> 8)
			out.Pix[i+3] = uint8(a >> 8)
		}
	}
	return out, nil
}
