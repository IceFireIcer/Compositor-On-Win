#include "clipbridge.h"

#define WIN32_LEAN_AND_MEAN
#include <windows.h>

int clip_put_dib(const unsigned char* data, unsigned long size) {
    if (!OpenClipboard(nullptr)) return 1;
    const int code = [data, size]() -> int {
        if (!EmptyClipboard()) return 2;
        HGLOBAL mem = GlobalAlloc(GMEM_MOVEABLE, size);
        if (!mem) return 2;
        void* p = GlobalLock(mem);
        if (!p) {
            GlobalFree(mem);
            return 2;
        }
        memcpy(p, data, size);
        GlobalUnlock(mem);
        // Ownership of mem passes to the clipboard on success.
        if (!SetClipboardData(CF_DIB, mem)) {
            GlobalFree(mem);
            return 2;
        }
        return 0;
    }();
    CloseClipboard();
    return code;
}

/* The cap is the only authority: nothing is written past it. A too-small
 * buffer reports the size needed (code 3) and copies nothing, so the caller
 * reallocates and reads again. GlobalSize may exceed the real DIB payload
 * (allocators round up); the extra tail bytes are ignored by the Go decoder,
 * whose header checks decide where the pixels actually end. */
int clip_get_data(const char* name, unsigned char* buf, unsigned long cap, unsigned long* out_size) {
    if (!out_size) return 2;
    *out_size = 0;
    UINT format = CF_DIB;
    if (name != nullptr) {
        format = RegisterClipboardFormatA(name);
        if (format == 0) return 2;
    }
    if (!IsClipboardFormatAvailable(format)) return 2;
    if (!OpenClipboard(nullptr)) return 1;
    int code = 2;
    HANDLE handle = GetClipboardData(format);
    if (handle) {
        const void* p = GlobalLock(handle);
        if (p) {
            SIZE_T size = GlobalSize(handle);
            if (size > cap) {
                *out_size = (unsigned long)size;
                code = 3;
            } else {
                memcpy(buf, p, size);
                *out_size = (unsigned long)size;
                code = 0;
            }
            GlobalUnlock(handle);
        }
    }
    CloseClipboard();
    return code;
}

unsigned long clip_sequence(void) {
    return (unsigned long)GetClipboardSequenceNumber();
}
