//go:build windows

package winchrome

import (
	"errors"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var (
	testUser32             = syscall.NewLazyDLL("user32.dll")
	procCreateWindowExTest = testUser32.NewProc("CreateWindowExW")
	procDestroyWindowTest  = testUser32.NewProc("DestroyWindow")
)

// createNotYetShownMainWindow 造一个"已建好但还没显示"的主窗口，形态与 Wails 在
// WebView2 导航完成前的主窗口一致：顶层、有标题、无属主、带 WS_EX_APPWINDOW、
// 不带 WS_VISIBLE。
func createNotYetShownMainWindow(t *testing.T) uintptr {
	t.Helper()
	class, err := syscall.UTF16PtrFromString("STATIC")
	if err != nil {
		t.Fatalf("窗口类名转 UTF-16 失败: %v", err)
	}
	title, err := syscall.UTF16PtrFromString("Compositor")
	if err != nil {
		t.Fatalf("窗口标题转 UTF-16 失败: %v", err)
	}
	const (
		wsExAppWindow      = 0x00040000 // WS_EX_APPWINDOW
		wsOverlappedWindow = 0x00CF0000 // WS_OVERLAPPEDWINDOW（不含 WS_VISIBLE）
	)
	hwnd, _, callErr := procCreateWindowExTest.Call(
		wsExAppWindow,
		uintptr(unsafe.Pointer(class)),
		uintptr(unsafe.Pointer(title)),
		wsOverlappedWindow,
		0, 0, 200, 120,
		0, 0, 0, 0,
	)
	if hwnd == 0 {
		t.Fatalf("CreateWindowExW 失败: %v", callErr)
	}
	vis, _, _ := testUser32.NewProc("IsWindowVisible").Call(hwnd)
	if vis != 0 {
		t.Fatalf("测试窗口不该是可见的（IsWindowVisible=%d）", vis)
	}
	return hwnd
}

// 回归（真机实测的启动告警）：Wails 的主窗口在 WebView2 导航完成之前是隐藏的
// （ShowWindow 在 navigationCompleted 回调里才调），而前端在挂载时就会同步一次
// 窗口外壳，所以每次启动都必然出现
//
//	任务栏覆盖图标不可用: 找不到主窗口，无法设置任务栏覆盖图标
//
// 只认"可见"窗口的搜索把这一瞬当成了"没有主窗口"。这里造一个同形态的隐藏窗口，
// 断言 SetDirtyOverlay 认得出"窗口已建好、只是还没显示"（返回 nil），而不是
// errNoMainWindow；也不该去调 SetOverlayIcon —— 真机实测：对一个还没有任务栏
// 按钮的窗口调 SetOverlayIcon 会一直不返回，白白占住外壳闸门 2 秒并打一行
// "外壳没有响应"的告警。
func TestSetDirtyOverlayFindsNotYetShownMainWindow(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("仅在 Windows 上跑真实的 Win32 路径")
	}
	// CreateWindowExW/DestroyWindow 都要求同一个 OS 线程，而 Go 的 goroutine
	// 会在线程间迁移。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hwnd := createNotYetShownMainWindow(t)
	// LazyProc.Call 的第三个返回值永远是装箱后的 Errno，成功时是 Errno(0)
	// （.Error() == "The operation completed successfully."），所以这里不看它。
	defer procDestroyWindowTest.Call(hwnd)

	start := time.Now()
	err := SetDirtyOverlay(false)
	if err != nil {
		t.Fatalf("窗口已建好但还没显示时应静默返回 nil，得到 %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("隐藏主窗口上不该真去调 SetOverlayIcon（会卡住），耗时 %v", elapsed)
	}
}

// 反面：没有任何候选窗口时仍然是 errNoMainWindow（测试进程里没有别的顶层窗口，
// 所以这里先把上一个测试留下的窗口销毁干净再断言）。这条守住的是"退到隐藏窗口"
// 没有退化成"随便抓一个窗口"。
func TestSetDirtyOverlayWithoutAnyWindow(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("仅在 Windows 上跑真实的 Win32 路径")
	}
	if err := SetDirtyOverlay(false); err != nil && !errors.Is(err, errNoMainWindow) {
		t.Fatalf("没有候选主窗口时应报 errNoMainWindow 或任务栏不可用，得到 %v", err)
	}
}
