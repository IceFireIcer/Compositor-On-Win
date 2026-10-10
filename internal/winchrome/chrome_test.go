package winchrome

import (
	"errors"
	"runtime"
	"testing"
	"time"
)

// equalPaths 把 nil 和空切片视作相等——JumpListPaths 返回的是非 nil 空切片，
// 测试里希望两者都能写。
func equalPaths(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestMaxJumpListEntries(t *testing.T) {
	if MaxJumpListEntries != 10 {
		t.Fatalf("MaxJumpListEntries = %d, 期望 10", MaxJumpListEntries)
	}
}

func TestSupported(t *testing.T) {
	want := runtime.GOOS == "windows"
	if got := Supported(); got != want {
		t.Fatalf("Supported() = %v, 期望 %v（GOOS=%s）", got, want, runtime.GOOS)
	}
	if runtime.GOOS == "windows" && !Supported() {
		t.Fatal("本机是 Windows，Supported() 必须为 true")
	}
}

func TestJumpListPaths(t *testing.T) {
	ten := []string{
		"C:\\work\\01.comp", "C:\\work\\02.comp", "C:\\work\\03.comp",
		"C:\\work\\04.comp", "C:\\work\\05.comp", "C:\\work\\06.comp",
		"C:\\work\\07.comp", "C:\\work\\08.comp", "C:\\work\\09.comp",
		"C:\\work\\10.comp",
	}
	eleven := append(append([]string{}, ten...), "C:\\work\\11.comp")

	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"nil 输入 → 空", nil, nil},
		{"空切片 → 空", []string{}, nil},
		{"空串与纯空白全部丢掉", []string{"", "   ", "\t\n", "  \r\n "}, nil},
		{"去首尾空白后保留", []string{"  C:\\work\\a.comp\t", "\nC:\\work\\b.comp "}, []string{"C:\\work\\a.comp", "C:\\work\\b.comp"}},
		{"保持原顺序", []string{"C:\\work\\b.comp", "C:\\work\\a.comp", "C:\\work\\c.comp"}, []string{"C:\\work\\b.comp", "C:\\work\\a.comp", "C:\\work\\c.comp"}},
		{"完全重复只留第一次", []string{"C:\\work\\a.comp", "C:\\work\\a.comp", "C:\\work\\b.comp", "C:\\work\\a.comp"}, []string{"C:\\work\\a.comp", "C:\\work\\b.comp"}},
		{"空白差异也算重复", []string{" C:\\work\\a.comp", "C:\\work\\a.comp "}, []string{"C:\\work\\a.comp"}},
		{"恰好 10 条不截断", ten, ten},
		{"11 条截到前 10", eleven, ten},
		{"去重先于截断（重复不占名额）", append([]string{"C:\\work\\a.comp", "C:\\work\\a.comp"}, eleven[1:]...), append([]string{"C:\\work\\a.comp"}, eleven[1:10]...)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := JumpListPaths(tc.in)
			if !equalPaths(got, tc.want) {
				t.Fatalf("JumpListPaths(%q) = %q, 期望 %q", tc.in, got, tc.want)
			}
			if len(got) > MaxJumpListEntries {
				t.Fatalf("结果 %d 条，超过上限 %d", len(got), MaxJumpListEntries)
			}
		})
	}
}

// 大小写去重只在 Windows 生效：NTFS 的常态，也是外壳归类最近文档时的行为。
// 非 Windows 上按大小写敏感处理，规则本身仍可测。
func TestJumpListPathsCaseFolding(t *testing.T) {
	in := []string{"C:\\work\\Image.comp", "c:\\WORK\\image.COMP"}
	got := JumpListPaths(in)
	if runtime.GOOS == "windows" {
		if len(got) != 1 || got[0] != "C:\\work\\Image.comp" {
			t.Fatalf("Windows 上应大小写不敏感去重并保留首次写法，得到 %q", got)
		}
		return
	}
	if len(got) != 2 {
		t.Fatalf("非 Windows 上应保留两条大小写不同的路径，得到 %q", got)
	}
}

func TestJumpListPathsDoesNotMutateInput(t *testing.T) {
	in := []string{"  C:\\work\\a.comp  ", "C:\\work\\a.comp", ""}
	JumpListPaths(in)
	if in[0] != "  C:\\work\\a.comp  " || in[2] != "" {
		t.Fatalf("输入切片被改动了: %q", in)
	}
}

// 空路径在任何平台上都是错误，且在碰外壳之前就返回。
func TestNoteJumpListPathEmptyPath(t *testing.T) {
	for _, path := range []string{"", "   ", "\t\r\n"} {
		if err := NoteJumpListPath(path); err == nil {
			t.Fatalf("NoteJumpListPath(%q) 应返回错误", path)
		}
	}
}

// 闸门正常路径：把 C 侧返回码原样带出来。
func TestShellGateReturnsCode(t *testing.T) {
	gate := newShellGate()
	code, err := gate.call(time.Second, func() int { return 7 })
	if err != nil || code != 7 {
		t.Fatalf("call() = (%d, %v)，期望 (7, nil)", code, err)
	}
}

// 卡住的外壳调用不能拖住调用者：超时立刻返回错误。
func TestShellGateTimeout(t *testing.T) {
	gate := newShellGate()
	blocked := make(chan struct{})
	defer close(blocked)
	start := time.Now()
	_, err := gate.call(30*time.Millisecond, func() int {
		<-blocked
		return 0
	})
	if !errors.Is(err, errShellTimeout) {
		t.Fatalf("卡住的调用应超时，得到 %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("超时不该等这么久: %v", elapsed)
	}
}

// 卡住的那次仍占着名额，后续调用立刻失败（最多只有一个线程被拖住）。
func TestShellGateBusyAfterTimeout(t *testing.T) {
	gate := newShellGate()
	blocked := make(chan struct{})
	defer close(blocked)
	if _, err := gate.call(30*time.Millisecond, func() int {
		<-blocked
		return 0
	}); !errors.Is(err, errShellTimeout) {
		t.Fatalf("第一次调用应超时，得到 %v", err)
	}
	called := make(chan struct{}, 1)
	if _, err := gate.call(time.Second, func() int {
		called <- struct{}{}
		return 0
	}); !errors.Is(err, errShellBusy) {
		t.Fatalf("名额被占时应用 errShellBusy 快速失败，得到 %v", err)
	}
	select {
	case <-called:
		t.Fatal("名额被占时不该真的执行外壳调用")
	default:
	}
}

// 正常返回会还回名额，下一个调用照常工作。
func TestShellGateReleasesSlot(t *testing.T) {
	gate := newShellGate()
	if _, err := gate.call(time.Second, func() int { return 1 }); err != nil {
		t.Fatalf("第一次调用不该失败: %v", err)
	}
	code, err := gate.call(time.Second, func() int { return 2 })
	if err != nil || code != 2 {
		t.Fatalf("第二次 call() = (%d, %v)，期望 (2, nil)", code, err)
	}
}

// 调用在飞时，并发调用拿到 errShellBusy；第一次返回后名额恢复。
func TestShellGateBusyWhileInFlight(t *testing.T) {
	gate := newShellGate()
	release := make(chan struct{})
	started := make(chan struct{})
	first := make(chan int, 1)
	go func() {
		code, err := gate.call(time.Second, func() int {
			close(started)
			<-release
			return 1
		})
		if err != nil {
			first <- -1
			return
		}
		first <- code
	}()
	<-started

	called := make(chan struct{}, 1)
	if _, err := gate.call(50*time.Millisecond, func() int {
		called <- struct{}{}
		return 0
	}); !errors.Is(err, errShellBusy) {
		t.Fatalf("在飞时应用 errShellBusy，得到 %v", err)
	}
	select {
	case <-called:
		t.Fatal("在飞时不该真的执行第二次外壳调用")
	default:
	}

	close(release)
	if code := <-first; code != 1 {
		t.Fatalf("第一次调用应返回 1，得到 %d", code)
	}
	if code, err := gate.call(time.Second, func() int { return 3 }); err != nil || code != 3 {
		t.Fatalf("名额恢复后 call() = (%d, %v)，期望 (3, nil)", code, err)
	}
}

// 非 Windows：三个入口都是 no-op，返回 nil 而不是错误（外壳失败不该影响编辑器）。
func TestNonWindowsIsNoop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("仅在非 Windows 上验证 no-op 语义")
	}
	if Supported() {
		t.Fatal("非 Windows 上 Supported() 应为 false")
	}
	if err := NoteJumpListPath("C:\\work\\a.comp"); err != nil {
		t.Fatalf("NoteJumpListPath 应为 no-op，得到 %v", err)
	}
	if err := ClearJumpList(); err != nil {
		t.Fatalf("ClearJumpList 应为 no-op，得到 %v", err)
	}
	if err := SetDirtyOverlay(true); err != nil {
		t.Fatalf("SetDirtyOverlay(true) 应为 no-op，得到 %v", err)
	}
	if err := SetDirtyOverlay(false); err != nil {
		t.Fatalf("SetDirtyOverlay(false) 应为 no-op，得到 %v", err)
	}
}

// Windows 上真调一次 C 侧：go test 的控制台窗口属于 conhost，不属于本进程，
// 所以本进程没有"可见且有标题"的顶层窗口，SetOverlayIcon 必然走到
// CHROME_ERR_WINDOW（或任务栏不可用）分支——只要不 panic、返回错误即可。
func TestSetDirtyOverlayWithoutWindow(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("仅在 Windows 上跑真实的 Win32 路径")
	}
	if err := SetDirtyOverlay(true); err == nil {
		t.Fatal("测试进程没有主窗口，SetDirtyOverlay(true) 应返回错误")
	}
	if err := SetDirtyOverlay(false); err == nil {
		t.Fatal("测试进程没有主窗口，SetDirtyOverlay(false) 应返回错误")
	}
}

// 清空最近文档没有失败路径（SHAddToRecentDocs 返回 void），Windows 上真调一次
// 只影响测试程序自己的最近列表，用来确认 C 侧确实可达。
func TestClearJumpListWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("仅在 Windows 上跑真实的 Win32 路径")
	}
	if err := ClearJumpList(); err != nil {
		t.Fatalf("ClearJumpList 应成功，得到 %v", err)
	}
}
