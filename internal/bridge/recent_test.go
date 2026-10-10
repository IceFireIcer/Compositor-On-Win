package bridge

// 最近项目列表的测试（票 49）：对齐 RecentProjects.swift 的语义——最近优先、
// 大小写不敏感去重、上限 10、读取时过滤已删除的文件。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// touch 造一个真实存在的文件（List 会 os.Stat 过滤）。
func touch(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRecentStoreAddsMostRecentFirstAndDedupes(t *testing.T) {
	store := NewRecentStoreAt(filepath.Join(t.TempDir(), "recent.json"))
	dir := t.TempDir()
	a := touch(t, dir, "a.comp")
	b := touch(t, dir, "b.comp")

	if err := store.Add(a); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(b); err != nil {
		t.Fatal(err)
	}
	got, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != b || got[1] != a {
		t.Fatalf("列表 = %v，想要最近优先 [b a]", got)
	}

	// 再打开 a：它回到最前，且不重复。
	if err := store.Add(a); err != nil {
		t.Fatal(err)
	}
	got, _ = store.List()
	if len(got) != 2 || got[0] != a {
		t.Fatalf("重复打开后 = %v，想要 [a b]", got)
	}

	// 大小写不同视为同一个项目（Windows 路径）。
	if err := store.Add(strings.ToUpper(a)); err != nil {
		t.Fatal(err)
	}
	got, _ = store.List()
	if len(got) != 2 {
		t.Fatalf("大小写不同不应产生第二项: %v", got)
	}
}

func TestRecentStoreCapsTheList(t *testing.T) {
	store := NewRecentStoreAt(filepath.Join(t.TempDir(), "recent.json"))
	dir := t.TempDir()
	var last string
	for i := 0; i < defaultRecentLimit+3; i++ {
		last = touch(t, dir, fmt.Sprintf("p%d.comp", i))
		if err := store.Add(last); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != defaultRecentLimit {
		t.Fatalf("上限应为 %d，得到 %d", defaultRecentLimit, len(got))
	}
	if got[0] != last {
		t.Fatalf("最新的一项应在最前: %v", got)
	}
}

func TestRecentStoreListDropsMissingFiles(t *testing.T) {
	store := NewRecentStoreAt(filepath.Join(t.TempDir(), "recent.json"))
	dir := t.TempDir()
	kept := touch(t, dir, "kept.comp")
	gone := touch(t, dir, "gone.comp")
	if err := store.Add(gone); err != nil {
		t.Fatal(err)
	}
	if err := store.Add(kept); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	got, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != kept {
		t.Fatalf("已删除的项目应被过滤: %v", got)
	}
	// 过滤只影响读取，落盘内容还在（文件回来后又出现）。
	raw, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "gone.comp") {
		t.Fatalf("过滤不应改写文件: %s", raw)
	}
}

func TestRecentStoreClearAndCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recent.json")
	store := NewRecentStoreAt(path)
	if err := store.Add(touch(t, t.TempDir(), "a.comp")); err != nil {
		t.Fatal(err)
	}
	if err := store.Clear(); err != nil {
		t.Fatal(err)
	}
	if got, err := store.List(); err != nil || len(got) != 0 {
		t.Fatalf("清空后 = %v / %v", got, err)
	}

	// 坏文件：自愈 —— 改名成 .bak 后当空列表，功能不会永久失效。
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := store.List(); err != nil || len(got) != 0 {
		t.Fatalf("坏文件应退化成空列表: %v / %v", got, err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("坏文件应改名成 .bak: %v", err)
	}
	b := touch(t, t.TempDir(), "b.comp")
	if err := store.Add(b); err != nil {
		t.Fatalf("自愈后应能继续记: %v", err)
	}
	if got, _ := store.List(); len(got) != 1 || got[0] != b {
		t.Fatalf("自愈后列表 = %v，想要 [%s]", got, b)
	}
}

// 落盘是原子的：写一半崩掉不会留下半个 JSON（先写 .tmp 再改名）。
func TestRecentStoreWritesAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recent.json")
	store := NewRecentStoreAt(path)
	a := touch(t, t.TempDir(), "a.comp")
	if err := store.Add(a); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("改名后不该留下临时文件: %v", err)
	}
	if raw, err := os.ReadFile(path); err != nil || !strings.Contains(string(raw), "a.comp") {
		t.Fatalf("正式文件应含刚记的路径: %s / %v", raw, err)
	}
}
