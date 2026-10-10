package bridge

// 外部变更监视的接线（票 07 提供基础设施，票 51 做端到端）。
//
// 数据流：文件系统事件 → watch.Watcher（fsnotify，300ms 合并）→ watch.Coordinator
// （sha256 digest 比对 + 250ms×2^n 退避，内容稳定后才报一次）→ 这里：读盘、判脏、
// 原地重载，或者先问用户。
//
// 契约见 docs/writing-comp-files.md：代理先写 images/ 下的 PNG，再把临时文件改名覆盖
// manifest.json。读不出来的包（写了一半、缺图、带 BOM）**静默忽略**，等下一次变更 ——
// 所以代理写错了再改回来仍然能看到更新。

import (
	"encoding/json"
	"fmt"

	"compositor-win/internal/domain"
	"compositor-win/internal/render"
	"compositor-win/internal/watch"
)

// externalChangedEvent 是推给前端的 "external:changed" 负载。
//
// pending 为 true 表示文档有未保存修改：应用**不会**静默覆盖，前端要问"放弃我的修改 /
// 保留我的"，然后把答案交给 ResolveExternalChange。pending 为 false 时 document 带的是
// 重载后的文档信封，前端据此刷新画布；视口（缩放/平移/选区）在前端，重载不动它。
type externalChangedEvent struct {
	TabID    string   `json:"tabId"`
	Path     string   `json:"path"`
	Pending  bool     `json:"pending"`
	Dirty    bool     `json:"dirty"`
	Snapshot Snapshot `json:"snapshot"`
	Document string   `json:"document,omitempty"`
}

// watchedProject 是一个标签的外部变更监视器（watcher + coordinator）。
type watchedProject struct {
	path        string
	watcher     *watch.Watcher
	coordinator *watch.Coordinator
	// pending 为 true 表示磁盘上有一份新内容，但文档有未保存修改，正等用户决定。
	pending bool
}

// watchProject 开始（或重新指向）监视某个标签持有的包。打开、另存为、确认 PSD 导入
// 之后都要调它：它总是先停掉旧监视器并重新记基线，所以"另存到新路径"不会留下盯着旧
// 目录的监视器。
func (s *Service) watchProject(tabID, path string) {
	if s.ws == nil || tabID == "" || path == "" {
		return
	}
	s.unwatchProject(tabID)

	coordinator := watch.NewCoordinator(path)
	coordinator.OnStable = func(digest string) {
		s.externalChanged(tabID, path, digest)
	}
	// 基线：先记住当前磁盘内容，只有真的变了才报告。读不出来（写了一半）就让
	// coordinator 记住"没有基线"，下一次变更会无条件报一次。
	if err := coordinator.Remember(); err != nil {
		s.logf("外部变更监视 %s：当前内容读不到（%v），等下一次变更", path, err)
	}
	watcher := watch.NewWatcher()
	if err := watcher.Start(path, coordinator.Notify); err != nil {
		s.logf("外部变更监视启动失败（%s）: %v", path, err)
		return
	}
	s.watchMu.Lock()
	if s.watched == nil {
		s.watched = map[string]*watchedProject{}
	}
	s.watched[tabID] = &watchedProject{path: path, watcher: watcher, coordinator: coordinator}
	s.watchMu.Unlock()
}

// unwatchProject 停掉并忘掉一个标签的监视器（标签关闭、另存为、文档消失时用）。
func (s *Service) unwatchProject(tabID string) {
	s.watchMu.Lock()
	entry := s.watched[tabID]
	delete(s.watched, tabID)
	s.watchMu.Unlock()
	if entry == nil {
		return
	}
	entry.watcher.Stop()
	entry.coordinator.Stop()
}

// stopWatchingProjects 停掉全部监视器（退出时用）。
func (s *Service) stopWatchingProjects() {
	s.watchMu.Lock()
	entries := s.watched
	s.watched = map[string]*watchedProject{}
	s.watchMu.Unlock()
	for _, entry := range entries {
		entry.watcher.Stop()
		entry.coordinator.Stop()
	}
}

// watchedEntry 读一个标签的监视器（**只给测试用**：它把活指针交出去，生产路径不要用，
// 因为 entry.pending 会被 coordinator 的 goroutine 改）。
func (s *Service) watchedEntry(tabID string) *watchedProject {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	return s.watched[tabID]
}

// setPendingExternal 在锁内标记/清除某个标签的"待用户决定"状态。
func (s *Service) setPendingExternal(tabID string, pending bool) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	if entry := s.watched[tabID]; entry != nil {
		entry.pending = pending
	}
}

// takePendingExternal 在锁内**取出并清掉**待决定状态，返回它对应的包路径。把"判断 + 取走"
// 合成一次原子操作，避免调用方拿着锁外的指针跨慢操作（读盘）继续用。
func (s *Service) takePendingExternal(tabID string) (path string, ok bool) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	entry := s.watched[tabID]
	if entry == nil || !entry.pending {
		return "", false
	}
	entry.pending = false
	return entry.path, true
}

// externalChanged 在 coordinator 的 goroutine 上跑：包内容稳定在一份新摘要上了。
func (s *Service) externalChanged(tabID, path, digest string) {
	if s.ws == nil {
		return
	}
	currentPath, dirty, ok := s.ws.TabState(tabID)
	if !ok || currentPath != path {
		// 标签关了，或者文档已经另存到别处：这个监视器退休。
		s.unwatchProject(tabID)
		return
	}
	doc, bitmaps, err := s.loadProject(path)
	if err != nil {
		// 半成品或不合法的包：忽略这一次。coordinator 已经把摘要推进到这份内容，
		// 所以代理改好之后（内容再变）会重新报一次。
		s.logf("外部变更未载入（%s）: %v", path, err)
		return
	}
	// 有未保存修改，或者**正有编辑在飞**（笔刷拖着、滤镜对话框开着）：绝不静默覆盖。
	// 前者是 Swift 的语义；后者是我们自己的补丁 —— 重载会把整个会话换掉，笔画/滤镜
	// 落在一个已经没人看的文档上会丢，也不能指望用户在这种状态下回答询问。
	if dirty || s.ws.TabBusy(tabID) {
		s.setPendingExternal(tabID, true)
		s.emitExternal(externalChangedEvent{
			TabID: tabID, Path: path, Pending: true, Dirty: true, Snapshot: s.ws.Snapshot(),
		})
		return
	}
	s.applyExternalReload(tabID, path, doc, bitmaps, digest)
}

// applyExternalReload 原地换掉标签的内容：标签 id/位置/活动标签都不变，撤销栈清空
// （等同重新打开文件），脏标记归零。
func (s *Service) applyExternalReload(tabID, path string, doc *domain.Document, bitmaps map[string]*render.Bitmap, _ string) {
	snapshot, reloadedID, ok, err := s.ws.ReloadDocument(path, doc, bitmaps)
	if err != nil {
		s.logf("外部变更重载失败（%s）: %v", path, err)
		return
	}
	if !ok {
		s.unwatchProject(tabID)
		return
	}
	s.setPendingExternal(reloadedID, false)

	// 文档信封只在前端"正在看这个标签"时才发：前端只维护**活动标签**的文档镜像，
	// 把它不认识的后台标签文档发过去只会把当前画布覆盖掉。后台标签的重载只需要前端
	// 更新标签列表；用户切过去时前端本来就会重新拉文档。
	document := ""
	if snapshot.ActiveID == reloadedID {
		encoded, err := s.ws.DocumentJSON(reloadedID)
		if err != nil {
			s.logf("外部变更重载后无法编码文档（%s）: %v", path, err)
		} else {
			document = encoded
		}
	}
	s.emitExternal(externalChangedEvent{
		TabID: reloadedID, Path: path, Snapshot: snapshot, Document: document,
	})
}

// ResolveExternalChange 是"文件在磁盘上被改了，而你有未保存修改"的回答：
// accept=true 放弃本地修改、载入磁盘版本；accept=false 保留本地修改（同一份磁盘内容
// 不会再问一次，但下一次变更会）。tabID 来自前端收到的事件负载。
func (s *Service) ResolveExternalChange(tabID string, accept bool) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	path, ok := s.takePendingExternal(tabID)
	if !ok {
		return "", fmt.Errorf("没有待处理的外部变更")
	}
	if !accept {
		// 保留本地修改：什么都不换，只回当前文档（活动标签才有意义）。
		if s.ws.Snapshot().ActiveID != tabID {
			return marshalEnvelope(s.ws.ActiveRev(), nil)
		}
		return s.ws.DocumentJSON(tabID)
	}
	doc, bitmaps, err := s.loadProject(path)
	if err != nil {
		return "", err
	}
	// 直接把这一版贴上去（不必再走 coordinator 的稳定等待：用户已经看到并同意了）。
	_, reloadedID, reloaded, err := s.ws.ReloadDocument(path, doc, bitmaps)
	if err != nil {
		return "", err
	}
	if !reloaded {
		s.unwatchProject(tabID)
		return "", fmt.Errorf("标签已不在，无法载入磁盘版本")
	}
	// 与 applyExternalReload 一样的规则：只有活动标签才回文档信封（前端只有一份文档镜像）。
	if s.ws.Snapshot().ActiveID != reloadedID {
		return marshalEnvelope(s.ws.ActiveRev(), nil)
	}
	return s.ws.DocumentJSON(reloadedID)
}

// emitExternal 把事件推给前端（没有运行时上下文时什么都不做：测试/无头启动）。
func (s *Service) emitExternal(event externalChangedEvent) {
	ctx := s.runtimeCtx()
	if ctx == nil {
		return
	}
	payload, err := json.Marshal(event)
	if err != nil {
		s.logf("无法编码外部变更事件: %v", err)
		return
	}
	shell.Emit(ctx, "external:changed", string(payload))
}

// logf 是这几条静默路径的日志出口（外部变更失败不该弹窗打断用户，但要看得到）。
func (s *Service) logf(format string, args ...any) {
	fmt.Printf(format+"\n", args...)
}
