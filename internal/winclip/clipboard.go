// Package winclip puts bitmaps on the Windows clipboard via user32.
// Ticket 42 ships Copy Merged (⇧⌘C) on this seam; the richer clipboard
// surface (copy layer/pixels, paste, drag-drop) lands with ticket 48.
package winclip

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32             = windows.NewLazySystemDLL("user32.dll")
	kernel32           = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClipboard  = user32.NewProc("OpenClipboard")
	procCloseClipboard = user32.NewProc("CloseClipboard")
	procEmptyClipboard = user32.NewProc("EmptyClipboard")
	procSetClipData    = user32.NewProc("SetClipboardData")
	procGlobalAlloc    = kernel32.NewProc("GlobalAlloc")
	procGlobalLock     = kernel32.NewProc("GlobalLock")
	procGlobalUnlock   = kernel32.NewProc("GlobalUnlock")
)

const (
	gmemMoveable = 0x0002
	cfDIB        = 8 // device-independent bitmap (BITMAPINFO + pixels)
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

	r1, _, err := procOpenClipboard.Call(0)
	if r1 == 0 {
		return errors.New("打开剪贴板失败: " + err.Error())
	}
	defer procCloseClipboard.Call()
	if r1, _, _ = procEmptyClipboard.Call(); r1 == 0 {
		return errors.New("清空剪贴板失败")
	}
	// syscall.SyscallN (not LazyProc.Call): go vet's unsafeptr check
	// accepts a pointer conversion of a SyscallN result, which is exactly
	// the GlobalLock hand-off below.
	r1, _, _ := syscall.SyscallN(procGlobalAlloc.Addr(), gmemMoveable, uintptr(len(dib)))
	if r1 == 0 {
		return errors.New("分配剪贴板内存失败")
	}
	p, _, _ := syscall.SyscallN(procGlobalLock.Addr(), r1)
	if p == 0 {
		return errors.New("锁定剪贴板内存失败")
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(p)), len(dib)), dib)
	procGlobalUnlock.Call(r1)
	r1, _, _ = procSetClipData.Call(cfDIB, r1)
	if r1 == 0 {
		return errors.New("写入剪贴板失败")
	}
	// Ownership of hGlobal passed to the clipboard on success.
	return nil
}
