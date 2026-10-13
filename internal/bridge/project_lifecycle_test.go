package bridge

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"compositor-win/internal/domain"
	"compositor-win/internal/project"
)

func TestSaveProjectAsDialogPicksNewPathForSavedDocument(t *testing.T) {
	svc, tabID := newTestService(t, 16, 16)
	t.Cleanup(svc.stopWatchingProjects)
	originalPath := filepath.Join(t.TempDir(), "original.comp")
	newPath := filepath.Join(t.TempDir(), "copy.comp")
	svc.saveDialog = func(context.Context) (string, error) { return originalPath, nil }
	if _, err := svc.SaveProjectDialog(); err != nil {
		t.Fatal(err)
	}
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	mustLayerOp(t, svc, "rename", `{"id":"`+layerID+`","name":"Save As edit"}`)
	picked := false
	svc.saveDialog = func(context.Context) (string, error) {
		picked = true
		return newPath, nil
	}
	reply, err := svc.SaveProjectAsDialog()
	if err != nil {
		t.Fatal(err)
	}
	if !picked || parseSaveResult(t, reply).Path != newPath {
		t.Fatalf("Save As must pick and report a new destination: picked=%v reply=%s", picked, reply)
	}
	var store project.Store
	original, err := store.Open(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	copyDoc, err := store.Open(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if original.Layers[0].Name != "背景" || copyDoc.Layers[0].Name != "Save As edit" {
		t.Fatalf("Save As changed the original package or missed edits: original=%q new=%q", original.Layers[0].Name, copyDoc.Layers[0].Name)
	}
	if path, dirty, ok := svc.ws.TabState(tabID); !ok || path != newPath || dirty {
		t.Fatalf("Save As tab state = path %q dirty %v exists %v", path, dirty, ok)
	}
}

func TestSaveLayerMaskUsesGray8PNGAndReopens(t *testing.T) {
	svc, tabID := newTestService(t, 4, 4)
	t.Cleanup(svc.stopWatchingProjects)
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	if _, err := svc.SetLayerMask(tabID, layerID, "createHideAll", ""); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "mask-round-trip.comp")
	if _, err := svc.ws.SaveActive(path); err != nil {
		t.Fatalf("保存带蒙版的项目失败: %v", err)
	}
	maskPath := filepath.Join(path, "images", layerID+".mask.png")
	data, err := os.ReadFile(maskPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 26 {
		t.Fatalf("蒙版 PNG 太短: %d", len(data))
	}
	if data[24] != 8 || data[25] != 0 {
		t.Fatalf("蒙版 PNG IHDR = bit depth %d, color type %d; want 8-bit grayscale", data[24], data[25])
	}
	if _, err := (&project.Store{}).Open(path); err != nil {
		t.Fatalf("保存的蒙版项目无法通过 Store.Open: %v", err)
	}

	reopened := NewService(NewWorkspace())
	t.Cleanup(reopened.stopWatchingProjects)
	if _, err := reopened.OpenPath(path); err != nil {
		t.Fatalf("保存的蒙版项目无法重开: %v", err)
	}
	openedLayer := mustDocumentSnapshot(t, reopened).Doc.Layers[0]
	if openedLayer.MaskFile == nil || *openedLayer.MaskFile != layerID+".mask.png" {
		t.Fatalf("重开的图层蒙版记录丢失: %+v", openedLayer)
	}
}

// The native picker can remain open while a different tab becomes active.
// Save must keep the document that initiated the command as its target.
func TestSaveProjectDialogKeepsOriginalTabWhenSelectionChanges(t *testing.T) {
	svc, originalID := newTestService(t, 16, 16)
	path := filepath.Join(t.TempDir(), "original.comp")
	var otherID string
	svc.saveDialog = func(context.Context) (string, error) {
		other, err := svc.ws.NewDocument(32, 24, 144)
		if err != nil {
			return "", err
		}
		otherID = other.ActiveID
		return path, nil
	}
	if _, err := svc.SaveProjectDialog(); err != nil {
		t.Fatal(err)
	}
	var store project.Store
	saved, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.DocumentID != originalID || saved.Width != 16 || saved.Height != 16 {
		t.Fatalf("save wrote another tab: id=%s size=%dx%d; want original %s 16x16", saved.DocumentID, saved.Width, saved.Height, originalID)
	}
	snapshot := svc.ws.Snapshot()
	if snapshot.ActiveID != otherID || len(snapshot.Tabs) != 2 || snapshot.Tabs[0].Dirty || !snapshot.Tabs[1].Dirty {
		t.Fatalf("save changed another tab's state: %+v", snapshot)
	}
}

func TestSaveProjectDialogRejectsPathOwnedByAnotherTab(t *testing.T) {
	svc, firstID := newTestService(t, 16, 16)
	destination := filepath.Join(t.TempDir(), "destination.comp")

	// Keep the first tab unnamed so SaveProjectDialog takes the picker path.
	second, err := svc.ws.NewDocument(32, 24, 144)
	if err != nil {
		t.Fatal(err)
	}
	secondID := second.ActiveID
	secondDocID := mustDocumentSnapshot(t, svc).Doc.DocumentID
	svc.saveDialog = func(context.Context) (string, error) { return destination, nil }
	if _, err := svc.SaveProjectDialog(); err != nil {
		t.Fatalf("保存目标标签失败: %v", err)
	}
	if _, err := svc.ws.SelectTab(firstID); err != nil {
		t.Fatal(err)
	}

	// A Save As from the first tab must not overwrite the package owned by the
	// second tab or create two live sessions for one package path.
	_, err = svc.SaveProjectDialog()
	if err == nil || !strings.Contains(err.Error(), "PATH_CONFLICT") {
		t.Fatalf("已被其他标签占用的保存路径应在写盘前拒绝，错误=%v", err)
	}
	if path, _, ok := svc.ws.TabState(firstID); !ok || path != "" {
		t.Fatalf("冲突保存不应给发起标签分配目标路径: path=%q ok=%v", path, ok)
	}
	if path, _, ok := svc.ws.TabState(secondID); !ok || path != destination {
		t.Fatalf("冲突保存不应改变目标标签: path=%q ok=%v", path, ok)
	}
	saved, err := (&project.Store{}).Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	if saved.DocumentID != secondDocID {
		t.Fatalf("冲突保存覆盖了目标包: documentID=%s want=%s", saved.DocumentID, secondDocID)
	}
	if got := len(svc.ws.Snapshot().Tabs); got != 2 {
		t.Fatalf("冲突保存不应复制/删除标签: got %d", got)
	}
}

func TestSaveReservesDestinationAgainstConcurrentOpen(t *testing.T) {
	svc, savingID := newTestService(t, 16, 16)
	destination := filepath.Join(t.TempDir(), "reserved.comp")
	started, release := make(chan struct{}), make(chan struct{})
	var store project.Store
	svc.ws.savePackage = func(path string, doc *domain.Document, assets map[string][]byte) error {
		close(started)
		<-release
		return store.Save(path, doc, assets)
	}

	saved := make(chan error, 1)
	go func() {
		_, err := svc.ws.SaveActive(destination)
		saved <- err
	}()
	<-started

	openedDoc, openedBitmaps := newDocumentModel(8, 8, 72)
	_, openErr := svc.ws.OpenDocument(destination, openedDoc, openedBitmaps)
	close(release)
	if err := <-saved; err != nil {
		t.Fatal(err)
	}
	if openErr == nil || !strings.Contains(openErr.Error(), "PATH_CONFLICT") {
		t.Fatalf("写盘中的目标应拒绝被另一个标签占用，错误=%v", openErr)
	}
	if got := len(svc.ws.Snapshot().Tabs); got != 1 {
		t.Fatalf("被拒绝的打开不应增加标签: %d", got)
	}
	if path, _, ok := svc.ws.TabState(savingID); !ok || path != destination {
		t.Fatalf("保存完成后发起标签应拥有目标路径: path=%q ok=%v", path, ok)
	}
}

func TestOpenProjectDialogAcceptsManifestInsideCompPackage(t *testing.T) {
	packagePath := filepath.Join(t.TempDir(), "picker.comp")
	writeAgentPackage(t, packagePath, "磁盘内容")

	svc := NewService(NewWorkspace())
	t.Cleanup(svc.stopWatchingProjects)
	svc.openDialog = func(context.Context) (string, error) {
		return filepath.Join(packagePath, "manifest.json"), nil
	}
	if _, err := svc.OpenProjectDialog(); err != nil {
		t.Fatalf("选择 .comp 内的 manifest 后应打开包: %v", err)
	}
	if _, ok := svc.ws.TabIDForPath(packagePath); !ok {
		t.Fatalf("打开后工作区应持有包目录，而不是 manifest 路径")
	}
}

func TestOpenPathReusesExistingDirtySession(t *testing.T) {
	packagePath := filepath.Join(t.TempDir(), "reopen.comp")
	writeAgentPackage(t, packagePath, "磁盘内容")

	svc, _ := newTestService(t, 16, 16)
	t.Cleanup(svc.stopWatchingProjects)
	if _, err := svc.OpenPath(packagePath); err != nil {
		t.Fatal(err)
	}
	opened := svc.ws.Snapshot()
	if len(opened.Tabs) != 2 {
		t.Fatalf("打开项目后应有两个标签: %+v", opened)
	}
	tabID, ok := svc.ws.TabIDForPath(packagePath)
	if !ok {
		t.Fatal("打开后找不到项目标签")
	}
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	mustLayerOp(t, svc, "rename", `{"id":"`+layerID+`","name":"现场编辑"}`)
	before := mustDocumentSnapshot(t, svc)
	if before.Doc.Layers[0].Name != "现场编辑" {
		t.Fatalf("现场编辑未落地: %q", before.Doc.Layers[0].Name)
	}
	// A package may be temporarily unreadable while an agent is replacing its
	// manifest. Re-opening the already-open path must not turn that transient
	// disk state into a destructive reload/error.
	if err := os.Remove(filepath.Join(packagePath, "manifest.json")); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.OpenPath(packagePath); err != nil {
		t.Fatal(err)
	}
	after := svc.ws.Snapshot()
	if len(after.Tabs) != 2 || after.ActiveID != tabID {
		t.Fatalf("重复打开应激活原标签而不是创建/替换标签: %+v", after)
	}
	if !after.Tabs[1].Dirty {
		t.Fatalf("重复打开不应清掉现场编辑的脏标记: %+v", after.Tabs)
	}
	current := mustDocumentSnapshot(t, svc)
	if current.Doc.Layers[0].Name != "现场编辑" {
		t.Fatalf("重复打开覆盖了现场编辑: %q", current.Doc.Layers[0].Name)
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatalf("重复打开不应清空历史: %v", err)
	}
	if got := mustDocumentSnapshot(t, svc).Doc.Layers[0].Name; got != "磁盘内容" {
		t.Fatalf("保留的历史撤销结果 = %q", got)
	}
}

func TestOpenCopiedPackageRejectsDuplicateDocumentIDWithoutReplacingFirstTab(t *testing.T) {
	ws := NewWorkspace()
	firstPath := filepath.Join(t.TempDir(), "original.comp")
	copyPath := filepath.Join(t.TempDir(), "copy.comp")
	first, firstBitmaps := newDocumentModel(16, 16, 72)
	first.Layers[0].Name = "Original"
	opened, err := ws.OpenDocument(firstPath, first, firstBitmaps)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ws.EditActive("Rename", func(sess *session) error {
		sess.doc.Layers[0].Name = "Unsaved"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// A filesystem copy legitimately retains the manifest's documentID, but
	// workspace tab IDs and render URLs must remain unique per live session.
	copyDoc, copyBitmaps := newDocumentModel(16, 16, 72)
	copyDoc.DocumentID = first.DocumentID
	copyDoc.Layers[0].Name = "Copy"
	if _, err := ws.OpenDocument(copyPath, copyDoc, copyBitmaps); err == nil || !strings.Contains(err.Error(), "DOCUMENT_ID_CONFLICT") {
		t.Fatalf("打开同 ID 的拷贝应在覆盖现场会话前拒绝，错误=%v", err)
	}
	if snapshot := ws.Snapshot(); len(snapshot.Tabs) != 1 || snapshot.ActiveID != opened.ActiveID || !snapshot.Tabs[0].Dirty {
		t.Fatalf("冲突打开改变了已有标签: %+v", snapshot)
	}
	encoded, err := ws.ActiveDocumentJSON()
	if err != nil || !strings.Contains(encoded, "Unsaved") {
		t.Fatalf("冲突打开覆盖了本地编辑: document=%s err=%v", encoded, err)
	}
	if _, err := ws.UndoActive(); err != nil {
		t.Fatalf("冲突打开清空了原标签历史: %v", err)
	}
}

func TestReloadDocumentRejectsDirtySession(t *testing.T) {
	ws := NewWorkspace()
	path := filepath.Join(t.TempDir(), "guarded-reload.comp")
	doc, bitmaps := newDocumentModel(16, 16, 72)
	opened, err := ws.OpenDocument(path, doc, bitmaps)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ws.EditActive("本地修改", func(sess *session) error {
		sess.doc.Layers[0].Name = "本地版本"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	replacement, replacementBitmaps := newDocumentModel(16, 16, 72)
	replacement.DocumentID = doc.DocumentID
	replacement.Layers[0].Name = "磁盘版本"
	if _, _, _, err := ws.ReloadDocument(path, replacement, replacementBitmaps); err == nil || !strings.Contains(err.Error(), "DOCUMENT_DIRTY") {
		t.Fatalf("脏文档的公开重载接口应拒绝覆盖，错误=%v", err)
	}
	if got := ws.sessions[opened.ActiveID].doc.Layers[0].Name; got != "本地版本" {
		t.Fatalf("拒绝重载后本地修改被覆盖: %q", got)
	}
}

func TestReloadDocumentRejectsDifferentDocumentID(t *testing.T) {
	ws := NewWorkspace()
	path := filepath.Join(t.TempDir(), "identity.comp")
	initial, bitmaps := newDocumentModel(16, 16, 72)
	opened, err := ws.OpenDocument(path, initial, bitmaps)
	if err != nil {
		t.Fatal(err)
	}
	replacement, replacementBitmaps := newDocumentModel(16, 16, 72)
	if _, _, _, err := ws.ReloadDocument(path, replacement, replacementBitmaps); err == nil || !strings.Contains(err.Error(), "DOCUMENT_ID_CONFLICT") {
		t.Fatalf("外部版本换了文档 ID 时不应破坏标签/渲染身份，错误=%v", err)
	}
	// Explicitly discarding local edits authorizes replacement of content, not
	// changing the tab/document ID that the canvas and render URL use.
	if _, _, _, err := ws.reloadDocument(path, replacement, replacementBitmaps, true); err == nil || !strings.Contains(err.Error(), "DOCUMENT_ID_CONFLICT") {
		t.Fatalf("强制重载也不应改变标签/渲染身份，错误=%v", err)
	}
	if snapshot := ws.Snapshot(); snapshot.ActiveID != opened.ActiveID || len(snapshot.Tabs) != 1 {
		t.Fatalf("身份冲突重载改变了标签: %+v", snapshot)
	}
	encoded, err := ws.ActiveDocumentJSON()
	if err != nil || !strings.Contains(encoded, initial.DocumentID) {
		t.Fatalf("身份冲突重载覆盖了会话: document=%s err=%v", encoded, err)
	}
}

func TestSaveProjectDoesNotBlockEditsOrMarkNewerRevisionSaved(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	path := filepath.Join(t.TempDir(), "overlap.comp")
	svc.saveDialog = func(context.Context) (string, error) { return path, nil }
	started, release := make(chan struct{}), make(chan struct{})
	var store project.Store
	svc.ws.savePackage = func(path string, doc *domain.Document, assets map[string][]byte) error {
		close(started)
		<-release
		return store.Save(path, doc, assets)
	}
	type result struct {
		raw string
		err error
	}
	saved := make(chan result, 1)
	go func() {
		raw, err := svc.SaveProjectDialog()
		saved <- result{raw, err}
	}()
	<-started
	edited := make(chan error, 1)
	go func() {
		_, err := svc.LayerOp("rename", `{"id":"`+layerID+`","name":"Newer edit"}`)
		edited <- err
	}()
	select {
	case err := <-edited:
		if err != nil {
			close(release)
			<-saved
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(release)
		<-saved
		<-edited
		t.Fatal("editing is blocked behind the package write")
	}
	close(release)
	r := <-saved
	if r.err != nil {
		t.Fatal(r.err)
	}
	if parseSaveResult(t, r.raw).Rev != 0 {
		t.Fatalf("save reported a revision it did not capture: %s", r.raw)
	}
	disk, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if disk.Layers[0].Name != "背景" {
		t.Fatalf("package changed during save: layer name = %q", disk.Layers[0].Name)
	}
	if !svc.ws.Snapshot().Tabs[0].Dirty || mustDocumentSnapshot(t, svc).Doc.Layers[0].Name != "Newer edit" {
		t.Fatal("newer edit was discarded or marked saved")
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if svc.ws.Snapshot().Tabs[0].Dirty {
		t.Fatal("undoing to the saved revision must make the document clean")
	}
	if _, err := svc.Redo(); err != nil {
		t.Fatal(err)
	}
	if !svc.ws.Snapshot().Tabs[0].Dirty {
		t.Fatal("redoing the unsaved edit must make the document dirty")
	}
}

func TestSaveProjectPausesWatcherAcrossAtomicWrite(t *testing.T) {
	svc, tabID := newTestService(t, 16, 16)
	t.Cleanup(svc.stopWatchingProjects)
	path := filepath.Join(t.TempDir(), "watch-save.comp")
	svc.saveDialog = func(context.Context) (string, error) { return path, nil }
	if _, err := svc.SaveProjectDialog(); err != nil {
		t.Fatalf("保存监视基线失败: %v", err)
	}
	if svc.watchedEntry(tabID) == nil {
		t.Fatal("保存成功后应建立项目监视器")
	}

	started, release := make(chan struct{}), make(chan struct{})
	var store project.Store
	svc.ws.savePackage = func(path string, doc *domain.Document, assets map[string][]byte) error {
		close(started)
		<-release
		return store.Save(path, doc, assets)
	}
	saved := make(chan error, 1)
	go func() {
		_, err := svc.SaveProjectDialog()
		saved <- err
	}()
	<-started

	if entry := svc.watchedEntry(tabID); entry != nil {
		t.Fatal("项目保存期间不应让旧监视器观察自己的原子写入")
	}
	close(release)
	if err := <-saved; err != nil {
		t.Fatal(err)
	}
	if entry := svc.watchedEntry(tabID); entry == nil {
		t.Fatal("保存完成后应恢复项目监视器")
	}
}

func TestSaveProjectRejectsActiveAdjustmentPreview(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	added := mustLayerOp(t, svc, "addAdjustment", payloadJSON(t, map[string]any{
		"kind": string(domain.AdjustmentLevels),
	}))
	adjustment := added.Doc.Layers[1]
	if adjustment.Adjustment == nil {
		t.Fatal("新建调整图层缺少 adjustment")
	}

	path := filepath.Join(t.TempDir(), "preview-save.comp")
	svc.saveDialog = func(context.Context) (string, error) { return path, nil }
	if _, err := svc.SaveProjectDialog(); err != nil {
		t.Fatalf("保存预览前的基线失败: %v", err)
	}

	preview := *adjustment.Adjustment
	preview.Levels.Ranges[0].Black = 32
	payload := payloadJSON(t, map[string]any{"adjustment": &preview})
	if _, err := svc.BeginFilterEdit("adjust:Levels", adjustment.ID, payload, 1, ""); err != nil {
		t.Fatalf("打开调整图层编辑失败: %v", err)
	}
	if _, err := svc.UpdateFilterPreview(payload); err != nil {
		t.Fatalf("更新调整预览失败: %v", err)
	}

	if _, err := svc.SaveProjectDialog(); err == nil {
		t.Fatal("调整预览尚未提交时不应保存项目")
	}

	disk, err := (&project.Store{}).Open(path)
	if err != nil {
		t.Fatalf("读取基线项目失败: %v", err)
	}
	if got := disk.Layers[1].Adjustment.Levels.Ranges[0].Black; got != 0 {
		t.Fatalf("拒绝预览保存后磁盘仍被污染: black=%v", got)
	}
	if _, err := svc.CancelFilterEdit(); err != nil {
		t.Fatalf("取消调整预览失败: %v", err)
	}
}

func TestTryCloseTabRejectsUnsavedChanges(t *testing.T) {
	svc, tabID := newTestService(t, 16, 16)
	path := filepath.Join(t.TempDir(), "close.comp")
	if _, err := svc.ws.SaveActive(path); err != nil {
		t.Fatal(err)
	}
	layerID := *mustDocumentSnapshot(t, svc).Doc.ActiveLayerID
	mustLayerOp(t, svc, "rename", `{"id":"`+layerID+`","name":"Unsaved"}`)
	if _, err := svc.ws.TryCloseTab(tabID); err == nil {
		t.Fatal("automatic close discarded an unsaved edit")
	}
	if len(svc.ws.Snapshot().Tabs) != 1 || mustDocumentSnapshot(t, svc).Doc.Layers[0].Name != "Unsaved" {
		t.Fatal("rejected close must preserve the live document")
	}
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ws.TryCloseTab(tabID); err != nil {
		t.Fatalf("clean close failed: %v", err)
	}
	if len(svc.ws.Snapshot().Tabs) != 0 {
		t.Fatal("clean tab was not closed")
	}
}

func TestTryCloseTabRejectsWhileSaveIsInFlight(t *testing.T) {
	svc, tabID := newTestService(t, 16, 16)
	path := filepath.Join(t.TempDir(), "busy-close.comp")
	if _, err := svc.ws.SaveActive(path); err != nil {
		t.Fatal(err)
	}

	started, release := make(chan struct{}), make(chan struct{})
	var store project.Store
	svc.ws.savePackage = func(path string, doc *domain.Document, assets map[string][]byte) error {
		close(started)
		<-release
		return store.Save(path, doc, assets)
	}
	saved := make(chan error, 1)
	go func() {
		_, err := svc.ws.SaveActive(path)
		saved <- err
	}()
	<-started

	_, closeErr := svc.ws.TryCloseTab(tabID)
	close(release)
	if err := <-saved; err != nil {
		t.Fatal(err)
	}
	if closeErr == nil {
		t.Fatal("automatic close discarded a tab while its package save was in flight")
	}
	if got := len(svc.ws.Snapshot().Tabs); got != 1 {
		t.Fatalf("rejected close removed the tab: %d", got)
	}
}
