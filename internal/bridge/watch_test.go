package bridge

// 外部变更的端到端测试（票 51 / P05）：真实磁盘写入 → watch.Watcher（fsnotify 合并
// 300ms）→ watch.Coordinator（digest 比对）→ 原位重载。
//
// "代理"由真实的 project.Store 扮演：改包时 PNG 先写、manifest 最后原子改名 —— 与
// docs/writing-comp-files.md 的契约一致。事件通过外壳接缝的假实现观察。

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"compositor-win/internal/domain"
	"compositor-win/internal/project"
	"compositor-win/internal/render"
)

const externalEvent = "external:changed"

// externalTest 是"应用打开了一个包 + 假外壳"的装置。
type externalTest struct {
	svc   *Service
	shell *fakeShell
	dir   string
	tabID string
}

func newExternalTest(t *testing.T, layerName string) *externalTest {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "演示.comp")
	writeAgentPackage(t, dir, layerName)

	svc, _ := newTestService(t, 8, 8)
	shell := useShell(t)
	svc.Startup(context.Background()) // 有运行时上下文才会推事件
	if _, err := svc.OpenPath(dir); err != nil {
		t.Fatalf("打开测试包失败: %v", err)
	}
	tabID, ok := svc.ws.TabIDForPath(dir)
	if !ok {
		t.Fatal("打开后应能找到持有该路径的标签")
	}
	t.Cleanup(func() { svc.stopWatchingProjects() })
	return &externalTest{svc: svc, shell: shell, dir: dir, tabID: tabID}
}

// writeAgentPackage 造一个包：24x18 的文档，背景层叫 layerName。
func writeAgentPackage(t *testing.T, dir, layerName string) {
	t.Helper()
	doc, bitmaps := newDocumentModel(24, 18, 72)
	doc.Layers[0].Name = layerName
	assets := map[string][]byte{}
	for _, name := range referencedAssetNames(doc) {
		data, err := render.EncodePNG(bitmaps[name])
		if err != nil {
			t.Fatal(err)
		}
		assets[name] = data
	}
	var store project.Store
	if err := store.Save(dir, doc, assets); err != nil {
		t.Fatalf("写测试包失败: %v", err)
	}
}

// agentRewrite 扮演代理改包：读回 → mutate 改文档/加资产 → store.Save（PNG 先写、
// manifest 最后原子改名，正是契约要求的顺序）。
func agentRewrite(t *testing.T, dir string, mutate func(*domain.Document, map[string][]byte)) {
	t.Helper()
	var store project.Store
	doc, err := store.Open(dir)
	if err != nil {
		t.Fatalf("代理读包失败: %v", err)
	}
	assets := map[string][]byte{}
	for _, name := range referencedAssetNames(doc) {
		data, err := store.ReadAsset(dir, name)
		if err != nil {
			t.Fatalf("代理读资产 %s 失败: %v", name, err)
		}
		assets[name] = data
	}
	mutate(doc, assets)
	if err := store.Save(dir, doc, assets); err != nil {
		t.Fatalf("代理写包失败: %v", err)
	}
}

// renameLayer 是最小的"改了 manifest"的代理动作。
func renameLayer(name string) func(*domain.Document, map[string][]byte) {
	return func(doc *domain.Document, _ map[string][]byte) { doc.Layers[0].Name = name }
}

// addTopLayer 追加一个图层：新 id、复制背景像素、指定名字（manifest 与 images/ 都变）。
func addTopLayer(name string) func(*domain.Document, map[string][]byte) {
	return func(doc *domain.Document, assets map[string][]byte) {
		src := doc.Layers[0]
		id := newUUID()
		asset := id + ".png"
		if src.ImageFile != nil {
			assets[asset] = assets[*src.ImageFile]
		}
		opacity := 1.0
		blend := domain.BlendNormal
		imageFile := asset
		doc.Layers = append(doc.Layers, domain.Layer{
			ID: id, Name: name, IsVisible: true, ImageFile: &imageFile,
			Transform: src.Transform, Opacity: &opacity, BlendMode: &blend,
		})
	}
}

// topLayerName 读当前活动文档最上面的图层名。
func topLayerName(t *testing.T, svc *Service) string {
	t.Helper()
	doc := mustDocumentSnapshot(t, svc)
	if len(doc.Doc.Layers) == 0 {
		t.Fatal("文档没有图层")
	}
	return doc.Doc.Layers[len(doc.Doc.Layers)-1].Name
}

// changeEvent 解析一条 external:changed 负载。
func changeEvent(t *testing.T, payload string) externalChangedEvent {
	t.Helper()
	var event externalChangedEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatalf("外部变更负载无法解析: %v（%s）", err, payload)
	}
	return event
}

// waitForEvents 等到某事件收到 want 条，返回全部负载；超时即失败并给出诊断。
func waitForEvents(t *testing.T, shell *fakeShell, name string, want int, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		all := shell.eventPayloads(name)
		if len(all) >= want {
			return all
		}
		if time.Now().After(deadline) {
			t.Fatalf("等 %q 事件（要 %d 条）超时：只收到 %d 条", name, want, len(all))
		}
		time.Sleep(15 * time.Millisecond)
	}
}

// waitForNoNewEvent 断言在给定窗口内没有新事件（用来验证"没被当成变更"）。
func waitForNoNewEvent(t *testing.T, shell *fakeShell, name string, baseline int, window time.Duration) {
	t.Helper()
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		if got := len(shell.eventPayloads(name)); got > baseline {
			t.Fatalf("不该有新事件，却收到了第 %d 条: %s", got, shell.lastEvent(name))
		}
		time.Sleep(15 * time.Millisecond)
	}
}

// doEdit 改一次图层名：走 LayerOp，所以会**进历史事务**并让文档变脏。
// （笔刷笔画不进历史 —— 它只提交像素 —— 所以这里不能用画一笔来造历史。）
func doEdit(t *testing.T, svc *Service) {
	t.Helper()
	doc := mustDocumentSnapshot(t, svc)
	if len(doc.Doc.Layers) == 0 {
		t.Fatal("文档没有图层")
	}
	payload, err := json.Marshal(map[string]string{"id": doc.Doc.Layers[0].ID, "name": "本地改的名字"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LayerOp("rename", string(payload)); err != nil {
		t.Fatalf("本地编辑失败: %v", err)
	}
	if sess := activeSessionOf(t, svc); !sess.hist.CanUndo() {
		t.Fatal("本地编辑应产生一条可撤销的历史")
	}
}

// 验收 1：真实写入 → ~300ms 合并 → 原位重载，标签不变、撤销栈清空、视口不动（视口在
// 前端，重载不碰它；这里核对事件不带视口字段、文档内容确实换了）。
func TestExternalWriteReloadsTheOpenDocumentInPlace(t *testing.T) {
	fixture := newExternalTest(t, "背景")

	// 先制造"有可撤销历史 + 已保存（不脏）"的状态：编辑一笔再保存。
	doEdit(t, fixture.svc)
	if _, err := fixture.svc.SaveProjectDialog(); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	// 保存只挪动"已保存修订"，历史还在 —— 这样后面才验证得了"重载清空撤销栈"。
	if sess := activeSessionOf(t, fixture.svc); !sess.hist.CanUndo() {
		t.Fatal("保存后应仍保留可撤销历史")
	}

	start := time.Now()
	agentRewrite(t, fixture.dir, renameLayer("代理改的名字"))
	payloads := waitForEvents(t, fixture.shell, externalEvent, 1, 5*time.Second)
	elapsed := time.Since(start)

	event := changeEvent(t, payloads[0])
	if event.Pending {
		t.Fatalf("干净的文档应直接重载，不该询问: %+v", event)
	}
	if event.TabID != fixture.tabID || event.Path != fixture.dir {
		t.Fatalf("事件应指向被监视的标签/路径: %+v", event)
	}
	if event.Document == "" {
		t.Fatal("重载事件应带上新文档信封（前端据此刷新画布）")
	}
	if !strings.Contains(event.Document, "代理改的名字") {
		t.Fatalf("文档信封里应有代理的新图层名: %s", event.Document)
	}
	// rev 必须往前走：前端的画布缓存键是 (docId, rev)，而代理按契约保留 documentID，
	// 于是 rev 不前进的话"刚打开还没编辑过"的标签会一直显示旧像素。
	envelope := parseEnvelope(t, event.Document)
	if envelope.Rev == 0 {
		t.Fatal("重载后的 rev 必须大于 0（否则前端会按同一键命中旧画面）")
	}
	if got := topLayerName(t, fixture.svc); got != "代理改的名字" {
		t.Fatalf("重载后最上层名字 = %q，想要 代理改的名字", got)
	}
	// 撤销栈被清空（等同重新打开文件），而且是一条新会话。
	if sess := activeSessionOf(t, fixture.svc); sess.hist.CanUndo() || sess.hist.UndoCount() != 0 {
		t.Fatalf("重载后撤销栈应已清空：count=%d", sess.hist.UndoCount())
	}
	// 标签没变、不脏。
	snapshot := fixture.svc.ws.Snapshot()
	if snapshot.ActiveID != fixture.tabID {
		t.Fatalf("重载不该换活动标签: %+v", snapshot)
	}
	for _, tab := range snapshot.Tabs {
		if tab.ID == fixture.tabID && tab.Dirty {
			t.Fatal("重载后标签不该是脏的")
		}
	}
	// 时序：合并窗口 300ms + coordinator 稳定（首次比较需要一次退避 250ms）。
	t.Logf("外部写入到重载完成：%v", elapsed.Round(time.Millisecond))
	if elapsed > 3*time.Second {
		t.Fatalf("重载太慢：%v", elapsed)
	}
}

// 验收 2：顺序破坏（manifest 先改、PNG 还没写）→ 忽略并保持旧画面；补上 PNG 后恢复。
func TestExternalBrokenOrderIsIgnoredThenPickedUp(t *testing.T) {
	fixture := newExternalTest(t, "背景")

	// 先记下"旧画面"的图层名，改完 manifest 后应保持不变。
	before := topLayerName(t, fixture.svc)

	// 破坏顺序：先加图层（manifest 引用一个新 PNG），随后删掉那个 PNG —— 相当于代理
	// 在"PNG 先写"这一步之前就把 manifest 改名了。
	agentRewrite(t, fixture.dir, addTopLayer("半成品图层"))
	var store project.Store
	doc, err := store.Open(fixture.dir)
	if err != nil {
		t.Fatalf("读包失败: %v", err)
	}
	missing := *doc.Layers[len(doc.Layers)-1].ImageFile
	// 借背景层的 PNG 字节，稍后用它把缺的资产"补写"回来（契约里的第一步：PNG 先写）。
	background, err := os.ReadFile(filepath.Join(fixture.dir, "images", *doc.Layers[0].ImageFile))
	if err != nil {
		t.Fatalf("读背景资产失败: %v", err)
	}
	if err := os.Remove(filepath.Join(fixture.dir, "images", missing)); err != nil {
		t.Fatalf("删除资产失败: %v", err)
	}

	// 半成品：被忽略（没有事件），旧画面保持。
	waitForNoNewEvent(t, fixture.shell, externalEvent, 0, 1200*time.Millisecond)
	if got := topLayerName(t, fixture.svc); got != before {
		t.Fatalf("半成品不该改画面：%q → %q", before, got)
	}

	// 代理补上 PNG（这正是契约的第一步；内容再变一次）→ 这次应该重载。
	if err := os.WriteFile(filepath.Join(fixture.dir, "images", missing), background, 0o644); err != nil {
		t.Fatalf("补写资产失败: %v", err)
	}
	payloads := waitForEvents(t, fixture.shell, externalEvent, 1, 5*time.Second)
	event := changeEvent(t, payloads[0])
	if event.Pending {
		t.Fatalf("干净文档应直接重载: %+v", event)
	}
	// 现在这个包是完整的，于是同一份 manifest（"半成品图层"）被载入 —— 正是契约要的
	// 行为：写坏的包被忽略，代理把缺的那一步补上之后，改动就出现了。
	if got := topLayerName(t, fixture.svc); got != "半成品图层" {
		t.Fatalf("补齐后最上层 = %q，想要 半成品图层", got)
	}
}

// 契约的一个已知边界：PNG 换了但**字节大小完全相同**、manifest 一字不改 → 摘要不变，
// 应用看不到更新（Swift 版同样如此，docs/writing-comp-files.md 里写明了）。
func TestExternalSameSizeRewriteIsNotNoticed(t *testing.T) {
	fixture := newExternalTest(t, "背景")

	var store project.Store
	doc, err := store.Open(fixture.dir)
	if err != nil {
		t.Fatal(err)
	}
	asset := *doc.Layers[0].ImageFile
	path := filepath.Join(fixture.dir, "images", asset)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// 造一份同样长度的不同字节（PNG 头之后改一个字节，仍是合法 PNG 的可能性不重要：
	// 这里只验证"摘要没变就不报"，所以要保持大小一致且 manifest 不变）。
	changed := append([]byte(nil), original...)
	changed[len(changed)-1] ^= 0xFF
	if len(changed) != len(original) {
		t.Fatal("测试自身有问题：长度应保持一致")
	}
	if err := os.WriteFile(path, changed, 0o644); err != nil {
		t.Fatal(err)
	}

	waitForNoNewEvent(t, fixture.shell, externalEvent, 0, 1200*time.Millisecond)
}

// 脏文档：绝不静默覆盖；问过之后再决定（保留 / 放弃）。
func TestExternalDirtyDocumentAsksBeforeReplacing(t *testing.T) {
	fixture := newExternalTest(t, "背景")
	doEdit(t, fixture.svc) // 画一笔 → 脏

	agentRewrite(t, fixture.dir, renameLayer("代理的版本"))
	payloads := waitForEvents(t, fixture.shell, externalEvent, 1, 5*time.Second)
	event := changeEvent(t, payloads[0])
	if !event.Pending || !event.Dirty {
		t.Fatalf("脏文档应报 pending+dirty: %+v", event)
	}
	if event.Document != "" {
		t.Fatal("pending 事件不该带新文档（还没决定要不要用）")
	}
	// 本地修改还在。
	if got := topLayerName(t, fixture.svc); got == "代理的版本" {
		t.Fatal("用户还没决定，不该已经换成代理的版本")
	}

	// 保留我的：文档不变，待决定状态清掉。
	if _, err := fixture.svc.ResolveExternalChange(fixture.tabID, false); err != nil {
		t.Fatalf("保留本地修改失败: %v", err)
	}
	if entry := fixture.svc.watchedEntry(fixture.tabID); entry == nil || entry.pending {
		t.Fatal("保留之后不该还挂着待决定状态")
	}
	if got := topLayerName(t, fixture.svc); got == "代理的版本" {
		t.Fatal("保留本地修改后文档不该变")
	}
	// 同一份磁盘内容不再追问。
	waitForNoNewEvent(t, fixture.shell, externalEvent, len(payloads), 700*time.Millisecond)

	// 代理再改一次（新摘要）→ 再问一次；这次放弃本地修改。
	agentRewrite(t, fixture.dir, renameLayer("代理的第二版"))
	payloads = waitForEvents(t, fixture.shell, externalEvent, 2, 5*time.Second)
	if !changeEvent(t, payloads[1]).Pending {
		t.Fatalf("新的外部变更应再次询问: %+v", payloads[1])
	}
	if _, err := fixture.svc.ResolveExternalChange(fixture.tabID, true); err != nil {
		t.Fatalf("放弃本地修改失败: %v", err)
	}
	if got := topLayerName(t, fixture.svc); got != "代理的第二版" {
		t.Fatalf("接受后应载入代理的版本，得到 %q", got)
	}
	snapshot := fixture.svc.ws.Snapshot()
	for _, tab := range snapshot.Tabs {
		if tab.ID == fixture.tabID && tab.Dirty {
			t.Fatal("载入磁盘版本后不该还是脏的")
		}
	}
	// 没有待决定状态时再调应报错（不能凭空接受）。
	if _, err := fixture.svc.ResolveExternalChange(fixture.tabID, true); err == nil || !strings.Contains(err.Error(), "EXTERNAL_CHANGE_NOT_PENDING") {
		t.Fatalf("没有待处理的外部变更应返回稳定错误码，错误=%v", err)
	}
}

// Accepting a pending change can race a package writer that is still replacing
// manifest.json. A failed load must leave the prompt actionable so the user
// can retry after the package becomes readable.
func TestResolveExternalChangeRestoresPendingAfterLoadFailure(t *testing.T) {
	fixture := newExternalTest(t, "背景")
	doEdit(t, fixture.svc)

	agentRewrite(t, fixture.dir, renameLayer("代理版本"))
	payloads := waitForEvents(t, fixture.shell, externalEvent, 1, 5*time.Second)
	if event := changeEvent(t, payloads[0]); !event.Pending {
		t.Fatalf("脏文档应先进入待决定状态: %+v", event)
	}

	manifest := filepath.Join(fixture.dir, "manifest.json")
	manifestBytes, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.svc.ResolveExternalChange(fixture.tabID, true); err == nil {
		t.Fatal("包不可读时接受外部版本应失败")
	}
	if entry := fixture.svc.watchedEntry(fixture.tabID); entry == nil || !entry.pending {
		t.Fatal("外部版本载入失败后应保留待决定状态")
	}
	if got := topLayerName(t, fixture.svc); got == "代理版本" {
		t.Fatal("载入失败不应覆盖本地文档")
	}

	if err := os.WriteFile(manifest, manifestBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.svc.ResolveExternalChange(fixture.tabID, true); err != nil {
		t.Fatalf("包恢复可读后应能重试接受外部版本: %v", err)
	}
	if got := topLayerName(t, fixture.svc); got != "代理版本" {
		t.Fatalf("重试接受后图层名 = %q，想要 代理版本", got)
	}
}

// 正在拖动笔刷时外部改了包：不静默重载（会把会话换掉，笔画落空），先问用户。
func TestExternalChangeWaitsWhileAStrokeIsInFlight(t *testing.T) {
	fixture := newExternalTest(t, "背景")

	if _, err := fixture.svc.BeginStroke(4, 4); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = fixture.svc.ws.EndStroke() }()

	agentRewrite(t, fixture.dir, renameLayer("笔刷进行中"))
	payloads := waitForEvents(t, fixture.shell, externalEvent, 1, 5*time.Second)
	event := changeEvent(t, payloads[0])
	if !event.Pending {
		t.Fatalf("有编辑在飞时应先询问而不是直接换掉会话: %+v", event)
	}
	if got := topLayerName(t, fixture.svc); got == "笔刷进行中" {
		t.Fatal("还没决定就换了文档")
	}

	// 结束笔画后用户选择载入磁盘版本：这时才真正重载。
	if _, err := fixture.svc.ws.EndStroke(); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.svc.ResolveExternalChange(fixture.tabID, true); err != nil {
		t.Fatalf("载入磁盘版本失败: %v", err)
	}
	if got := topLayerName(t, fixture.svc); got != "笔刷进行中" {
		t.Fatalf("接受后应载入磁盘版本，得到 %q", got)
	}
}

// 连写多次 → 合并成一次重载（合并窗口 300ms）。
func TestExternalConcurrentWritesMergeIntoOneReload(t *testing.T) {
	fixture := newExternalTest(t, "背景")

	// 三次写入挤在一个合并窗口里（40ms 间隔 ≈ 80ms 总长，远小于 300ms）。
	for i := 0; i < 3; i++ {
		agentRewrite(t, fixture.dir, renameLayer("第 "+string(rune('1'+i))+" 版"))
		time.Sleep(40 * time.Millisecond)
	}
	waitForEvents(t, fixture.shell, externalEvent, 1, 5*time.Second)
	// 重载发生在最后一次写入之后；再等一个合并窗口，确认没有被拆成很多次。
	time.Sleep(800 * time.Millisecond)
	// 断言写成"≤ 2"而不是"== 1"：慢机器上三次写入可能跨过 300ms 边界而被拆成两次，
	// 那仍然是"合并生效"（5 次原始事件被压成 2 次重载），而"每次写入都重载一次"才会 >2。
	if got := len(fixture.shell.eventPayloads(externalEvent)); got > 2 {
		t.Fatalf("连写应被合并（期望 1–2 次重载），得到 %d 次", got)
	}
}

// 后台标签的文件被外部改了：内容重载，但不把用户从当前标签拽走。
func TestExternalReloadKeepsTheActiveTab(t *testing.T) {
	fixture := newExternalTest(t, "背景")

	// 再开一个文档并让它成为活动标签。
	if _, err := fixture.svc.ws.NewDocument(16, 16, 72); err != nil {
		t.Fatal(err)
	}
	active := fixture.svc.ws.Snapshot().ActiveID
	if active == fixture.tabID {
		t.Fatal("测试自身有问题：新文档应成为活动标签")
	}

	agentRewrite(t, fixture.dir, renameLayer("后台改的"))
	payloads := waitForEvents(t, fixture.shell, externalEvent, 1, 5*time.Second)
	event := changeEvent(t, payloads[0])
	if event.TabID != fixture.tabID {
		t.Fatalf("事件应指向被改的标签: %+v", event)
	}
	if event.Snapshot.ActiveID != active {
		t.Fatalf("后台重载不该换活动标签：%s → %s", active, event.Snapshot.ActiveID)
	}
	// 后台标签的事件不能带文档信封：前端的文档镜像只有活动标签那一份，收到别的标签的
	// 文档会把当前画布覆盖掉。
	if event.Document != "" {
		t.Fatalf("后台标签的重载不该带文档信封: %s", event.Document)
	}
}

// 关掉标签后监视器退休：文件再变也不该有事件，也不该崩。
func TestWatchStopsWhenTheTabCloses(t *testing.T) {
	fixture := newExternalTest(t, "背景")
	if _, err := fixture.svc.ws.CloseTab(fixture.tabID); err != nil {
		t.Fatal(err)
	}
	if entry := fixture.svc.watchedEntry(fixture.tabID); entry != nil {
		t.Fatal("关闭标签应停掉它的监视器")
	}

	agentRewrite(t, fixture.dir, renameLayer("关掉之后"))
	waitForNoNewEvent(t, fixture.shell, externalEvent, 0, 1200*time.Millisecond)
}

// manifest 读不出来（这里用"把 manifest.json 换成目录"来模拟被独占/云占位符）：
// 忽略这一次，恢复之后仍能重载。
func TestExternalUnreadablePackageIsIgnoredUntilFixed(t *testing.T) {
	fixture := newExternalTest(t, "背景")
	before := topLayerName(t, fixture.svc)
	var store project.Store
	original, err := store.Open(fixture.dir)
	if err != nil {
		t.Fatal(err)
	}
	assets := map[string][]byte{}
	for _, name := range referencedAssetNames(original) {
		data, err := store.ReadAsset(fixture.dir, name)
		if err != nil {
			t.Fatal(err)
		}
		assets[name] = data
	}

	manifest := filepath.Join(fixture.dir, "manifest.json")
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(manifest, 0o755); err != nil {
		t.Fatal(err)
	}
	waitForNoNewEvent(t, fixture.shell, externalEvent, 0, 1200*time.Millisecond)
	if got := topLayerName(t, fixture.svc); got != before {
		t.Fatalf("读不了的包不该改画面：%q → %q", before, got)
	}

	// 修复同一项目：保留原 documentID 和资产，再提交一份合法 manifest。
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	original.Layers[0].Name = "修好了"
	if err := store.Save(fixture.dir, original, assets); err != nil {
		t.Fatal(err)
	}
	payloads := waitForEvents(t, fixture.shell, externalEvent, 1, 5*time.Second)
	if event := changeEvent(t, payloads[0]); event.Pending {
		t.Fatalf("修好后应直接重载: %+v", event)
	}
	if got := topLayerName(t, fixture.svc); got != "修好了" {
		t.Fatalf("修好后最上层 = %q，想要 修好了", got)
	}
}

// A coordinator callback can already be inside OnStable when Save retires its
// watcher. That callback belongs to the old watcher and must not reload the
// newly saved session (or emit a stale external-change event) after Save has
// installed the replacement watcher.
func TestSaveIgnoresStaleCallbackFromRetiredWatcher(t *testing.T) {
	fixture := newExternalTest(t, "背景")
	doEdit(t, fixture.svc)
	entry := fixture.svc.watchedEntry(fixture.tabID)
	if entry == nil {
		t.Fatal("打开项目后应有项目监视器")
	}
	oldStable := entry.coordinator.OnStable
	callbackStarted := make(chan struct{})
	callbackRelease := make(chan struct{})
	callbackDone := make(chan struct{})
	entry.coordinator.Base = time.Millisecond
	entry.coordinator.Digest = func(string) (string, error) {
		return "stale-digest", nil
	}
	entry.coordinator.OnStable = func(digest string) {
		close(callbackStarted)
		<-callbackRelease
		oldStable(digest)
		close(callbackDone)
	}
	entry.coordinator.Notify()
	select {
	case <-callbackStarted:
	case <-time.After(time.Second):
		t.Fatal("旧监视器没有进入稳定变更回调")
	}

	saved := make(chan error, 1)
	go func() {
		_, err := fixture.svc.SaveProjectDialog()
		saved <- err
	}()
	select {
	case err := <-saved:
		if err != nil {
			t.Fatalf("保存期间旧回调不应阻塞保存: %v", err)
		}
	case <-time.After(time.Second):
		close(callbackRelease)
		t.Fatal("保存被已退休监视器的回调阻塞")
	}

	close(callbackRelease)
	select {
	case <-callbackDone:
	case <-time.After(time.Second):
		t.Fatal("旧监视器回调未退出")
	}
	if entry := fixture.svc.watchedEntry(fixture.tabID); entry == nil {
		t.Fatal("保存成功后应安装新的项目监视器")
	}
	if sess := activeSessionOf(t, fixture.svc); !sess.hist.CanUndo() {
		t.Fatal("保存不应清空仍可撤销的历史")
	}
	if got := len(fixture.shell.eventPayloads(externalEvent)); got != 0 {
		t.Fatalf("已退休监视器不应在保存后发出外部变更事件，收到 %d 条", got)
	}
}
