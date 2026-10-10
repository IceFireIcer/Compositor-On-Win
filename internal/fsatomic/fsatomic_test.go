package fsatomic

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWriteFileCreatesAndReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")

	if err := WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "first" {
		t.Fatalf("内容 = %q / %v", got, err)
	}

	if err := WriteFile(path, []byte("second"), 0o644); err != nil {
		t.Fatalf("覆盖写入失败: %v（Windows 上 rename 覆盖是允许的，不该需要先删）", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "second" {
		t.Fatalf("覆盖后内容 = %q，想要 second", got)
	}

	assertNoTempLeftovers(t, filepath.Dir(path))
}

// assertNoTempLeftovers 断言目录里没有残留的临时文件（临时名每次唯一，所以按后缀扫）。
func assertNoTempLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("不该留下临时文件: %s", entry.Name())
		}
	}
}

// 多个写者同时提交同一个文件：内容永远是完整的一版（不能互为半成品），要么成功要么
// 报出可见错误 —— 固定临时名时其中一次提交会静默丢失（本机实测：rename file-not-found）。
func TestWriteFileConcurrentWritersDoNotCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	payloads := [][]byte{bytes.Repeat([]byte("A"), 4096), bytes.Repeat([]byte("B"), 4096)}

	const writers = 8
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = WriteFile(path, payloads[i%len(payloads)], 0o644)
		}(i)
	}
	wg.Wait()

	succeeded := 0
	for i, err := range errs {
		if err == nil {
			succeeded++
			continue
		}
		// 失败必须是"看得见的错误"，不是静默丢一次提交。
		t.Logf("写者 %d 报了错（并发竞争下允许，但要可见）: %v", i, err)
	}
	if succeeded == 0 {
		t.Fatal("所有写者都失败了：并发写不该全灭")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payloads[0]) && !bytes.Equal(got, payloads[1]) {
		t.Fatalf("最终内容既不是 A 也不是 B（长度 %d）——写者互相污染了", len(got))
	}
	assertNoTempLeftovers(t, dir)
}

// 目标被本进程打开着：Windows 上 rename 会失败（Access is denied），本包重试后报错，
// 但**旧内容必须还在**（这正是"先删再改名"会丢数据而本包不会的原因）。
func TestWriteFileKeepsTheOldContentWhenTheTargetIsLocked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	handle, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()

	start := time.Now()
	err = WriteFile(path, []byte("new"), 0o644)
	elapsed := time.Since(start)

	if runtime.GOOS == "windows" {
		if err == nil {
			t.Fatal("Windows 上目标被打开时应失败（本机实测行为）")
		}
		if !strings.Contains(err.Error(), "重试") {
			t.Fatalf("错误应说明重试过：%v", err)
		}
		if elapsed < baseBackoff {
			t.Fatalf("应该真的重试过（耗时 %v）", elapsed)
		}
	} else {
		// POSIX 允许替换被打开的文件。
		if err != nil {
			t.Fatalf("POSIX 上应成功: %v", err)
		}
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "old"
	if runtime.GOOS != "windows" {
		want = "new"
	}
	if string(got) != want {
		t.Fatalf("目标内容 = %q，想要 %q（旧内容不能被毁）", got, want)
	}
	assertNoTempLeftovers(t, filepath.Dir(path))
}

// 重试次数：注入会失败前 N 次的 rename，确认本包确实重试了 maxAttempts 次。
func TestWriteFileRetriesBeforeGivingUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	saved := renameFn
	attempts := 0
	renameFn = func(string, string) error {
		attempts++
		return os.ErrPermission
	}
	t.Cleanup(func() { renameFn = saved })

	if err := WriteFile(path, []byte("new"), 0o644); err == nil {
		t.Fatal("一直失败时应报错")
	}
	if attempts != maxAttempts {
		t.Fatalf("尝试次数 = %d，想要 %d", attempts, maxAttempts)
	}
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Fatalf("失败后旧内容应原样: %q", got)
	}
}
