package bridge

// Shell 集成的测试（票 49）：外壳递来的路径（启动参数 / 第二次启动转发）、
// 最近项目菜单、窗口标题与任务栏脏标记。外壳副作用走 shellPlane 接缝的假实现，
// 测试不动真窗口、不写用户配置目录。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"compositor-win/internal/project"
	"compositor-win/internal/render"
)

// fakeShell 记录外壳副作用，代替窗口/任务栏。
type fakeShell struct {
	titles      []string
	overlays    []bool
	notedRecent []string
	cleared     int
	overlayErr  error
	raised      int
	emitted     []int
	// 通用事件（名字 → 负载）。外部变更的事件由 coordinator 自己的 goroutine 推，
	// 测试在另一个 goroutine 上读，所以这把锁是必须的（-race 会报）。
	mu     sync.Mutex
	events map[string][]string
}

func (f *fakeShell) Emit(_ context.Context, name string, payload string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.events == nil {
		f.events = map[string][]string{}
	}
	f.events[name] = append(f.events[name], payload)
}

// eventPayloads 读某个事件收到的全部负载（按顺序）。
func (f *fakeShell) eventPayloads(name string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events[name]...)
}

// lastEvent 读某个事件最后一条负载（"" = 没收到）。
func (f *fakeShell) lastEvent(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := f.events[name]
	if len(all) == 0 {
		return ""
	}
	return all[len(all)-1]
}

func (f *fakeShell) SetTitle(_ context.Context, title string) { f.titles = append(f.titles, title) }
func (f *fakeShell) SetDirtyOverlay(on bool) error {
	f.overlays = append(f.overlays, on)
	return f.overlayErr
}
func (f *fakeShell) NoteRecentDocument(path string) error {
	f.notedRecent = append(f.notedRecent, path)
	return nil
}
func (f *fakeShell) ClearRecentDocuments() error { f.cleared++; return nil }
func (f *fakeShell) RaiseWindow(context.Context) { f.raised++ }
func (f *fakeShell) EmitOpenPaths(_ context.Context, queued int) {
	f.emitted = append(f.emitted, queued)
}

// useShell 换掉外壳接缝并在测试结束复原。
func useShell(t *testing.T) *fakeShell {
	t.Helper()
	fake := &fakeShell{}
	saved := shell
	shell = fake
	t.Cleanup(func() { shell = saved })
	return fake
}

// useRecent 给服务装一个临时目录里的最近项目列表（生产由 main.go 装）。
func useRecent(t *testing.T, svc *Service) *RecentStore {
	t.Helper()
	store := NewRecentStoreAt(filepath.Join(t.TempDir(), "recent.json"))
	NewShellHooks(svc).EnableRecentFiles(store)
	return store
}

// saveActiveTo 用 stub 的保存对话框把当前文档落到一个真实 .comp（供"外壳打开"
// 用真实包）。
func saveActiveTo(t *testing.T, svc *Service) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "外壳.comp")
	svc.saveDialog = func(context.Context) (string, error) { return path, nil }
	if _, err := svc.SaveProjectDialog(); err != nil {
		t.Fatalf("保存测试项目失败: %v", err)
	}
	return path
}

// writePackage 直接在磁盘上造一个真实的 .comp 包：外壳打开的目标通常**不是**
// 当前已打开的标签（否则工作区会复用那个标签，测不出"新标签"）。
func writePackage(t *testing.T, path string) {
	t.Helper()
	doc, bitmaps := newDocumentModel(24, 18, 72)
	assets := map[string][]byte{}
	for name, bmp := range bitmaps {
		data, err := render.EncodePNG(bmp)
		if err != nil {
			t.Fatal(err)
		}
		assets[name] = data
	}
	var store project.Store
	if err := store.Save(path, doc, assets); err != nil {
		t.Fatalf("写测试包失败: %v", err)
	}
}

func TestOpenShellPathsOpensQueuedProjectsAndNotesRecent(t *testing.T) {
	svc, _ := newTestService(t, 32, 24)
	useShell(t)
	recent := useRecent(t, svc)
	path := filepath.Join(t.TempDir(), "从外壳打开.comp")
	writePackage(t, path)

	NewShellHooks(svc).Queue([]string{path, ""}) // 空串被丢掉
	out, err := svc.OpenShellPaths()
	if err != nil {
		t.Fatal(err)
	}
	reply := parseJSON[openShellReply](t, out)
	if len(reply.Opened) != 1 || reply.Opened[0] != path {
		t.Fatalf("打开的路径 = %v，想要 [%s]", reply.Opened, path)
	}
	if len(reply.Failed) != 0 {
		t.Fatalf("不该有失败: %+v", reply.Failed)
	}
	if len(reply.Snapshot.Tabs) != 2 {
		t.Fatalf("外壳打开应新增一个标签，得到 %d 个", len(reply.Snapshot.Tabs))
	}
	// 最近项目里记了一笔。
	got, err := recent.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != path {
		t.Fatalf("最近项目 = %v，想要 [%s]", got, path)
	}
}

func TestOpenShellPathsReportsUnreadablePathsAndKeepsDocuments(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	useShell(t)
	useRecent(t, svc)
	missing := filepath.Join(t.TempDir(), "不存在.comp")

	NewShellHooks(svc).Queue([]string{missing})
	out, err := svc.OpenShellPaths()
	if err != nil {
		t.Fatal(err)
	}
	reply := parseJSON[openShellReply](t, out)
	if len(reply.Opened) != 0 || len(reply.Failed) != 1 || reply.Failed[0].Path != missing {
		t.Fatalf("读不了的文件应进失败清单: %+v", reply)
	}
	if reply.Failed[0].Error == "" {
		t.Fatal("失败项应带原因")
	}
	if len(reply.Snapshot.Tabs) != 1 {
		t.Fatalf("失败的打开不该改标签: %d", len(reply.Snapshot.Tabs))
	}
}

func TestOpenShellPathsStopsAtOnePendingPhotoshopReport(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	useShell(t)
	useRecent(t, svc)
	// 直接把"已有待确认"摆上（真造一个 PSD 夹具不属于本包）。
	svc.psdMu.Lock()
	svc.pendingPSD = &pendingPSDOpen{path: "已有.psd"}
	svc.psdMu.Unlock()

	NewShellHooks(svc).Queue([]string{filepath.Join(t.TempDir(), "另一个.psd")})
	out, err := svc.OpenShellPaths()
	if err != nil {
		t.Fatal(err)
	}
	reply := parseJSON[openShellReply](t, out)
	if len(reply.Failed) != 1 || !strings.Contains(reply.Failed[0].Error, "待确认") {
		t.Fatalf("第二个 PSD 应报'已有待确认': %+v", reply.Failed)
	}
	// 原有的待确认槽没被动过。
	if conv := svc.pendingConversions(); conv != nil {
		t.Fatalf("待确认槽不该被替换: %+v", conv)
	}
	svc.psdMu.Lock()
	still := svc.pendingPSD
	svc.psdMu.Unlock()
	if still == nil || still.path != "已有.psd" {
		t.Fatalf("待确认槽 = %+v，想要原来的 已有.psd", still)
	}
}

// 单实例转发：第二次启动把参数交进来后，既有实例把窗口提到前台并推事件，前端收到
// 事件再调 OpenShellPaths 打开（shell:openPaths）。
func TestNotifyShellPathsRaisesWindowAndEmits(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	fake := useShell(t)
	useRecent(t, svc)
	svc.Startup(context.Background())

	path := filepath.Join(t.TempDir(), "第二次.comp")
	writePackage(t, path)
	hooks := NewShellHooks(svc)
	hooks.Queue([]string{path})
	hooks.Notify()

	if fake.raised != 1 {
		t.Fatalf("应把窗口提到前台一次，得到 %d", fake.raised)
	}
	if len(fake.emitted) != 1 || fake.emitted[0] != 1 {
		t.Fatalf("应推一次事件且带排队条数 1，得到 %v", fake.emitted)
	}
	// 事件之后前端会调 OpenShellPaths：队列确实还在，且能打开。
	out, err := svc.OpenShellPaths()
	if err != nil {
		t.Fatal(err)
	}
	reply := parseJSON[openShellReply](t, out)
	if len(reply.Opened) != 1 || reply.Opened[0] != path {
		t.Fatalf("排队的外壳路径应被打开: %+v", reply)
	}
	// 没有运行时上下文时不推事件（测试/无头启动），也不 panic。
	headless, _ := newTestService(t, 16, 16)
	useShell(t)
	NewShellHooks(headless).Notify()
}

func TestIsPhotoshopPath(t *testing.T) {
	for _, p := range []string{"a.psd", "A.PSB", "C:\\x\\b.PsD"} {
		if !isPhotoshopPath(p) {
			t.Fatalf("%s 应是 Photoshop 路径", p)
		}
	}
	for _, p := range []string{"a.comp", "a.png", "psd"} {
		if isPhotoshopPath(p) {
			t.Fatalf("%s 不该是 Photoshop 路径", p)
		}
	}
}

func TestSyncWindowChromeTitleAndOverlay(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	fake := useShell(t)
	svc.Startup(context.Background()) // 有 ctx 才会碰窗口标题（生产由 Wails 调用）

	cases := []struct {
		name  string
		dirty bool
		want  string
	}{
		{"未命名", true, "未命名* - Compositor"},
		{"未命名", false, "未命名 - Compositor"},
		{"  ", true, "Compositor"},
	}
	for i, tc := range cases {
		out, err := svc.SyncWindowChrome(tc.name, tc.dirty)
		if err != nil {
			t.Fatal(err)
		}
		var reply struct {
			Title string `json:"title"`
		}
		if err := json.Unmarshal([]byte(out), &reply); err != nil {
			t.Fatal(err)
		}
		if reply.Title != tc.want {
			t.Fatalf("标题 = %q，想要 %q", reply.Title, tc.want)
		}
		if got := fake.titles[i]; got != tc.want {
			t.Fatalf("外壳收到的标题 = %q，想要 %q", got, tc.want)
		}
		if fake.overlays[i] != tc.dirty {
			t.Fatalf("覆盖图标 = %v，想要 %v", fake.overlays[i], tc.dirty)
		}
	}

	// 覆盖图标失败不致命（没有任务栏按钮时也会走到这里）。
	fake.overlayErr = os.ErrPermission
	if _, err := svc.SyncWindowChrome("x", true); err != nil {
		t.Fatalf("覆盖图标失败不该让调用失败: %v", err)
	}
}

func TestRecentProjectsMenuAndClear(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	fake := useShell(t)
	store := useRecent(t, svc)

	dir := t.TempDir()
	kept := filepath.Join(dir, "甲.comp")
	if err := os.WriteFile(kept, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(kept); err != nil {
		t.Fatal(err)
	}

	out, err := svc.RecentProjects()
	if err != nil {
		t.Fatal(err)
	}
	var reply shellPathsReply
	if err := json.Unmarshal([]byte(out), &reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Paths) != 1 || reply.Paths[0] != kept {
		t.Fatalf("最近项目 = %v，想要 [%s]", reply.Paths, kept)
	}

	cleared, err := svc.ClearRecentProjects()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(cleared), &reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Paths) != 0 {
		t.Fatalf("清空后应为空: %v", reply.Paths)
	}
	if fake.cleared != 1 {
		t.Fatalf("应清一次外壳最近文档，得到 %d", fake.cleared)
	}
	if list, _ := store.List(); len(list) != 0 {
		t.Fatalf("持久层也应清空: %v", list)
	}
}

func TestRecentProjectsWithoutStoreIsEmpty(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	useShell(t)
	// 没装持久层（测试默认）：菜单是空列表，也不能报错。
	out, err := svc.RecentProjects()
	if err != nil {
		t.Fatal(err)
	}
	if out != `{"paths":[]}` {
		t.Fatalf("空列表拼写 = %s", out)
	}
}

func TestOpenPathNotesRecentOnSuccessOnly(t *testing.T) {
	svc, _ := newTestService(t, 32, 24)
	fake := useShell(t)
	recent := useRecent(t, svc)
	path := saveActiveTo(t, svc)

	if _, err := svc.OpenPath(path); err != nil {
		t.Fatal(err)
	}
	if len(fake.notedRecent) != 2 {
		// 保存一次 + 打开一次。
		t.Fatalf("外壳最近文档应记两次（保存 + 打开），得到 %v", fake.notedRecent)
	}
	if list, _ := recent.List(); len(list) != 1 {
		t.Fatalf("打开后最近项目应只有去重后的一项: %v", list)
	}

	if _, err := svc.OpenPath(filepath.Join(t.TempDir(), "没有.comp")); err == nil {
		t.Fatal("打不开的路径应报错")
	}
	if list, _ := recent.List(); len(list) != 1 {
		t.Fatalf("失败的打开不应改最近项目: %v", list)
	}
}
