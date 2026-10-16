package bridge

// 票 77：文件夹导入。上游锚点 ImageImportTests.folderImportsTheImagesDirectlyInsideIt /
// folderWithoutImagesSaysSo（ImageImportTests.swift:184-213）。

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// writePNG puts a real PNG at dir/name so the decode pass accepts it.
func writePNG(t *testing.T, dir, name string, w, h int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, encodeTestPNG(t, w, h, color.NRGBA{R: 255, A: 255}), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// 直接子级、隐藏项、子目录、包与不支持的文件：只剩两张图，且 2 在 10 前。
func TestFolderImportTakesOnlyTheImagesDirectlyInsideIt(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "More")
	project := filepath.Join(root, "Project.comp")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writePNG(t, root, "10 Ten.png", 4, 4)
	writePNG(t, root, "2 Two.png", 4, 4)
	writePNG(t, root, ".Hidden.png", 4, 4)
	writePNG(t, nested, "Nested.png", 4, 4)
	writePNG(t, project, "InProject.png", 4, 4)
	if err := os.WriteFile(filepath.Join(root, "Notes.txt"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !isPlainFolder(root) {
		t.Fatal("普通文件夹应被认作可导入的文件夹")
	}
	if isPlainFolder(project) {
		t.Fatal(".comp 是磁盘上的目录，但不该从它导入")
	}
	if isPlainFolder(filepath.Join(root, "10 Ten.png")) {
		t.Fatal("文件不是文件夹")
	}

	svc, _ := newTestService(t, 16, 16)
	begin, err := svc.beginImport([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, item := range begin.Items {
		if item.Status != "ok" {
			t.Fatalf("条目 %+v 不该失败", item)
		}
		names = append(names, item.Name)
	}
	if len(names) != 2 || names[0] != "2 Two" || names[1] != "10 Ten" {
		t.Fatalf("导入次序 = %v，想要 [2 Two, 10 Ten]（自然序，直接子级）", names)
	}
}

// Windows 的隐藏是文件属性，不只是点号前缀。
func TestFolderImportSkipsTheHiddenAttribute(t *testing.T) {
	root := t.TempDir()
	writePNG(t, root, "Visible.png", 4, 4)
	hidden := writePNG(t, root, "Secret.png", 4, 4)
	ptr, err := windows.UTF16PtrFromString(hidden)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetFileAttributes(ptr, windows.FILE_ATTRIBUTE_HIDDEN); err != nil {
		t.Fatalf("设置隐藏属性: %v", err)
	}
	defer func() { _ = windows.SetFileAttributes(ptr, windows.FILE_ATTRIBUTE_NORMAL) }()

	paths, err := folderImagePaths(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || filepath.Base(paths[0]) != "Visible.png" {
		t.Fatalf("扫描结果 = %v，想要只有 Visible.png", paths)
	}
}

// 空文件夹有明确结果：作为一条错误条目报出来，而不是静默什么都不发生。
func TestEmptyFolderIsReported(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Notes.txt"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc, _ := newTestService(t, 16, 16)
	begin, err := svc.beginImport([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(begin.Items) != 1 {
		t.Fatalf("条目 = %+v，想要一条错误", begin.Items)
	}
	if begin.Items[0].Status != "error" || !strings.Contains(begin.Items[0].Error, "没有可导入的图像") {
		t.Fatalf("空文件夹的条目 = %+v", begin.Items[0])
	}
}

// 混合批次：文件夹里的图照常进，旁边的散文件也不受影响。
func TestFolderAndFilesImportTogether(t *testing.T) {
	root := t.TempDir()
	writePNG(t, root, "b.png", 4, 4)
	writePNG(t, root, "a.png", 4, 4)
	loose := writePNG(t, t.TempDir(), "Loose.png", 4, 4)

	svc, _ := newTestService(t, 16, 16)
	begin, err := svc.beginImport([]string{loose, root})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, item := range begin.Items {
		names = append(names, item.Name)
	}
	if len(names) != 3 || names[0] != "Loose" || names[1] != "a" || names[2] != "b" {
		t.Fatalf("导入次序 = %v，想要 [Loose a b]（调用方给的次序在前）", names)
	}
}

// 自然序：数字段按数值比，其余按码点比。
func TestNaturalLessOrdersNumbersAsNumbers(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"2 Two", "10 Ten", true},
		{"10 Ten", "2 Two", false},
		{"img2.png", "img10.png", true},
		{"img10.png", "img2.png", false},
		{"a.png", "b.png", true},
		{"b.png", "a.png", false},
		{"same", "same", false},
		{"a", "a1", true},
	}
	for _, c := range cases {
		if got := naturalLess(c.a, c.b); got != c.want {
			t.Errorf("naturalLess(%q, %q) = %v，想要 %v", c.a, c.b, got, c.want)
		}
	}
	// 前导零的平局（img02.png 与 img2.png 数值相等）方向由上游的
	// localizedStandardCompare 决定，这里没核实过，所以只钉**确定性**：两个方向
	// 里恰好一个成立，排序才稳定。
	if naturalLess("img02.png", "img2.png") == naturalLess("img2.png", "img02.png") {
		t.Fatal("数值相等的两个名字必须有确定的先后")
	}
}

// 文件夹扫描接受的就是对话框列出的那些格式 —— 两处不会分叉。
func TestFolderScanAcceptsTheDialogsFormats(t *testing.T) {
	for _, pattern := range strings.Split(imageFilter.Pattern, ";") {
		ext := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(pattern)), "*")
		if ext == "" {
			continue
		}
		if !importableImageExtensions[ext] {
			t.Errorf("对话框接受 %s，文件夹扫描却不认", ext)
		}
	}
}
