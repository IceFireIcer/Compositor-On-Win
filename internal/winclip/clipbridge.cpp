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
