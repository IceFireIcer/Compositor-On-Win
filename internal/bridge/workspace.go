package bridge

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"compositor-win/internal/domain"
	"compositor-win/internal/project"
	"compositor-win/internal/render"
)

// Document is the shell-stage document descriptor: just enough for the
// tabs, the checkerboard and the dirty marker. Since the M2 wiring each
// tab is backed by a real domain.Document + history session (see session);
// ID/Name/Dirty semantics carry over unchanged.
type Document struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Resolution int    `json:"resolution"`
	Dirty      bool   `json:"dirty"`
}

// Snapshot is the full workspace state handed to the frontend after every
// mutation — commands return state instead of diffing, so the store stays a
// dumb mirror.
type Snapshot struct {
	Tabs     []Document `json:"tabs"`
	ActiveID string     `json:"activeId"`
}

// Workspace holds the open document tabs (single window, in-app tabs like
// the macOS ProjectWorkspace). Every tab owns a session: the real domain
// document, its undo history and its in-memory bitmap library. One mutex
// guards tabs + sessions; all rendering and editing funnels through it.
type Workspace struct {
	mu       sync.Mutex
	tabs     []Document
	active   string
	counter  int
	sessions map[string]*session
}

// NewWorkspace starts with no documents.
func NewWorkspace() *Workspace {
	return &Workspace{sessions: map[string]*session{}}
}

func newDocID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return "doc-" + hex.EncodeToString(b[:])
}

// NewDocument validates against the document limits, appends a tab backed
// by a real document (one white 背景 layer) and activates it. Name is
// 未命名, then 未命名 2, 未命名 3, …
func (w *Workspace) NewDocument(width, height, resolution int) (Snapshot, error) {
	if err := domain.ValidateNewDocument(width, height, resolution); err != nil {
		return Snapshot{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.counter++
	name := "未命名"
	if w.counter > 1 {
		name = fmt.Sprintf("未命名 %d", w.counter)
	}
	doc, bitmaps := newDocumentModel(width, height, resolution)
	tab := Document{
		ID:         newDocID(),
		Name:       name,
		Width:      width,
		Height:     height,
		Resolution: resolution,
		Dirty:      true,
	}
	w.tabs = append(w.tabs, tab)
	w.sessions[tab.ID] = newSession("", doc, bitmaps)
	w.active = tab.ID
	return w.snapshotLocked(), nil
}

// OpenDocument installs a parsed package as a tab: a fresh session (empty
// history, rev 0) around the given document and its decoded bitmaps.
// Re-opening a path that already has a tab replaces that tab's session in
// place — one tab per file. The tab name comes from the package directory.
func (w *Workspace) OpenDocument(path string, doc *domain.Document, bitmaps map[string]*render.Bitmap) (Snapshot, error) {
	if doc == nil {
		return Snapshot{}, fmt.Errorf("文档为空，无法打开")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	name := displayName(path)
	for i := range w.tabs {
		t := &w.tabs[i]
		if sess := w.sessions[t.ID]; sess != nil && sess.path == path {
			w.sessions[t.ID] = newSession(path, doc, bitmaps)
			t.Name = name
			t.Width, t.Height = doc.Width, doc.Height
			t.Dirty = false
			w.active = t.ID
			return w.snapshotLocked(), nil
		}
	}
	res := domain.DefaultResolution
	if doc.Resolution != nil {
		res = *doc.Resolution
	}
	tab := Document{
		ID:         newDocID(),
		Name:       name,
		Width:      doc.Width,
		Height:     doc.Height,
		Resolution: res,
		Dirty:      false,
	}
	w.tabs = append(w.tabs, tab)
	w.sessions[tab.ID] = newSession(path, doc, bitmaps)
	w.active = tab.ID
	return w.snapshotLocked(), nil
}

// SelectTab activates an existing tab.
func (w *Workspace) SelectTab(id string) (Snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.hasLocked(id) {
		return Snapshot{}, fmt.Errorf("标签不存在: %s", id)
	}
	w.active = id
	return w.snapshotLocked(), nil
}

// CloseTab removes a tab and its session. Closing the active tab activates
// its next neighbor (or the previous one at the end); closing the last tab
// leaves the workspace empty and returns to the welcome screen.
func (w *Workspace) CloseTab(id string) (Snapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	index := -1
	for i, d := range w.tabs {
		if d.ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return Snapshot{}, fmt.Errorf("标签不存在: %s", id)
	}
	w.tabs = append(w.tabs[:index], w.tabs[index+1:]...)
	delete(w.sessions, id)
	switch {
	case w.active != id:
		// keep current selection
	case len(w.tabs) == 0:
		w.active = ""
	case index < len(w.tabs):
		w.active = w.tabs[index].ID
	default:
		w.active = w.tabs[len(w.tabs)-1].ID
	}
	return w.snapshotLocked(), nil
}

// Snapshot returns the current state (safe copy).
func (w *Workspace) Snapshot() Snapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.snapshotLocked()
}

// ActiveDocumentJSON marshals the active tab's document envelope:
// {"rev":N,"doc":{...}} — doc is null (rev 0) when nothing is open.
func (w *Workspace) ActiveDocumentJSON() (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return marshalEnvelope(0, nil)
	}
	return marshalEnvelope(sess.rev, sess.doc)
}

// ActiveRev is the active tab's revision, 0 when nothing is open.
func (w *Workspace) ActiveRev() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	if sess := w.activeSessionLocked(); sess != nil {
		return sess.rev
	}
	return 0
}

// ActiveSaveInfo reports the active tab's package path ("" before the
// first save) and its display name, for the save dialog defaults.
func (w *Workspace) ActiveSaveInfo() (path, name string, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return "", "", errNoDocument
	}
	if t := w.activeTabLocked(); t != nil {
		name = t.Name
	}
	return sess.path, name, nil
}

// EditActive runs one named document command against the active session,
// wrapped in history.Begin/End. fn must validate before mutating; if it
// returns an error the working copy is rolled back and no entry is
// recorded. When the command changed the document (history committed an
// entry) the revision bumps and the tab turns dirty. Returns the document
// envelope JSON.
func (w *Workspace) EditActive(name string, fn func(*session) error) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	before := cloneDocForEdit(sess.doc)
	revBefore := sess.hist.Revision()
	sess.hist.Begin(name, *sess.doc, sess.doc.ActiveLayerID)
	err := fn(sess)
	if err != nil {
		sess.doc = before
	}
	sess.hist.End(*sess.doc, sess.doc.ActiveLayerID)
	if err != nil {
		return "", err
	}
	if sess.hist.Revision() != revBefore {
		sess.rev++
		w.setTabDirtyLocked(w.active, true)
	}
	return marshalEnvelope(sess.rev, sess.doc)
}

// UndoActive rolls the active document back one history entry and returns
// the envelope. Undo is itself an observable change, so rev bumps.
func (w *Workspace) UndoActive() (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	snap, ok := sess.hist.Undo()
	if !ok {
		return "", fmt.Errorf("没有可撤销的操作")
	}
	doc := snap.Document
	doc.ActiveLayerID = snap.ActiveLayerID
	sess.doc = &doc
	sess.rev++
	w.setTabDirtyLocked(w.active, sess.path == "" || sess.hist.IsModified())
	return marshalEnvelope(sess.rev, sess.doc)
}

// RedoActive replays the newest undone entry.
func (w *Workspace) RedoActive() (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	snap, ok := sess.hist.Redo()
	if !ok {
		return "", fmt.Errorf("没有可重做的操作")
	}
	doc := snap.Document
	doc.ActiveLayerID = snap.ActiveLayerID
	sess.doc = &doc
	sess.rev++
	w.setTabDirtyLocked(w.active, sess.path == "" || sess.hist.IsModified())
	return marshalEnvelope(sess.rev, sess.doc)
}

// SaveActive writes the active document to the package at path: every
// referenced asset is encoded from the in-memory bitmap library, the store
// commits the package, and the session records its path and saved state.
// Returns the revision to report alongside the path.
func (w *Workspace) SaveActive(path string) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return 0, errNoDocument
	}
	assets := map[string][]byte{}
	for _, name := range referencedAssetNames(sess.doc) {
		bmp, ok := sess.bitmaps[name]
		if !ok || bmp == nil {
			return 0, fmt.Errorf("内存位图库缺少资产 %s，无法保存", name)
		}
		data, err := render.EncodePNG(bmp)
		if err != nil {
			return 0, fmt.Errorf("编码资产 %s 失败: %w", name, err)
		}
		assets[name] = data
	}
	var store project.Store
	if err := store.Save(path, sess.doc, assets); err != nil {
		return 0, err
	}
	sess.path = path
	sess.hist.MarkSaved()
	w.markTabSavedLocked(w.active, path)
	return sess.rev, nil
}

// BeginStroke starts a default-settings brush stroke (size 30, hardness
// 0.5, black, opacity 1) on the active layer's bitmap.
func (w *Workspace) BeginStroke(x, y float64) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	if sess.stroke != nil {
		return "", fmt.Errorf("已有进行中的笔刷")
	}
	if sess.doc.ActiveLayerID == nil {
		return "", fmt.Errorf("没有活动图层")
	}
	idx := findLayerIndex(sess.doc, *sess.doc.ActiveLayerID)
	if idx < 0 {
		return "", fmt.Errorf("活动图层不存在: %s", *sess.doc.ActiveLayerID)
	}
	l := &sess.doc.Layers[idx]
	if l.IsGroupLayer() {
		return "", fmt.Errorf("编组不能绘制")
	}
	if l.ImageFile == nil {
		return "", fmt.Errorf("图层 %s 没有位图，无法绘制", l.ID)
	}
	base, ok := sess.bitmaps[*l.ImageFile]
	if !ok || base == nil {
		return "", fmt.Errorf("内存位图库缺少资产 %s", *l.ImageFile)
	}
	st, err := render.NewStroke(render.TileGridFromBitmap(base), defaultBrushSettings())
	if err != nil {
		return "", err
	}
	st.Append(render.Point{X: x, Y: y})
	sess.stroke = st
	sess.strokeKey = *l.ImageFile
	return marshalEnvelope(sess.rev, sess.doc)
}

// StrokePoint extends the in-flight stroke. The manifest is untouched, so
// rev stays put; the canvas updates through /render after EndStroke.
func (w *Workspace) StrokePoint(x, y float64) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	if sess.stroke == nil {
		return "", fmt.Errorf("没有进行中的笔刷")
	}
	sess.stroke.Append(render.Point{X: x, Y: y})
	return marshalEnvelope(sess.rev, sess.doc)
}

// EndStroke commits the stroke back into the bitmap library and bumps rev
// so the render cache invalidates.
func (w *Workspace) EndStroke() (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.activeSessionLocked()
	if sess == nil {
		return "", errNoDocument
	}
	if sess.stroke == nil {
		return "", fmt.Errorf("没有进行中的笔刷")
	}
	grid := sess.stroke.Commit()
	sess.stroke = nil
	key := sess.strokeKey
	if key == "" {
		return "", fmt.Errorf("笔刷没有目标资产")
	}
	sess.bitmaps[key] = grid.Bitmap()
	sess.strokeKey = ""
	sess.rev++
	w.setTabDirtyLocked(w.active, true)
	return marshalEnvelope(sess.rev, sess.doc)
}

// RenderPNG composes the document identified by tab ID or domain
// documentID into PNG bytes, served from the per-session cache until rev
// changes. ok=false means no such document.
func (w *Workspace) RenderPNG(docID string) (png []byte, ok bool, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	sess := w.sessionByIDLocked(docID)
	if sess == nil {
		return nil, false, nil
	}
	if sess.renderPNG == nil || sess.renderRev != sess.rev {
		bmp, err := render.Render(sess.doc, sess.pixelSource())
		if err != nil {
			return nil, true, err
		}
		data, err := render.EncodePNG(bmp)
		if err != nil {
			return nil, true, err
		}
		sess.renderPNG = data
		sess.renderRev = sess.rev
	}
	return sess.renderPNG, true, nil
}

func (w *Workspace) activeSessionLocked() *session {
	if w.active == "" {
		return nil
	}
	return w.sessions[w.active]
}

func (w *Workspace) activeTabLocked() *Document {
	for i := range w.tabs {
		if w.tabs[i].ID == w.active {
			return &w.tabs[i]
		}
	}
	return nil
}

// sessionByIDLocked resolves a render URL's docID: tab ID first, then the
// domain documentID (both are unique identifier namespaces).
func (w *Workspace) sessionByIDLocked(id string) *session {
	if sess, ok := w.sessions[id]; ok {
		return sess
	}
	for _, t := range w.tabs {
		if sess := w.sessions[t.ID]; sess != nil && sess.doc.DocumentID == id {
			return sess
		}
	}
	return nil
}

func (w *Workspace) setTabDirtyLocked(id string, dirty bool) {
	for i := range w.tabs {
		if w.tabs[i].ID == id {
			w.tabs[i].Dirty = dirty
			return
		}
	}
}

// markTabSavedLocked renames the tab after the saved package and clears
// its dirty marker.
func (w *Workspace) markTabSavedLocked(id, path string) {
	w.setTabDirtyLocked(id, false)
	for i := range w.tabs {
		if w.tabs[i].ID == id {
			w.tabs[i].Name = displayName(path)
			return
		}
	}
}

func (w *Workspace) hasLocked(id string) bool {
	for _, d := range w.tabs {
		if d.ID == id {
			return true
		}
	}
	return false
}

func (w *Workspace) snapshotLocked() Snapshot {
	tabs := make([]Document, len(w.tabs))
	copy(tabs, w.tabs)
	return Snapshot{Tabs: tabs, ActiveID: w.active}
}

// displayName strips the .comp suffix (any case) from a package path for
// tab display.
func displayName(path string) string {
	base := filepath.Base(path)
	if ext := filepath.Ext(base); strings.EqualFold(ext, ".comp") {
		base = base[:len(base)-len(ext)]
	}
	return base
}
