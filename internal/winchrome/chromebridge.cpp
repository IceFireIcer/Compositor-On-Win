#include "chromebridge.h"

#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <shlobj.h>   /* SHAddToRecentDocs / SHARD_* */
#include <shobjidl.h> /* ITaskbarList3 */

#include <cmath>
#include <mutex>

namespace {

/* EnumWindows 回调：本进程第一个"可见且标题非空"的顶层窗口就是应用主窗口。
 * WebView2 会造一串不可见的辅助窗口，标题也为空，这两个条件把它们挡在外面。 */
struct WindowSearch {
    DWORD pid;
    HWND found;
};

BOOL CALLBACK find_main_window(HWND hwnd, LPARAM param) {
    WindowSearch* search = reinterpret_cast<WindowSearch*>(param);
    DWORD pid = 0;
    GetWindowThreadProcessId(hwnd, &pid);
    if (pid != search->pid) return TRUE;
    if (!IsWindowVisible(hwnd)) return TRUE;
    if (GetWindowTextLengthW(hwnd) <= 0) return TRUE;
    search->found = hwnd;
    return FALSE; /* 找到就停 */
}

/* 16x16 的红色圆点：预乘 BGRA，边缘一像素羽化，其余像素全透明。 */
HICON create_dot_icon() {
    const int kSize = 16;
    BITMAPINFO info = {};
    info.bmiHeader.biSize = sizeof(BITMAPINFOHEADER);
    info.bmiHeader.biWidth = kSize;
    info.bmiHeader.biHeight = -kSize; /* 负数 = 自上而下 */
    info.bmiHeader.biPlanes = 1;
    info.bmiHeader.biBitCount = 32;
    info.bmiHeader.biCompression = BI_RGB;
    void* bits = nullptr;
    HBITMAP color = CreateDIBSection(nullptr, &info, DIB_RGB_COLORS, &bits, nullptr, 0);
    if (color == nullptr || bits == nullptr) {
        if (color) DeleteObject(color);
        return nullptr;
    }
    const double center = (kSize - 1) / 2.0;
    const double radius = 7.0;
    unsigned int* pixels = static_cast<unsigned int*>(bits);
    for (int y = 0; y < kSize; ++y) {
        for (int x = 0; x < kSize; ++x) {
            const double dx = x - center;
            const double dy = y - center;
            double coverage = radius - std::sqrt(dx * dx + dy * dy) + 0.5;
            if (coverage <= 0.0) {
                pixels[y * kSize + x] = 0; /* 全透明 */
                continue;
            }
            if (coverage > 1.0) coverage = 1.0;
            const unsigned int a = static_cast<unsigned int>(coverage * 255.0 + 0.5);
            /* 预乘的纯红 (255,0,0) → BGRA = (0, 0, a, a) */
            pixels[y * kSize + x] = (a << 24) | (a << 16);
        }
    }
    /* 32bpp 图标以 alpha 为准，单色 AND 掩码的内容会被忽略；MSDN 不保证
     * CreateBitmap(bits=NULL) 的内容，所以显式清零一次（16 个像素，代价可忽略）。 */
    HBITMAP mask = CreateBitmap(kSize, kSize, 1, 1, nullptr);
    if (mask == nullptr) {
        DeleteObject(color);
        return nullptr;
    }
    {
        /* 单色位图：CreateBitmap 的 bits 为空时内容未定义，用 memory DC 清一次。 */
        HDC dc = CreateCompatibleDC(nullptr);
        if (dc != nullptr) {
            HGDIOBJ old = SelectObject(dc, mask);
            RECT rect = {0, 0, kSize, kSize};
            FillRect(dc, &rect, static_cast<HBRUSH>(GetStockObject(BLACK_BRUSH)));
            SelectObject(dc, old);
            DeleteDC(dc);
        }
    }
    ICONINFO icon_info = {};
    icon_info.fIcon = TRUE;
    icon_info.hbmColor = color;
    icon_info.hbmMask = mask;
    HICON icon = CreateIconIndirect(&icon_info); /* 会复制位图，之后可删 */
    DeleteObject(color);
    DeleteObject(mask);
    return icon;
}

/* 图标只建一次并缓存：句柄随进程存活（退出时由系统回收），避免每次脏标记切换
 * 都重新分配 GDI 对象。std::call_once 让并发调用也只建一次。 */
HICON overlay_icon() {
    static std::once_flag once;
    static HICON icon = nullptr;
    std::call_once(once, []() { icon = create_dot_icon(); });
    return icon;
}

}  // namespace

int chrome_note_recent(const wchar_t* path) {
    if (path == nullptr || path[0] == L'\0') return CHROME_ERR_PATH;
    SHAddToRecentDocs(SHARD_PATHW, path);
    return CHROME_OK;
}

int chrome_clear_recent(void) {
    /* SHARD_PIDL + NULL 是"清空本应用的最近文档"的官方写法。 */
    SHAddToRecentDocs(SHARD_PIDL, nullptr);
    return CHROME_OK;
}

int chrome_set_overlay(int on) {
    WindowSearch search = {GetCurrentProcessId(), nullptr};
    EnumWindows(find_main_window, reinterpret_cast<LPARAM>(&search));
    if (search.found == nullptr) return CHROME_ERR_WINDOW;

    /* STA 与 Wails/WebView2 一致。RPC_E_CHANGED_MODE 说明这个线程已经用别的
     * 单元模型初始化过 COM——CoCreateInstance 照样能用，只是不该由我们反初始化。 */
    const HRESULT init = CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);
    const bool own_apartment = SUCCEEDED(init);

    ITaskbarList3* taskbar = nullptr;
    HRESULT hr = CoCreateInstance(CLSID_TaskbarList, nullptr, CLSCTX_INPROC_SERVER,
                                  IID_ITaskbarList3, reinterpret_cast<void**>(&taskbar));
    if (FAILED(hr) || taskbar == nullptr) {
        if (own_apartment) CoUninitialize();
        return CHROME_ERR_TASKBAR;
    }
    hr = taskbar->HrInit();
    if (FAILED(hr)) {
        taskbar->Release();
        if (own_apartment) CoUninitialize();
        return CHROME_ERR_TASKBAR;
    }

    int code = CHROME_OK;
    if (on) {
        HICON icon = overlay_icon();
        if (icon == nullptr) {
            code = CHROME_ERR_ICON;
        } else if (FAILED(taskbar->SetOverlayIcon(search.found, icon, L"未保存的更改"))) {
            code = CHROME_ERR_OVERLAY;
        }
    } else if (FAILED(taskbar->SetOverlayIcon(search.found, nullptr, nullptr))) {
        code = CHROME_ERR_OVERLAY;
    }

    taskbar->Release();
    if (own_apartment) CoUninitialize();
    return code;
}
