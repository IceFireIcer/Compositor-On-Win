package bridge

// 文件夹导入（票 77）：把普通文件夹的直接子级图像展开进导入批次。
//
// 上游 ImageFolder（ImageImporter.swift:23-43）：
//   matches —— 是目录、不是包（.comp 也是磁盘上的目录，但不该从它导入）；
//   images  —— 直接子级里的**常规文件**且类型可导入，跳过隐藏项，按 Finder 的名字序
//              （localizedStandardCompare，所以 "2" 排在 "10" 前）。
//
// 展开点选在 beginImport：BeginImageImport、ImportDroppedPaths 与
// ImportDroppedPathsAsNewProject 都走它，所以导入对话框、画布、标签栏、Dock 四条入口
// 天然一致（票 77 验收 3），不会出现「对话框认文件夹、拖放不认」这种分裂。

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/sys/windows"
)

// importableImageExtensions is what a folder scan accepts, derived from the
// dialog's own filter so the two can never list different formats.
var importableImageExtensions = func() map[string]bool {
	out := map[string]bool{}
	for _, pattern := range strings.Split(imageFilter.Pattern, ";") {
		if ext := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(pattern)), "*"); ext != "" {
			out[ext] = true
		}
	}
	return out
}()

// isPlainFolder is ImageFolder.matches: a real directory, and not a package.
// A .comp is a directory on disk too, but it is a project, not a source of
// loose images, so a drop on it opens the project instead.
func isPlainFolder(path string) bool {
	if strings.EqualFold(filepath.Ext(path), ".comp") {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// folderImagePaths is ImageFolder.images: the importable images directly
// inside folder, hidden ones and subfolders left out, in natural name order.
func folderImagePaths(folder string) ([]string, error) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if isHiddenEntry(filepath.Join(folder, name), name) {
			continue
		}
		// Direct children only, and regular files only: a subfolder is not an
		// image however its name ends.
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if !importableImageExtensions[strings.ToLower(filepath.Ext(name))] {
			continue
		}
		out = append(out, filepath.Join(folder, name))
	}
	sort.Slice(out, func(i, j int) bool {
		return naturalLess(filepath.Base(out[i]), filepath.Base(out[j]))
	})
	return out, nil
}

// isHiddenEntry follows FileManager's skipsHiddenFiles. macOS hides by the dot
// prefix; on Windows the attribute is the real one, so both are honoured.
func isHiddenEntry(path, name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(ptr)
	if err != nil {
		return false
	}
	return attrs&windows.FILE_ATTRIBUTE_HIDDEN != 0
}

// naturalLess is Finder's name order: a run of digits compares as a number, so
// "2 Two" sorts before "10 Ten". Everything else compares by rune.
func naturalLess(a, b string) bool {
	ar, br := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ar) && j < len(br) {
		if unicode.IsDigit(ar[i]) && unicode.IsDigit(br[j]) {
			si, sj := i, j
			for i < len(ar) && unicode.IsDigit(ar[i]) {
				i++
			}
			for j < len(br) && unicode.IsDigit(br[j]) {
				j++
			}
			// Leading zeros do not change the value.
			da := strings.TrimLeft(string(ar[si:i]), "0")
			db := strings.TrimLeft(string(br[sj:j]), "0")
			if len(da) != len(db) {
				return len(da) < len(db)
			}
			if da != db {
				return da < db
			}
			continue
		}
		if ar[i] != br[j] {
			return ar[i] < br[j]
		}
		i++
		j++
	}
	return len(ar) < len(br)
}

// expandImportFolders replaces every plain folder with the images directly
// inside it, keeping the caller's order for the rest. The second result names
// the folders that held nothing importable, so a batch can report them without
// swallowing the files that did work.
func expandImportFolders(paths []string) (expanded []string, empty []string) {
	for _, path := range paths {
		if !isPlainFolder(path) {
			expanded = append(expanded, path)
			continue
		}
		images, err := folderImagePaths(path)
		if err != nil || len(images) == 0 {
			empty = append(empty, path)
			continue
		}
		expanded = append(expanded, images...)
	}
	return expanded, empty
}
