// Package heicio decodes HEIC images through libheif (LGPL-3.0, vcpkg
// static link) — the ImageIO HEIC path of the original importer (ticket
// 41). Decoding only; nothing here encodes.
package heicio

/*
#cgo CFLAGS: -I${SRCDIR}/../../vcpkg_installed/x64-mingw-static/include
#cgo LDFLAGS: -L${SRCDIR}/../../vcpkg_installed/x64-mingw-static/lib -Wl,--start-group -lheif -lde265 -lspng_static -lzs -Wl,--end-group -static-libstdc++ -static-libgcc
#include <libheif/heif.h>
#include <libheif/heif_metadata.h>
#include <libheif/heif_properties.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"

	"compositor-win/internal/rasterio"
	"compositor-win/internal/render"
)

// ErrUnreadable mirrors the importer's refusal for corrupt/unexpected files.
var ErrUnreadable = fmt.Errorf("无法读取该图像：文件可能已损坏或不可用")

func heifError(err C.struct_heif_error) error {
	if err.code == C.heif_error_Ok {
		return nil
	}
	message := C.GoString(err.message)
	if message == "" {
		message = "libheif 错误"
	}
	return fmt.Errorf("%w: %s", ErrUnreadable, message)
}

// Size reads the primary image's stored dimensions from the header only —
// the original's pixelSize probe, so an oversized file is refused before
// the decode work. Values are pre-transform (ispe), which is what the
// camera stored.
func Size(path string) (int, int, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))
	ctx := C.heif_context_alloc()
	if ctx == nil {
		return 0, 0, ErrUnreadable
	}
	defer C.heif_context_free(ctx)
	if err := heifError(C.heif_context_read_from_file(ctx, cPath, nil)); err != nil {
		return 0, 0, err
	}
	var handle *C.heif_image_handle
	if err := heifError(C.heif_context_get_primary_image_handle(ctx, &handle)); err != nil {
		return 0, 0, err
	}
	defer C.heif_image_handle_release(handle)
	w := int(C.heif_image_handle_get_width(handle))
	h := int(C.heif_image_handle_get_height(handle))
	if w < 1 || h < 1 {
		return 0, 0, ErrUnreadable
	}
	return w, h, nil
}

// Decode reads the primary image of an HEIC file into a premultiplied
// bitmap (straight RGBA out of libheif, premultiplied on the way in —
// HEIC stores straight alpha, same as the original's CGImageSource path).
//
// Orientation: libheif applies the file's transformative properties
// (irot/imir/clap) during decode. When the item carries none, the EXIF
// metadata block still may (the original applied the EXIF tag through
// CIImage.oriented) — that fallback runs here against the TIFF payload
// the block wraps.
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
	if orientation := exifFallbackOrientation(ctx, handle); orientation != 1 {
		bmp = rasterio.ApplyOrientation(bmp, orientation)
	}
	runtime.KeepAlive(bmp)
	return bmp, nil
}

// exifFallbackOrientation reads the EXIF orientation tag, but only when
// the item has no transformative properties — otherwise libheif already
// applied the rotation and applying the tag again would double it.
func exifFallbackOrientation(ctx *C.heif_context, handle *C.heif_image_handle) int {
	itemID := C.heif_image_handle_get_item_id(handle)
	var props [1]C.heif_property_id
	if n := int(C.heif_item_get_transformation_properties(ctx, itemID, &props[0], 1)); n > 0 {
		return 1
	}
	exif := C.CString("Exif")
	defer C.free(unsafe.Pointer(exif))
	count := int(C.heif_image_handle_get_number_of_metadata_blocks(handle, exif))
	if count <= 0 {
		return 1
	}
	if count > 64 {
		count = 64
	}
	ids := make([]C.heif_item_id, count)
	if int(C.heif_image_handle_get_list_of_metadata_block_IDs(handle, exif, &ids[0], C.int(count))) <= 0 {
		return 1
	}
	for _, id := range ids {
		size := int(C.heif_image_handle_get_metadata_size(handle, id))
		if size <= 4 || size > 1<<20 {
			continue
		}
		buf := make([]byte, size)
		if err := heifError(C.heif_image_handle_get_metadata(handle, id, unsafe.Pointer(&buf[0]))); err != nil {
			continue
		}
		// The block's first four bytes hold the offset to the TIFF header.
		offset := int(uint32(buf[0])<<24 | uint32(buf[1])<<16 | uint32(buf[2])<<8 | uint32(buf[3]))
		if offset < 0 || offset+8 > len(buf) {
			offset = 4
		}
		return rasterio.TIFFOrientation(buf[offset:])
	}
	return 1
}
