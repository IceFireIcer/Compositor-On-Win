//go:build windows

// 票 87 的 U34 剩余：CMYK JPEG 导出。
//
// Go 标准库的 image/jpeg 只写 1 或 3 分量，四分量 JPEG 写不了；自写熵编码器是几千行且必然
// 与标准实现有出入。所以直接用随 vcpkg 一起装好的 libjpeg（libjpeg-turbo），与 lcms2/LibRaw/
// libheif 同一条路子。
//
// **油墨约定**：Photoshop 与 libjpeg 都按 Adobe 约定写 CMYK JPEG —— 存储值是**反相**的
// （255 = 不上墨，0 = 满墨），并且带 Adobe APP14 标记。colorio.ConvertToInk 产出的是 ICC/PDF
// 约定（0 = 不上墨），所以这里逐字节取反，读者按标记再反回来。
package render

/*
#cgo CFLAGS: -I${SRCDIR}/../../vcpkg_installed/x64-mingw-static/include
#cgo LDFLAGS: -L${SRCDIR}/../../vcpkg_installed/x64-mingw-static/lib -Wl,--start-group -ljpeg -Wl,--end-group -static-libstdc++ -static-libgcc
#include <stdio.h> // jpeglib.h 用到 FILE，必须先包含
#include <jpeglib.h>
#include <setjmp.h>
#include <stdlib.h>
#include <string.h>

// libjpeg 的出错方式是 longjmp，不是返回值；不接住就会直接终止进程。
struct go_jpeg_error {
    struct jpeg_error_mgr pub;
    jmp_buf jump;
    char message[JMSG_LENGTH_MAX];
};

static void go_jpeg_error_exit(j_common_ptr cinfo) {
    struct go_jpeg_error *err = (struct go_jpeg_error *)cinfo->err;
    (*cinfo->err->format_message)(cinfo, err->message);
    longjmp(err->jump, 1);
}

// go_jpeg_write_cmyk encodes 8-bit CMYK ink (w*h*4, ICC/PDF convention: 0 is no
// ink) as a 4-component JPEG at the given quality. Returns 0 on success and
// fills *out / *outLen with a malloc'd buffer the caller frees.
static int go_jpeg_write_cmyk(const unsigned char *ink, int w, int h, int quality,
                              unsigned char **out, unsigned long *outLen) {
    struct jpeg_compress_struct cinfo;
    struct go_jpeg_error err;
    unsigned char *row = NULL;
    int rc = 1;

    cinfo.err = jpeg_std_error(&err.pub);
    err.pub.error_exit = go_jpeg_error_exit;
    err.message[0] = 0;
    if (setjmp(err.jump)) {
        jpeg_destroy_compress(&cinfo);
        free(row);
        if (*out) { free(*out); *out = NULL; }
        *outLen = 0;
        return 2;
    }

    jpeg_create_compress(&cinfo);
    jpeg_mem_dest(&cinfo, out, outLen);
    cinfo.image_width = (JDIMENSION)w;
    cinfo.image_height = (JDIMENSION)h;
    cinfo.input_components = 4;
    cinfo.in_color_space = JCS_CMYK;
    jpeg_set_defaults(&cinfo);
    jpeg_set_quality(&cinfo, quality, TRUE);
    cinfo.optimize_coding = TRUE;
    // Adobe 约定：存储值反相。libjpeg 的 JCS_CMYK 输入按「0 = 满墨」解释，
    // 所以把 ICC 约定的值取反后再喂进去。
    jpeg_start_compress(&cinfo, TRUE);

    row = (unsigned char *)malloc((size_t)w * 4);
    if (row == NULL) {
        rc = 3;
    } else {
        while (cinfo.next_scanline < cinfo.image_height) {
            const unsigned char *src = ink + (size_t)cinfo.next_scanline * (size_t)w * 4;
            JSAMPROW rows[1];
            int i;
            for (i = 0; i < w * 4; ++i) {
                row[i] = (unsigned char)(255 - src[i]);
            }
            rows[0] = row;
            jpeg_write_scanlines(&cinfo, rows, 1);
        }
        rc = 0;
    }
    jpeg_finish_compress(&cinfo);
    jpeg_destroy_compress(&cinfo);
    free(row);
    return rc;
}

// go_jpeg_read_cmyk decodes a 4-component JPEG back to 8-bit ink (w*h*4, ICC/PDF
// convention: this function un-inverts what the writer inverted). Returns 0 on
// success, 4 when the file is not a 4-component JPEG.
static int go_jpeg_read_cmyk(const unsigned char *data, unsigned long len,
                             unsigned char **out, int *w, int *h) {
    struct jpeg_decompress_struct cinfo;
    struct go_jpeg_error err;
    unsigned char *pixels = NULL;
    int rc = 1;

    cinfo.err = jpeg_std_error(&err.pub);
    err.pub.error_exit = go_jpeg_error_exit;
    err.message[0] = 0;
    if (setjmp(err.jump)) {
        jpeg_destroy_decompress(&cinfo);
        if (pixels) { free(pixels); }
        *out = NULL;
        return 2;
    }

    jpeg_create_decompress(&cinfo);
    jpeg_mem_src(&cinfo, data, len);
    jpeg_read_header(&cinfo, TRUE);
    if (cinfo.num_components != 4) {
        jpeg_destroy_decompress(&cinfo);
        return 4;
    }
    // 按 CMYK 输出，别再让 libjpeg 替我们转成 RGB。
    cinfo.out_color_space = JCS_CMYK;
    jpeg_start_decompress(&cinfo);
    *w = (int)cinfo.output_width;
    *h = (int)cinfo.output_height;
    pixels = (unsigned char *)malloc((size_t)cinfo.output_width * cinfo.output_height * 4);
    if (pixels == NULL) {
        jpeg_destroy_decompress(&cinfo);
        return 3;
    }
    while (cinfo.output_scanline < cinfo.output_height) {
        JSAMPROW rows[1];
        rows[0] = pixels + (size_t)cinfo.output_scanline * (size_t)cinfo.output_width * 4;
        jpeg_read_scanlines(&cinfo, rows, 1);
    }
    jpeg_finish_decompress(&cinfo);
    jpeg_destroy_decompress(&cinfo);
    *out = pixels;
    return 0;
}

static const char *go_jpeg_error_message(struct go_jpeg_error *err) { return err->message; }
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// EncodeCMYKJPEG writes 8-bit CMYK ink as a 4-component JPEG: the Adobe
// convention (inverted samples plus the APP14 marker), which is what Photoshop
// and every print workflow expect from a CMYK JPEG.
//
// The ink arrives in the ICC/PDF convention colorio.ConvertToInk produces
// (0 = no ink) and is inverted on the way in.
func EncodeCMYKJPEG(ink []uint8, w, h, quality int) ([]byte, error) {
	if w < 1 || h < 1 {
		return nil, fmt.Errorf("CMYK JPEG 尺寸无效: %dx%d", w, h)
	}
	if len(ink) != w*h*4 {
		return nil, fmt.Errorf("油墨缓冲是 %d 字节，%dx%d 需要 %d", len(ink), w, h, w*h*4)
	}
	if quality < 1 {
		quality = 1
	}
	if quality > 100 {
		quality = 100
	}
	var out *C.uchar
	var outLen C.ulong
	rc := C.go_jpeg_write_cmyk((*C.uchar)(unsafe.Pointer(&ink[0])), C.int(w), C.int(h), C.int(quality), &out, &outLen)
	if rc != 0 {
		return nil, fmt.Errorf("CMYK JPEG 编码失败（libjpeg 返回 %d）", int(rc))
	}
	data := C.GoBytes(unsafe.Pointer(out), C.int(outLen))
	C.free(unsafe.Pointer(out))
	return data, nil
}

// DecodeCMYKJPEG reads a 4-component JPEG back into 8-bit ink in the ICC/PDF
// convention (0 = no ink), undoing the Adobe inversion the writer applied.
//
// It is the JPEG half of ticket 86's remaining CMYK import work, and the reader
// the round-trip test checks the writer against. It does not look for an embedded
// ICC profile yet — an APP2 ICC_PROFILE would have to be reassembled from its
// segments, and until that exists the caller has to supply the profile.
func DecodeCMYKJPEG(data []byte) ([]uint8, int, int, error) {
	if len(data) == 0 {
		return nil, 0, 0, fmt.Errorf("空的 JPEG")
	}
	var out *C.uchar
	var w, h C.int
	rc := C.go_jpeg_read_cmyk((*C.uchar)(unsafe.Pointer(&data[0])), C.ulong(len(data)), &out, &w, &h)
	switch rc {
	case 0:
	case 4:
		return nil, 0, 0, fmt.Errorf("不是四分量 JPEG（CMYK JPEG 必须有 4 个分量）")
	default:
		return nil, 0, 0, fmt.Errorf("CMYK JPEG 解码失败（libjpeg 返回 %d）", int(rc))
	}
	defer C.free(unsafe.Pointer(out))
	ink := C.GoBytes(unsafe.Pointer(out), w*h*4)
	// 写的时候逐字节取反，读的时候反回来。
	for i := range ink {
		ink[i] = 255 - ink[i]
	}
	return ink, int(w), int(h), nil
}
