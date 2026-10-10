package main

// 外壳递来的参数整理（票 49）：双击 .comp 的关联调用给绝对路径，命令行可能给相对
// 路径，启动开关（- 开头）不是文件。注意 Wails 的 SecondInstanceData.WorkingDirectory
// 是可执行文件目录、不是第二实例的工作目录，所以相对路径按本进程的 CWD 补全。

import (
	"path/filepath"
	"testing"
)

func TestResolveLaunchedPathsMakesRelativePathsAbsolute(t *testing.T) {
	got := resolveLaunchedPaths([]string{
		"relative.comp",
		filepath.Join(t.TempDir(), "absolute.comp"),
		"--flag",
		"",
	})
	if len(got) != 2 {
		t.Fatalf("路径 = %v，想要两条（相对 + 绝对，开关与空串丢掉）", got)
	}
	wantAbs, err := filepath.Abs("relative.comp")
	if err != nil {
		t.Fatal(err)
	}
	if got[0] != wantAbs {
		t.Fatalf("相对路径应补成 %q，得到 %q", wantAbs, got[0])
	}
	if !filepath.IsAbs(got[1]) {
		t.Fatalf("绝对路径应原样保留: %v", got[1])
	}
}

func TestResolveLaunchedPathsDropsSwitchesAndEmpty(t *testing.T) {
	if got := resolveLaunchedPaths([]string{"-nsis", "--dev", ""}); len(got) != 0 {
		t.Fatalf("开关与空串都该丢掉: %v", got)
	}
	if got := resolveLaunchedPaths(nil); len(got) != 0 {
		t.Fatalf("没有参数时应是空: %v", got)
	}
}
