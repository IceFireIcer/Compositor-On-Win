package bridge

// I08：写在后台进行时编辑不该被挡住。上游 ProjectController.save 在**写之前**就把
// isProjectBusy 放开（ProjectController.swift:33-35）：「Only the snapshot (and the
// Save panel) holds the tools. The document is captured, so editing can go on while
// the package is written in the background, as in Photoshop.」
//
// 本仓的快照也在锁内做完（doc 克隆 + 位图克隆），写用的是克隆，所以放开是安全的。

import (
	"path/filepath"
	"testing"

	"compositor-win/internal/domain"
)

func TestEditingContinuesWhileASaveIsWriting(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	sess := activeSessionOf(t, svc)

	entered := make(chan struct{})
	release := make(chan struct{})
	svc.ws.savePackage = func(path string, doc *domain.Document, assets map[string][]byte) error {
		close(entered)
		<-release
		return nil
	}

	done := make(chan error, 1)
	go func() {
		_, err := svc.ws.saveTab(sess.doc.DocumentID, filepath.Join(t.TempDir(), "bg.comp"))
		done <- err
	}()
	<-entered // 写已经进了文件边界，停在锁外

	// 写在后台跑着：编辑应当照常成功。用笔触这条真被 sess.saving 挡过的路。
	// 笔触要走完（End 才提交历史），否则标签不会变脏，也就测不出「保存不该洗白
	// 快照之后的编辑」。
	token, err := svc.BeginPaintStroke(sess.doc.DocumentID, `{"diameter":8,"hardness":1,"r":1}`, "", 12, 12)
	if err != nil {
		close(release)
		t.Fatalf("保存写入期间落笔被挡住了: %v", err)
	}
	if err := svc.PaintStrokePoint(token, 20, 20); err != nil {
		close(release)
		t.Fatalf("保存写入期间拖笔被挡住了: %v", err)
	}
	if _, err := svc.EndPaintStroke(token, false); err != nil {
		close(release)
		t.Fatalf("保存写入期间抬笔被挡住了: %v", err)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("保存: %v", err)
	}
	// 编辑发生在快照之后：标签必须仍是脏的，不能因为一次「成功保存」就洗白。
	if !svc.ws.Snapshot().Tabs[0].Dirty {
		t.Fatal("保存期间发生的编辑应让标签保持脏")
	}
}

// 反过来：快照期间（有未完成的笔触/会话）仍然要拒绝保存，这条没动。
func TestSaveStillRefusesWhileAnEditSessionIsOpen(t *testing.T) {
	svc, _ := newTestService(t, 32, 32)
	tab := svc.ws.activeID()
	if _, err := svc.BeginStroke(10, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ws.saveTab(tab, filepath.Join(t.TempDir(), "busy.comp")); err == nil {
		t.Fatal("有未完成的笔触时保存应当被拒绝")
	}
}
