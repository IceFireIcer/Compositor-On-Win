// rawbridge.cpp — the LibRaw (LGPL-2.1) side of the RAW develop seam.
// Threads: one RB_Session per caller session; LibRaw itself is used from
// the single goroutine that owns the develop sheet (the original kept one
// actor Queue for the same reason).
#include "rawbridge.h"

#include <libraw/libraw.h>
#include <cstdio>
#include <cstdlib>
#include <cstring>

struct RB_Session {
    LibRaw* raw;
};

static thread_local char g_error[512] = {0};

static void set_error(const char* what, int code) {
    std::snprintf(g_error, sizeof(g_error), "%s (LibRaw %d: %s)", what, code, LibRaw::strerror(code));
}

extern "C" {

int rb_open(const char* path, int halfSize, RB_Session** out,
            int* width, int* height, double* camMul) {
    *out = nullptr;
    g_error[0] = '\0';
    LibRaw* raw = new LibRaw();
    libraw_output_params_t& p = raw->imgdata.params;
    p.use_camera_wb = 1;
    p.no_auto_bright = 1;
    p.output_bps = 16;
    p.output_color = 1; // sRGB matrix
    p.gamm[0] = 1.0;    // linear output: the Go develop pass spends tone
    p.gamm[1] = 1.0;
    p.half_size = halfSize ? 1 : 0;
    int rc = raw->open_file(path);
    if (rc != LIBRAW_SUCCESS) {
        set_error("打开 RAW 失败", rc);
        delete raw;
        return rc;
    }
    rc = raw->unpack();
    if (rc != LIBRAW_SUCCESS) {
        set_error("RAW 解包失败", rc);
        raw->recycle();
        delete raw;
        return rc;
    }
    *width = raw->imgdata.sizes.width;
    *height = raw->imgdata.sizes.height;
    camMul[0] = raw->imgdata.color.cam_mul[0];
    camMul[1] = raw->imgdata.color.cam_mul[1];
    camMul[2] = raw->imgdata.color.cam_mul[2];
    *out = new RB_Session{raw};
    return LIBRAW_SUCCESS;
}

int rb_develop(RB_Session* s, const double* mul4, int useCameraWb,
               unsigned short** rgb, int* width, int* height) {
    if (s == nullptr || s->raw == nullptr) {
        std::snprintf(g_error, sizeof(g_error), "会话未打开");
        return LIBRAW_UNSUFFICIENT_MEMORY;
    }
    g_error[0] = '\0';
    libraw_output_params_t& p = s->raw->imgdata.params;
    p.use_camera_wb = useCameraWb ? 1 : 0;
    p.use_auto_wb = 0;
    if (!useCameraWb) {
        p.user_mul[0] = mul4[0];
        p.user_mul[1] = mul4[1];
        p.user_mul[2] = mul4[2];
        p.user_mul[3] = mul4[3];
    }
    int rc = s->raw->dcraw_process();
    if (rc != LIBRAW_SUCCESS) {
        set_error("RAW 显影失败", rc);
        return rc;
    }
    int memRc = 0;
    libraw_processed_image_t* img = s->raw->dcraw_make_mem_image(&memRc);
    if (img == nullptr) {
        set_error("RAW 内存映像失败", memRc);
        return memRc;
    }
    const size_t n = static_cast<size_t>(img->width) * img->height * img->colors;
    auto* copy = static_cast<unsigned short*>(std::malloc(n * 2));
    if (copy == nullptr) {
        LibRaw::dcraw_clear_mem(img);
        return LIBRAW_UNSUFFICIENT_MEMORY;
    }
    std::memcpy(copy, img->data, n * 2);
    *width = img->width;
    *height = img->height;
    *rgb = copy;
    LibRaw::dcraw_clear_mem(img);
    return LIBRAW_SUCCESS;
}

void rb_free(unsigned short* p) { std::free(p); }

void rb_close(RB_Session* s) {
    if (s == nullptr) return;
    if (s->raw != nullptr) {
        s->raw->recycle();
        delete s->raw;
    }
    delete s;
}

const char* rb_last_error(void) { return g_error; }
}
