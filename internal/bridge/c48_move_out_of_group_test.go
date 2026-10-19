package bridge

// C48 (ticket 53): Move Out of Folder is a real Go op ("moveOutOfGroup"), not
// a frontend-only array rewrite. The menu needs it as one undoable edit, and
// moveLayer alone cannot do it (it only reorders the flat array and never
// touches parentID). These tests pin:
//   - a nested layer leaves the folder for the folder's own level, directly
//     above the folder in the flat array, and the whole step undoes;
//   - a folder moved out keeps its own children;
//   - root-level / missing / absent active layers are rejected without
//     touching the document, the revision or the history.

import (
	"testing"

	"compositor-win/internal/domain"
)

// moveOutFixture builds [bottom, Folder 1(bottom, inner, other), top] and
// returns the service plus the ids involved.
func moveOutFixture(t *testing.T) (svc *Service, bottom, folder, inner, other, top string) {
	t.Helper()
	f := newFixture(t, 16, 16)
	bottom = f.pixel("bottom")
	folder = f.group("Folder 1")
	inner = f.pixel("inner")
	f.under(t, inner, folder)
	other = f.pixel("other")
	f.under(t, other, folder)
	top = f.pixel("top")
	f.active(inner)
	return f.service(t), bottom, folder, inner, other, top
}

func layerIn(t *testing.T, doc *domain.Document, id string) *domain.Layer {
	t.Helper()
	layer := layerAt(doc, id)
	if layer == nil {
		t.Fatalf("文档缺少图层 %s", id)
	}
	return layer
}

func TestMoveOutOfGroupMovesLayerAboveFolderAndUndoes(t *testing.T) {
	svc, bottom, folder, inner, other, top := moveOutFixture(t)
	before := mustDocumentSnapshot(t, svc)
	wantBefore := []string{bottom, folder, inner, other, top}
	if got := flatIDs(before.Doc); !equalStrings(got, wantBefore) {
		t.Fatalf("初始扁平顺序 = %v，想要 %v", got, wantBefore)
	}
	if got := parentIDOf(layerIn(t, before.Doc, inner)); got != folder {
		t.Fatalf("inner 的父级 = %q，想要 %q", got, folder)
	}

	e := mustLayerOp(t, svc, "moveOutOfGroup", "{}")

	// Parent change is the point of the op: moveLayer alone cannot do it.
	if got := parentIDOf(layerIn(t, e.Doc, inner)); got != "" {
		t.Fatalf("移出后 inner 的父级 = %q，想要根级", got)
	}
	// Above the folder, at the folder's own level: flat slot immediately after it.
	wantAfter := []string{bottom, folder, inner, other, top}
	if got := flatIDs(e.Doc); !equalStrings(got, wantAfter) {
		t.Fatalf("移出后扁平顺序 = %v，想要 %v", got, wantAfter)
	}
	// The sibling still inside the folder and the rest of the stack are untouched.
	if got := parentIDOf(layerIn(t, e.Doc, other)); got != folder {
		t.Fatalf("other 的父级 = %q，想要仍在 %q 内", got, folder)
	}
	if e.Doc.ActiveLayerID == nil || *e.Doc.ActiveLayerID != inner {
		t.Fatalf("活动图层 = %v，想要仍是 %s", e.Doc.ActiveLayerID, inner)
	}
	if e.Rev != before.Rev+1 {
		t.Fatalf("rev = %d，想要 %d（一步编辑只推进一次）", e.Rev, before.Rev+1)
	}
	if got := undoNameOf(t, svc); got != "移出文件夹" {
		t.Fatalf("undoName = %q，想要 移出文件夹", got)
	}

	// One undo puts the layer back inside the folder, in its original slot.
	out, err := svc.Undo()
	if err != nil {
		t.Fatalf("撤销移出文件夹失败: %v", err)
	}
	undone := parseEnvelope(t, out)
	if got := flatIDs(undone.Doc); !equalStrings(got, wantBefore) {
		t.Fatalf("撤销后扁平顺序 = %v，想要 %v", got, wantBefore)
	}
	if got := parentIDOf(layerIn(t, undone.Doc, inner)); got != folder {
		t.Fatalf("撤销后 inner 的父级 = %q，想要 %q", got, folder)
	}
	if undone.Doc.ActiveLayerID == nil || *undone.Doc.ActiveLayerID != inner {
		t.Fatalf("撤销后活动图层 = %v，想要 %s", undone.Doc.ActiveLayerID, inner)
	}
}

func TestMoveOutOfGroupKeepsChildrenOfAMovedFolder(t *testing.T) {
	f := newFixture(t, 16, 16)
	outer := f.group("Folder 1")
	nested := f.group("Folder 2")
	f.under(t, nested, outer)
	child := f.pixel("child")
	f.under(t, child, nested)
	top := f.pixel("top")
	f.active(nested)
	svc := f.service(t)

	e := mustLayerOp(t, svc, "moveOutOfGroup", "{}")
	if got := parentIDOf(layerIn(t, e.Doc, nested)); got != "" {
		t.Fatalf("移出后嵌套文件夹的父级 = %q，想要根级", got)
	}
	if got := parentIDOf(layerIn(t, e.Doc, child)); got != nested {
		t.Fatalf("子层父级 = %q，想要仍为 %q", got, nested)
	}
	want := []string{outer, nested, child, top}
	if got := flatIDs(e.Doc); !equalStrings(got, want) {
		t.Fatalf("移出嵌套文件夹后扁平顺序 = %v，想要 %v", got, want)
	}
}

func TestMoveOutOfGroupRejectsRootLevelLayer(t *testing.T) {
	f := newFixture(t, 16, 16)
	root := f.pixel("root")
	f.active(root)
	svc := f.service(t)
	failedLayerOp(t, svc, "moveOutOfGroup", "{}", "不在文件夹")
}

func TestMoveOutOfGroupRejectsMissingLayer(t *testing.T) {
	f := newFixture(t, 16, 16)
	f.pixel("root")
	f.active("missing-layer")
	svc := f.service(t)
	failedLayerOp(t, svc, "moveOutOfGroup", "{}", "活动图层不存在")
}

func TestMoveOutOfGroupRejectsWithoutActiveLayer(t *testing.T) {
	f := newFixture(t, 16, 16)
	f.pixel("root")
	svc := f.service(t)
	failedLayerOp(t, svc, "moveOutOfGroup", "{}", "没有活动图层")
}

func equalStrings(a, b []string) bool {
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
