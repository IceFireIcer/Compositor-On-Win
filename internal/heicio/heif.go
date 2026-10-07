// Package heicio decodes HEIC images through libheif (LGPL-3.0, vcpkg
// static link) — the ImageIO HEIC path of the original importer (ticket
// 41). Decoding only; nothing here encodes.
package heicio

/*
#cgo CFLAGS: -I${SRCDIR}/../../vcpkg_installed/x64-mingw-static/include
#cgo LDFLAGS: -L${SRCDIR}/../../vcpkg_installed/x64-mingw-static/lib -Wl,--start-group -lheif -lde265 -lspng_static -lzs -Wl,--end-group -static-libstdc++ -static-libgcc
#include <libheif/heif.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"

	"compositor-win/internal/render"
)

// ErrUnreadable mirrors the importer's refusal for corrupt/unexpected files.
var ErrUnreadable = fmt.Errorf("无法读取该图像：文件可能已损坏或不可用")

func heifError(err C.struct_heif_error) error {
	if err.code == C.heif_error_Ok {
		return nil
	}
	message := C.GoString(err.message)
	return fmt.Errorf("%w: %s", ErrUnreadable, message)
}

// Decode reads the primary image of an HEIC file into a premultiplied
// bitmap (straight RGBA out of libheif, premultiplied on the way in —
// HEIC stores straight alpha, same as the original's CGImageSource path).
func Decode(path string) (*render.Bitmap, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	ctx := C.heif_context_alloc()
	if ctx == nil {
		return nil, ErrUnreadable
	}
	defer C.heif_context_free(ctx)
	if err := heifError(C.heif_context_read_from_file(ctx, cPath, nil)); err != nil {
		return nil, err
	}
	var handle *C.heif_image_handle
	if err := heifError(C.heif_context_get_primary_image_handle(ctx, &handle)); err != nil {
		return nil, err
	}
	defer C.heif_image_handle_release(handle)
	width := int(C.heif_image_handle_get_width(handle))
	height := int(C.heif_image_handle_get_height(handle))
	if width < 1 || height < 1 {
		return nil, ErrUnreadable
	}
	var img *C.heif_image
	if err := heifError(C.heif_decode_image(handle, &img,
		C.heif_colorspace_RGB, C.heif_chroma_interleaved_RGBA, nil)); err != nil {
		return nil, err
	}
	defer C.heif_image_release(img)
	var stride C.int
	plane := C.heif_image_get_plane_readonly(img, C.heif_channel_interleaved, &stride)
	if plane == nil || int(stride) < width*4 {
		return nil, ErrUnreadable
	}
	bmp := render.NewBitmap(width, height)
	row := (*[1 << 30]C.uint8_t)(unsafe.Pointer(plane))[: int(stride)*height : int(stride)*height]
	for y := 0; y < height; y++ {
		base := y * int(stride)
		for x := 0; x < width; x++ {
			i := base + x*4
			a := uint32(row[i+3])
			j := (y*width + x) * 4
			if a == 0 {
				continue
			}
			// Straight RGBA → premultiplied RGBA (sRGB, 8-bit).
			bmp.Pix[j] = uint8(uint32(row[i]) * a / 255)
			bmp.Pix[j+1] = uint8(uint32(row[i+1]) * a / 255)
			bmp.Pix[j+2] = uint8(uint32(row[i+2]) * a / 255)
			bmp.Pix[j+3] = uint8(a)
		}
	}
	runtime.KeepAlive(bmp)
	return bmp, nil
}
