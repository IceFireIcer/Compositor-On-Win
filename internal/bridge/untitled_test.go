package bridge

// 票 74 / 上游 issue #231：新标签取**最小的空闲**未命名序号，而不是一路递增。
// 关掉最后一个项目之后应该又拿到「未命名」，而不是继续往上数。
// 原版锚点：ProjectWorkspaceTests.untitledNumbersStayLowAndTheBlankTabStays。

import "testing"

// activeTabOf is the tab the workspace just activated.
func activeTabOf(t *testing.T, snap Snapshot) Document {
	t.Helper()
	for _, tab := range snap.Tabs {
		if tab.ID == snap.ActiveID {
			return tab
		}
	}
	t.Fatalf("快照里没有活动标签 %s", snap.ActiveID)
	return Document{}
}

func TestUntitledNumbersReuseTheLowestFreeSlot(t *testing.T) {
	ws := NewWorkspace()
	first, err := ws.NewDocument(10, 10, 72, "white")
	if err != nil {
		t.Fatal(err)
	}
	if got := activeTabOf(t, first).Name; got != "未命名" {
		t.Fatalf("第一个标签名 = %q，想要 未命名", got)
	}

	second, err := ws.NewDocument(10, 10, 72, "white")
	if err != nil {
		t.Fatal(err)
	}
	third, err := ws.NewDocument(10, 10, 72, "white")
	if err != nil {
		t.Fatal(err)
	}
	if got := activeTabOf(t, second).Name; got != "未命名 2" {
		t.Fatalf("第二个标签名 = %q，想要 未命名 2", got)
	}
	if got := activeTabOf(t, third).Name; got != "未命名 3" {
		t.Fatalf("第三个标签名 = %q，想要 未命名 3", got)
	}

	// 关掉中间那个：下一个应补 2，而不是 4。
	if _, err := ws.CloseTab(activeTabOf(t, second).ID); err != nil {
		t.Fatal(err)
	}
	fourth, err := ws.NewDocument(10, 10, 72, "white")
	if err != nil {
		t.Fatal(err)
	}
	if got := activeTabOf(t, fourth).Name; got != "未命名 2" {
		t.Fatalf("补位标签名 = %q，想要 未命名 2（最小空闲序号）", got)
	}
}

// untitledNumber 只认「未命名」与「未命名 N」，别的名字不占号。
func TestUntitledNumberReadsOnlyUntitledNames(t *testing.T) {
	cases := []struct {
		name string
		want int
		ok   bool
	}{
		{"未命名", 1, true},
		{"未命名 2", 2, true},
		{"未命名 12", 12, true},
		{"未命名 1", 0, false}, // 1 号写作「未命名」，不是「未命名 1」
		{"未命名 x", 0, false},
		{"我的项目", 0, false},
		{"未命名 2 副本", 0, false},
	}
	for _, c := range cases {
		got, ok := untitledNumber(c.name)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%q → (%d, %v)，想要 (%d, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}
