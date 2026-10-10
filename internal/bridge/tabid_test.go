package bridge

import "testing"

// 回归：**标签 ID 必须等于其会话文档的 domain.DocumentID**。
//
// 前端把 /render/{id}.png 与当前文档快照按这个值配对（App.svelte 的
// loadDocument(expectedDocId)、CanvasSurface 的 renderSrc 守卫）。这两处
// 曾经各自生成一个 ID（标签 "doc-<hex>"、文档 RFC 4122 UUID），于是：
//   - App 的标签同步 effect 传标签 ID 当期望值，快照因不匹配被**静默丢弃**；
//   - CanvasSurface 的 renderSrc 守卫永远为假，画布**永远不显示合成结果**。
//
// 表现就是"画笔/填充都在跑、画布却一直是棋盘格"。
//
// 所以这个不变量是前端可见性的地基，必须钉住。
func TestTabIDEqualsDocumentID(t *testing.T) {
	svc, activeID := newTestService(t, 32, 32)

	sess := activeSessionOf(t, svc)
	if activeID != sess.doc.DocumentID {
		t.Fatalf("标签 ID = %q，文档 ID = %q；两者必须一致", activeID, sess.doc.DocumentID)
	}

	// 工作区快照里暴露给前端的 id 也必须是同一个值。
	svc.ws.mu.Lock()
	snap := svc.ws.snapshotLocked()
	svc.ws.mu.Unlock()
	if len(snap.Tabs) != 1 {
		t.Fatalf("标签数 = %d，想要 1", len(snap.Tabs))
	}
	if snap.Tabs[0].ID != sess.doc.DocumentID {
		t.Fatalf("快照标签 ID = %q，文档 ID = %q", snap.Tabs[0].ID, sess.doc.DocumentID)
	}
	if snap.ActiveID != sess.doc.DocumentID {
		t.Fatalf("快照 activeId = %q，文档 ID = %q", snap.ActiveID, sess.doc.DocumentID)
	}

	// 文档快照信封里的 docId 与标签 ID 一致 —— 前端的期望值比较靠它。
	env := mustDocumentSnapshot(t, svc)
	if env.Doc == nil || env.Doc.DocumentID != activeID {
		t.Fatalf("信封 docId = %v，标签 ID = %q", env.Doc, activeID)
	}
}

// 同一份 .comp 重复打开时，标签 ID 仍等于文档 ID（.comp 的 documentID 稳定）。
func TestReopenKeepsTabIDEqualToDocumentID(t *testing.T) {
	ws := NewWorkspace()
	doc, bitmaps := newDocumentModel(24, 24, 72)
	path := "C:\\tmp\\demo.comp"

	if _, err := ws.OpenDocument(path, doc, bitmaps); err != nil {
		t.Fatal(err)
	}
	first := ws.activeID()
	if first != doc.DocumentID {
		t.Fatalf("首次打开：标签 ID = %q，文档 ID = %q", first, doc.DocumentID)
	}

	// 重新解析同一份包：ID 来自 manifest，因此不变。
	doc2, bitmaps2 := newDocumentModel(24, 24, 72)
	doc2.DocumentID = doc.DocumentID
	if _, err := ws.OpenDocument(path, doc2, bitmaps2); err != nil {
		t.Fatal(err)
	}
	if got := ws.activeID(); got != doc.DocumentID {
		t.Fatalf("重复打开后标签 ID = %q，文档 ID = %q", got, doc.DocumentID)
	}
	if n := len(ws.tabs); n != 1 {
		t.Fatalf("同一路径应只有一个标签，实际 %d", n)
	}
}
