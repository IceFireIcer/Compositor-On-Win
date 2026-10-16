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
// go_jpeg_write_icc writes the ICC profile as APP2 ICC_PROFILE segments, the
// container JPEG has for one (the profile is split because a marker cannot
// exceed 64 KB). Each segment carries its 1-based index and the segment count.
static void go_jpeg_write_icc(struct jpeg_compress_struct *cinfo,
                              const unsigned char *profile, unsigned long profileLen) {
    const unsigned long chunk = 65533 - 14;
    unsigned long done = 0;
    int index = 1;
    int total = (int)((profileLen + chunk - 1) / chunk);
    (void)go_jpeg_write_icc;
    while (done < profileLen) {
        unsigned long n = profileLen - done < chunk ? profileLen - done : chunk;
        unsigned char *seg = (unsigned char *)malloc(14 + n);
        if (seg == NULL) { return; }
        memcpy(seg, "ICC_PROFILE\0", 12);
        seg[12] = (unsigned char)index;
        seg[13] = (unsigned char)total;
        memcpy(seg + 14, profile + done, n);
        jpeg_write_marker(cinfo, JPEG_APP0 + 2, seg, (unsigned int)(14 + n));
        free(seg);
        done += n;
        index++;
    }
}

static int go_jpeg_write_cmyk(const unsigned char *ink, int w, int h, int quality,
                              const unsigned char *profile, unsigned long profileLen,
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
    if (profile != NULL && profileLen > 0) {
        go_jpeg_write_icc(&cinfo, profile, profileLen);
    }

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
// go_jpeg_read_icc reassembles the ICC profile from the APP2 ICC_PROFILE
// segments a writer split it into. Segments carry a 1-based index and a total,
// so they are placed by index rather than by the order they appear.
static void go_jpeg_read_icc(struct jpeg_decompress_struct *cinfo,
                             unsigned char **profile, unsigned long *profileLen) {
    jpeg_saved_marker_ptr m;
    unsigned long *lens = NULL;
    unsigned long *offsets = NULL;
    unsigned char *buf = NULL;
    unsigned long size = 0, acc = 0;
    int total = 0, i;

    *profile = NULL;
    *profileLen = 0;

    // 段数写在第 14 个字节（下标 13）。
    for (m = cinfo->marker_list; m != NULL; m = m->next) {
        if (m->marker != JPEG_APP0 + 2 || m->data_length < 14) { continue; }
        if (memcmp(m->data, "ICC_PROFILE\0", 12) != 0) { continue; }
        total = m->data[13];
        if (total > 0) { break; }
    }
    if (total <= 0) { return; }

    for (m = cinfo->marker_list; m != NULL; m = m->next) {
        if (m->marker != JPEG_APP0 + 2 || m->data_length < 14) { continue; }
        if (memcmp(m->data, "ICC_PROFILE\0", 12) != 0) { continue; }
        size += (unsigned long)(m->data_length - 14);
    }
    if (size == 0) { return; }

    lens = (unsigned long *)calloc((size_t)total + 1, sizeof(unsigned long));
    offsets = (unsigned long *)calloc((size_t)total + 1, sizeof(unsigned long));
    buf = (unsigned char *)malloc(size);
    if (lens == NULL || offsets == NULL || buf == NULL) {
        free(lens); free(offsets); free(buf);
        return;
    }
    for (m = cinfo->marker_list; m != NULL; m = m->next) {
        int idx;
        if (m->marker != JPEG_APP0 + 2 || m->data_length < 14) { continue; }
        if (memcmp(m->data, "ICC_PROFILE\0", 12) != 0) { continue; }
        idx = m->data[12];
        if (idx >= 1 && idx <= total) { lens[idx] = (unsigned long)(m->data_length - 14); }
    }
    for (i = 1; i <= total; ++i) { offsets[i] = acc; acc += lens[i]; }
    for (m = cinfo->marker_list; m != NULL; m = m->next) {
        int idx;
        if (m->marker != JPEG_APP0 + 2 || m->data_length < 14) { continue; }
        if (memcmp(m->data, "ICC_PROFILE\0", 12) != 0) { continue; }
        idx = m->data[12];
        if (idx < 1 || idx > total) { continue; }
        memcpy(buf + offsets[idx], m->data + 14, (size_t)(m->data_length - 14));
    }
    free(lens);
    free(offsets);
    *profile = buf;
    *profileLen = size;
}

static int go_jpeg_read_cmyk(const unsigned char *data, unsigned long len,
                             unsigned char **profile, unsigned long *profileLen,
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
    // 保留 APP2，才拿得到内嵌的 ICC 配置。
    jpeg_save_markers(&cinfo, JPEG_APP0 + 2, 0xFFFF);
    jpeg_read_header(&cinfo, TRUE);
    go_jpeg_read_icc(&cinfo, profile, profileLen);
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
	return EncodeCMYKJPEGWithProfile(ink, w, h, quality, nil)
}

// EncodeCMYKJPEGWithProfile is the same encoder with the ICC profile embedded as
// APP2 ICC_PROFILE segments, which is what tells a reader whose inks these are
// (ticket 87's first item asks for the profile, not just four components).
func EncodeCMYKJPEGWithProfile(ink []uint8, w, h, quality int, profile []byte) ([]byte, error) {
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
	var profilePtr *C.uchar
	if len(profile) > 0 {
		profilePtr = (*C.uchar)(unsafe.Pointer(&profile[0]))
	}
	rc := C.go_jpeg_write_cmyk((*C.uchar)(unsafe.Pointer(&ink[0])), C.int(w), C.int(h), C.int(quality),
		profilePtr, C.ulong(len(profile)), &out, &outLen)
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
	ink, w, h, _, err := DecodeCMYKJPEGWithProfile(data)
	return ink, w, h, err
}

// DecodeCMYKJPEGWithProfile is the same reader plus whatever ICC profile the
// file carries in its APP2 ICC_PROFILE segments (nil when it carries none). The
// profile is what says whose inks these are, so a CMYK import without one has to
// ask rather than assume sRGB.
func DecodeCMYKJPEGWithProfile(data []byte) ([]uint8, int, int, []byte, error) {
	if len(data) == 0 {
		return nil, 0, 0, nil, fmt.Errorf("空的 JPEG")
	}
	var out *C.uchar
	var w, h C.int
	var icc *C.uchar
	var iccLen C.ulong
	rc := C.go_jpeg_read_cmyk((*C.uchar)(unsafe.Pointer(&data[0])), C.ulong(len(data)),
		&icc, &iccLen, &out, &w, &h)
	switch rc {
	case 0:
	case 4:
		return nil, 0, 0, nil, fmt.Errorf("不是四分量 JPEG（CMYK JPEG 必须有 4 个分量）")
	default:
		return nil, 0, 0, nil, fmt.Errorf("CMYK JPEG 解码失败（libjpeg 返回 %d）", int(rc))
	}
	defer C.free(unsafe.Pointer(out))
	ink := C.GoBytes(unsafe.Pointer(out), w*h*4)
	// 写的时候逐字节取反，读的时候反回来。
	for i := range ink {
		ink[i] = 255 - ink[i]
	}
	var profile []byte
	if icc != nil && iccLen > 0 {
		profile = C.GoBytes(unsafe.Pointer(icc), C.int(iccLen))
		C.free(unsafe.Pointer(icc))
	}
	return ink, int(w), int(h), profile, nil
}
