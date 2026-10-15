//go:build windows

// Package colorio is the Windows counterpart of the original's CMYK.swift:
// the print-facing view and export transforms, on documents that stay RGB.
// Ink values are never edited directly; what printing changes is mostly how
// saturated a color can be, which the proof shows on the RGB screen.
//
// The color engine is Little CMS 2 (lcms2, MIT) — see docs/adr/0008. The
// library is built by vcpkg (x64-mingw-static, manifest mode, relative to
// the repo root) exactly like LibRaw and libheif in internal/rawio and
// internal/heicio.
package colorio

/*
#cgo CFLAGS: -I${SRCDIR}/../../vcpkg_installed/x64-mingw-static/include
#cgo LDFLAGS: -L${SRCDIR}/../../vcpkg_installed/x64-mingw-static/lib -Wl,--start-group -llcms2 -Wl,--end-group -static-libstdc++ -static-libgcc
#include <lcms2.h>
#include <stdio.h>
#include <stdlib.h>

// lcms2 reports what went wrong through a log handler; keep its last message
// so Go can put it in an error instead of losing it.
static char go_cmyk_last_error[256];

void go_cmyk_error_handler(cmsContext ContextID, cmsUInt32Number ErrorCode,
                           const char *Text) {
    (void)ContextID;
    (void)ErrorCode;
    snprintf(go_cmyk_last_error, sizeof(go_cmyk_last_error), "%s", Text == NULL ? "" : Text);
}

// go_cmyk_install_handler installs the handler. It lives in C because the
// handler must have C linkage: taking its address in Go would make the
// symbol a Go variable, which the C side cannot call.
static void go_cmyk_install_handler(void) {
    cmsSetLogErrorHandler(go_cmyk_error_handler);
}

// go_cmyk_last_message returns the message of the last lcms2 error; empty
// when lcms2 has not reported one.
static const char *go_cmyk_last_message(void) {
    return go_cmyk_last_error;
}

// The transforms are built here rather than in Go so the profile handles
// live and die inside one call: lcms2 keeps the cmsHPROFILE it is handed
// alive for as long as the transform exists, and no cmsHPROFILE ever
// escapes to the Go heap.

// go_cmyk_srgb_to_ink converts an sRGB (TYPE_RGB_8, no alpha) buffer to
// 8-bit CMYK.
static cmsHTRANSFORM go_cmyk_srgb_to_ink(cmsHPROFILE profile, cmsUInt32Number flags) {
    cmsHPROFILE srgb = cmsCreate_sRGBProfile();
    if (srgb == NULL) return NULL;
    cmsHTRANSFORM t = cmsCreateTransform(srgb, TYPE_RGB_8, profile, TYPE_CMYK_8,
                                         INTENT_RELATIVE_COLORIMETRIC, flags);
    cmsCloseProfile(srgb);
    return t;
}

// go_cmyk_ink_to_srgb is the same transform read backwards, for CMYK import.
static cmsHTRANSFORM go_cmyk_ink_to_srgb(cmsHPROFILE profile, cmsUInt32Number flags) {
    cmsHPROFILE srgb = cmsCreate_sRGBProfile();
    if (srgb == NULL) return NULL;
    cmsHTRANSFORM t = cmsCreateTransform(profile, TYPE_CMYK_8, srgb, TYPE_RGB_8,
                                         INTENT_RELATIVE_COLORIMETRIC, flags);
    cmsCloseProfile(srgb);
    return t;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// ErrNotCMYK mirrors upstream `CMYKError.notCMYK`: the file opened, but it
// is not a CMYK output profile, so it cannot describe a press.
var ErrNotCMYK = errors.New("That profile isn't a CMYK output profile.")

// cmykSignature is cmsSigCmykData ('CMYK'): the color space an output
// profile for a press must have.
const cmykSignature = C.cmsSigCmykData

// lcmsOnce installs the error handler once per process. lcms2 has no
// per-thread handler here, so the handler is global and its buffer is
// guarded by lcmsErrorMu.
var (
	lcmsOnce    sync.Once
	lcmsErrorMu sync.Mutex
)

// checkCMYKProfile opens data as an ICC profile and requires it to be a
// CMYK output profile — the gate upstream's `CMYKProfile.init?(name:data:)`
// applies. Every caller must close the returned handle.
func checkCMYKProfile(data []byte) (C.cmsHPROFILE, error) {
	profile, err := openICCProfile(data)
	if err != nil {
		// Not a profile at all is the same refusal as a profile that is not
		// CMYK: neither can describe a press.
		return nil, fmt.Errorf("%w (%v)", ErrNotCMYK, err)
	}
	if C.cmsGetColorSpace(profile) != cmykSignature {
		C.cmsCloseProfile(profile)
		return nil, ErrNotCMYK
	}
	return profile, nil
}

// lcmsMessage is lcms2's own last error, for the diagnostics a broken
// profile deserves.
func lcmsMessage() string {
	lcmsErrorMu.Lock()
	defer lcmsErrorMu.Unlock()
	return C.GoString(C.go_cmyk_last_message())
}

// IsCMYKProfile reports whether data is an ICC profile whose color space is
// CMYK — the gate upstream's `CMYKProfile.init?(name:data:)` applies.
func IsCMYKProfile(data []byte) bool {
	profile, err := checkCMYKProfile(data)
	if err != nil {
		return false
	}
	C.cmsCloseProfile(profile)
	return true
}

// isICCProfile reports whether data opens as an ICC profile at all, whatever
// its color space.
func isICCProfile(data []byte) bool {
	profile, err := openICCProfile(data)
	if err != nil {
		return false
	}
	C.cmsCloseProfile(profile)
	return true
}

// openICCProfile opens data as an ICC profile without judging its color
// space. The caller closes the handle.
func openICCProfile(data []byte) (C.cmsHPROFILE, error) {
	if len(data) == 0 {
		return nil, ErrNotCMYK
	}
	lcmsOnce.Do(func() { C.go_cmyk_install_handler() })
	profile := C.cmsOpenProfileFromMem(unsafe.Pointer(&data[0]), C.cmsUInt32Number(len(data)))
	if profile == nil {
		return nil, fmt.Errorf("unreadable ICC profile: %s", lcmsMessage())
	}
	return profile, nil
}

// ConvertToInk converts an 8-bit unassociated sRGB image (w*h*3 bytes) to
// the profile's inks, over matte where it is clear: ink has no
// transparency, so the image is flattened over the matte in RGB first,
// exactly as upstream does. Relative colorimetric, as Photoshop converts by
// default, so colors the press cannot reach go to the nearest it can.
//
// The result is 8-bit CMYK, w*h*4 bytes, with no alpha.
func ConvertToInk(rgb []uint8, w, h int, profile []byte, matte [3]uint8) ([]uint8, error) {
	if w < 0 || h < 0 || len(rgb) != w*h*3 {
		return nil, fmt.Errorf("sRGB buffer is %d bytes, want %d for %dx%d", len(rgb), w*h*3, w, h)
	}
	handle, err := checkCMYKProfile(profile)
	if err != nil {
		return nil, err
	}
	defer C.cmsCloseProfile(handle)

	// Flattened over the matte in RGB first: the CMYK context takes no alpha.
	flat := flattenOverMatte(rgb, matte)

	transform := C.go_cmyk_srgb_to_ink(handle, 0)
	if transform == nil {
		return nil, fmt.Errorf("could not build the sRGB to CMYK transform: %s", lcmsMessage())
	}
	defer C.cmsDeleteTransform(transform)

	ink := make([]uint8, w*h*4)
	if len(ink) > 0 {
		C.cmsDoTransform(transform, unsafe.Pointer(&flat[0]), unsafe.Pointer(&ink[0]), C.cmsUInt32Number(w*h))
	}
	return ink, nil
}

// ConvertToRGB converts 8-bit CMYK (w*h*4 bytes) back to 8-bit sRGB through
// the profile. The CMYK import path needs it; the proof uses it to read what
// printing a color shows on screen.
func ConvertToRGB(ink []uint8, w, h int, profile []byte) ([]uint8, error) {
	if w < 0 || h < 0 || len(ink) != w*h*4 {
		return nil, fmt.Errorf("CMYK buffer is %d bytes, want %d for %dx%d", len(ink), w*h*4, w, h)
	}
	handle, err := checkCMYKProfile(profile)
	if err != nil {
		return nil, err
	}
	defer C.cmsCloseProfile(handle)

	transform := C.go_cmyk_ink_to_srgb(handle, 0)
	if transform == nil {
		return nil, fmt.Errorf("could not build the CMYK to sRGB transform: %s", lcmsMessage())
	}
	defer C.cmsDeleteTransform(transform)

	rgb := make([]uint8, w*h*3)
	if len(rgb) > 0 {
		C.cmsDoTransform(transform, unsafe.Pointer(&ink[0]), unsafe.Pointer(&rgb[0]), C.cmsUInt32Number(w*h))
	}
	return rgb, nil
}

// flattenOverMatte is the explicit "flatten over the matte in RGB first"
// step upstream performs before the CMYK conversion. The buffers this
// package receives are already flattened by the caller — the compositor's
// canvas is opaque 8-bit RGBA over a matte — so there is no alpha channel
// left to composite here; the matte is applied by whoever has the alpha.
//
// It is kept as a named step so the conversion order reads like the
// original, and so the matte stays part of the API contract.
func flattenOverMatte(rgb []uint8, matte [3]uint8) []uint8 {
	_ = matte
	out := make([]uint8, len(rgb))
	copy(out, rgb)
	return out
}
