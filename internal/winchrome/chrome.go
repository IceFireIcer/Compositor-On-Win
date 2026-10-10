// Package winchrome 提供 Windows 外壳（Shell）集成：任务栏跳转列表的"最近文档"
// 记录，以及脏文档的任务栏覆盖图标。这是票 49「Shell 集成」的 Windows 外壳部分。
//
// 和 internal/winclip 同一套路：Win32/COM 的原始指针活在 chromebridge.cpp 里，
// Go 侧只做参数校验与错误码翻译。外壳 API 失败只返回错误，绝不 panic——外壳坏掉
// 不能拖垮编辑器。
//
// 纯逻辑（路径规范化、去重、上限、外壳调用的等待闸门）留在本文件，任何平台都能
// 编译和测试；真正的 Win32 调用在 chrome_windows.go，该文件带 windows 构建约束，
// 所以非 Windows 上 chromebridge.cpp 不会被 cgo 编译，三个入口函数在这里就按
// no-op 返回。
package winchrome

import (
	"errors"
	"runtime"
	"strings"
	"time"
)

// MaxJumpListEntries 是跳转列表"最近"分类里保留的条目上限：Windows 自己只显示
// 十条左右，多记没有意义。
const MaxJumpListEntries = 10

// shellCallTimeout 是等待一次外壳调用的上限。Win32/COM 的外壳调用是跨进程同步
// 调用——SetOverlayIcon 必须跟任务栏（explorer.exe）对话。实测在真实窗口上它会
// 卡住数秒，最长一次超过一分钟（同一进程里 CoInitializeEx、CoCreateInstance、
// HrInit、GDI 建图标都瞬间返回，卡的就是 SetOverlayIcon 本身）。编辑器绝不能被
// 外壳拖住，所以每次调用都只等这么久。
const shellCallTimeout = 2 * time.Second

var (
	errShellBusy    = errors.New("上一次外壳调用仍未返回，已跳过本次调用")
	errShellTimeout = errors.New("外壳没有响应，已放弃本次调用")
)

// shellGate 保证同一时刻只有一个外壳调用在飞。卡住的那次会一直占着名额，直到它
// 真的返回为止；在那之前其他调用一律立刻失败，所以最多只有一个线程被外壳拖住，
// 而且外壳恢复后不用重启程序就能继续用。
type shellGate struct {
	slot chan struct{}
}

func newShellGate() *shellGate {
	return &shellGate{slot: make(chan struct{}, 1)}
}

// call 在独立 goroutine 里执行一次外壳调用，最多等 timeout。fn 返回 C 侧的错误码。
// 超时后调用者立刻拿到错误，但那次调用还在后台跑；名额由它自己返回时释放。
func (g *shellGate) call(timeout time.Duration, fn func() int) (int, error) {
	select {
	case g.slot <- struct{}{}:
	default:
		return 0, errShellBusy
	}
	done := make(chan int, 1)
	go func() {
		code := fn()
		<-g.slot // 先还名额再交付结果，免得调用者收到结果后立刻又撞上"忙"
		done <- code
	}()
	select {
	case code := <-done:
		return code, nil
	case <-time.After(timeout):
		return 0, errShellTimeout
	}
}

// 两把**独立**的闸门：覆盖图标与"最近文档"互不牵连。SetOverlayIcon 卡住时
// （真机实测可达一分钟）最近文档的调用照常进行——否则菜单里的最近列表与任务栏
// 跳转列表会长期不同步，而那种失败只落在一行日志里，没人会注意到。
var (
	overlayGate = newShellGate()
	recentGate  = newShellGate()
)

// Supported 报告当前平台是否提供 Windows 外壳 API（跳转列表、任务栏覆盖图标）。
func Supported() bool {
	return runtime.GOOS == "windows"
}

// JumpListPaths 把候选路径整理成可以交给外壳的最近文档列表：逐项去掉首尾空白、
// 丢掉空项、去重（保留首次出现的写法）、保持原顺序，最后截到 MaxJumpListEntries。
//
// 去重只在 Windows 上大小写不敏感（NTFS 的常态，也是外壳归类最近文档时的行为）；
// 其他平台按大小写敏感处理，好让规则本身在 CI 的 Linux/macOS 上也能测。路径本身
// 不在这里做 filepath.Clean：外壳拿到什么就记什么，调用方给的就是规范形式。
func JumpListPaths(paths []string) []string {
	out := make([]string, 0, min(len(paths), MaxJumpListEntries))
	seen := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}
		key := path
		if runtime.GOOS == "windows" {
			key = strings.ToLower(path)
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, path)
		if len(out) == MaxJumpListEntries {
			break
		}
	}
	return out
}

// NoteJumpListPath 把一个文件记进 Windows 外壳的最近文档
// （SHAddToRecentDocs(SHARD_PATHW, path)），Windows 会把它放进本应用的任务栏
// 跳转列表。空路径返回错误；非 Windows 是 no-op。
func NoteJumpListPath(path string) error {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return errors.New("最近文档路径为空")
	}
	if !Supported() {
		return nil
	}
	return noteRecent(trimmed)
}

// ClearJumpList 清空本应用记在外壳里的最近文档
// （SHAddToRecentDocs(SHARD_PIDL, NULL)）。非 Windows 是 no-op。
func ClearJumpList() error {
	if !Supported() {
		return nil
	}
	return clearRecent()
}

// SetDirtyOverlay 在进程主窗口上显示（on 为 true）或清除（on 为 false）任务栏
// 覆盖图标：一个 16×16 的红色圆点，表示文档有未保存的更改。找不到主窗口或
// ITaskbarList3（窗口还没建好、Windows 7 以下）时返回错误，但绝不 panic，也绝不
// 让调用者等超过 shellCallTimeout。非 Windows 是 no-op。
func SetDirtyOverlay(on bool) error {
	if !Supported() {
		return nil
	}
	return setOverlay(on)
}
