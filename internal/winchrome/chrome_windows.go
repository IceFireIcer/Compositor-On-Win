//go:build windows

package winchrome

/*
#cgo windows LDFLAGS: -lole32 -luuid -lshell32 -luser32 -lgdi32
#include <stdlib.h>
#include "chromebridge.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// noteRecent 走 SHAddToRecentDocs(SHARD_PATHW, path)。路径转 UTF-16 由 syscall
// 完成，闭包持有切片所以指针在 C 调用期间一直有效；C 侧不打印任何东西，返回值
// 就是错误分类。
func noteRecent(path string) error {
	wide, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return fmt.Errorf("最近文档路径非法: %w", err)
	}
	code, err := recentGate.call(shellCallTimeout, func() int {
		return int(C.chrome_note_recent((*C.wchar_t)(unsafe.Pointer(wide))))
	})
	if err != nil {
		return err
	}
	switch code {
	case C.CHROME_OK:
		return nil
	case C.CHROME_ERR_PATH:
		return errors.New("最近文档路径为空")
	default:
		return errors.New("记录最近文档失败")
	}
}

// clearRecent 走 SHAddToRecentDocs(SHARD_PIDL, NULL)：清空本应用的最近文档。
func clearRecent() error {
	code, err := recentGate.call(shellCallTimeout, func() int {
		return int(C.chrome_clear_recent())
	})
	if err != nil {
		return err
	}
	switch code {
	case C.CHROME_OK:
		return nil
	default:
		return errors.New("清空最近文档失败")
	}
}

// setOverlay 走 ITaskbarList3::SetOverlayIcon；主窗口由 C 侧 EnumWindows 自己找。
func setOverlay(on bool) error {
	flag := C.int(0)
	if on {
		flag = 1
	}
	code, err := overlayGate.call(shellCallTimeout, func() int {
		return int(C.chrome_set_overlay(flag))
	})
	if err != nil {
		return err
	}
	switch code {
	case C.CHROME_OK:
		return nil
	case C.CHROME_ERR_NOT_SHOWN:
		// 窗口已建好但还没显示：Wails 在 WebView2 导航完成之后才 ShowWindow，而
		// 前端挂载时就会同步一次窗口外壳，所以每次启动都会走到这里。这一刻没有
		// 任务栏按钮可画，不是失败；等窗口显示后的下一次同步（文档名/脏标记一变
		// 前端就会调）自然生效。
		return nil
	case C.CHROME_ERR_WINDOW:
		return errNoMainWindow
	case C.CHROME_ERR_TASKBAR:
		return errors.New("任务栏接口不可用（需要 Windows 7 及以上）")
	case C.CHROME_ERR_ICON:
		return errors.New("创建覆盖图标失败")
	case C.CHROME_ERR_OVERLAY:
		return errors.New("设置任务栏覆盖图标失败")
	default:
		return errors.New("设置任务栏覆盖图标失败")
	}
}
