package rawio

// The LibRaw develop session (ticket 41): open once, develop per slider
// move. The vcpkg-built static libraries live in
// vcpkg_installed/x64-mingw-static (manifest mode, relative to the repo
// root), LGPL-2.1 linked in — declared separately in
// THIRD-PARTY-NOTICES.md.

/*
#cgo CXXFLAGS: -I${SRCDIR}/../../vcpkg_installed/x64-mingw-static/include -std=c++17
#cgo LDFLAGS: -L${SRCDIR}/../../vcpkg_installed/x64-mingw-static/lib -Wl,--start-group -lraw_r -ljasper -llcms2 -ljpeg -lzs -lws2_32 -Wl,--end-group -static-libstdc++ -static-libgcc
#include <stdlib.h>
#include "rawbridge.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// cInt maps a Go bool onto the C int the bridge ABI uses.
func cInt(b bool) C.int {
	if b {
		return 1
	}
	return 0
}

// Session holds one opened RAW file. Not safe for concurrent use — the
// develop sheet owns it (the original's Queue actor serialized the same
// way).
type Session struct {
	ptr    *C.RB_Session
	Width  int
	Height int
	// CamMul is the camera's own white balance (R, G, B) — the asShot seed.
	CamMul [3]float64
}

// Open opens and unpacks a RAW file. halfSize halves both sides for the
// develop sheet's live preview; the import itself opens with false.
func Open(path string, halfSize bool) (*Session, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	var ptr *C.RB_Session
	var w, h C.int
	camMul := [3]C.double{}
	rc := C.rb_open(cPath, cInt(halfSize), &ptr, &w, &h, &camMul[0])
	if rc != 0 {
		return nil, fmt.Errorf("%s", C.GoString(C.rb_last_error()))
	}
	return &Session{
		ptr:    ptr,
		Width:  int(w),
		Height: int(h),
		CamMul: [3]float64{float64(camMul[0]), float64(camMul[1]), float64(camMul[2])},
	}, nil
}

// Close releases the held frame.
func (s *Session) Close() {
	if s.ptr != nil {
		C.rb_close(s.ptr)
		s.ptr = nil
	}
}

// Develop renders the frame with the settings applied: LibRaw spends the
// white balance (camera's own when asShot) and outputs linear 16-bit RGB;
// ApplyDevelop spends exposure, further Kelvin/tint and the boost curve,
// returning opaque 8-bit sRGB pixels. asShot flags the camera-WB path.
func (s *Session) Develop(settings DevelopSettings) ([]uint8, int, int, error) {
	useCameraWb := settings.IsAsShot()
	mul := [4]C.double{1, 1, 1, 1}
	if !useCameraWb {
		r, g, b := kelvinToGains(settings.Temperature, settings.Tint)
		mul = [4]C.double{C.double(r), C.double(g), C.double(b), C.double(g)}
	}
	var rgb *C.ushort
	var w, h C.int
	rc := C.rb_develop(s.ptr, &mul[0], cInt(useCameraWb), &rgb, &w, &h)
	if rc != 0 {
		return nil, 0, 0, fmt.Errorf("%s", C.GoString(C.rb_last_error()))
	}
	defer C.rb_free(rgb)
	count := int(w) * int(h)
	r16 := (*[1 << 30]C.ushort)(unsafe.Pointer(rgb))[:count:count]
	planes := make([]uint16, count*3)
	for i := 0; i < count; i++ {
		planes[i*3] = uint16(r16[i*3])
		planes[i*3+1] = uint16(r16[i*3+1])
		planes[i*3+2] = uint16(r16[i*3+2])
	}
	pix := ApplyDevelop(planes[0:count:count], planes[count:count*2:count*2], planes[count*2:], settings)
	return pix, int(w), int(h), nil
}
