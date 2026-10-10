package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallScriptWaitsAndInstallsSilently(t *testing.T) {
	script := installScript("C:\\Program Files\\Compositor\\setup.exe")
	if !strings.Contains(script, "\"C:\\Program Files\\Compositor\\setup.exe\" /S") {
		t.Fatalf("安装器路径要带引号并静默安装:\n%s", script)
	}
	if !strings.Contains(script, "timeout /t 3") {
		t.Fatalf("要先等应用退出:\n%s", script)
	}
	if !strings.HasPrefix(script, "@echo off") {
		t.Fatalf("要静默执行:\n%s", script)
	}
	if !strings.Contains(script, "\r\n") {
		t.Fatal("批处理要用 CRLF 行尾")
	}
}

func TestLaunchInstallerRefusesMissingOrEmptyPaths(t *testing.T) {
	if err := LaunchInstaller(""); err == nil || !strings.Contains(err.Error(), "更新包") {
		t.Fatalf("空路径应报错，得到 %v", err)
	}
	if err := LaunchInstaller(filepath.Join(t.TempDir(), "没有这个.exe")); err == nil {
		t.Fatal("不存在的安装器应报错")
	}
	if err := LaunchInstaller(t.TempDir()); err == nil {
		t.Fatal("目录应被拒绝")
	}
}

func TestLaunchInstallerWritesTheScriptAndStartsDetached(t *testing.T) {
	installer := filepath.Join(t.TempDir(), "Compositor-0.2.0-setup.exe")
	if err := os.WriteFile(installer, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	saved := launchDetached
	var started string
	var content string
	launchDetached = func(scriptPath string) error {
		started = scriptPath
		data, err := os.ReadFile(scriptPath)
		if err != nil {
			return err
		}
		content = string(data)
		return nil
	}
	t.Cleanup(func() { launchDetached = saved })

	if err := LaunchInstaller(installer); err != nil {
		t.Fatalf("启动更新脚本失败: %v", err)
	}
	if started == "" || !strings.HasSuffix(started, "apply-update.bat") {
		t.Fatalf("应启动一个 apply-update.bat，得到 %q", started)
	}
	if !strings.Contains(content, installer) || !strings.Contains(content, "/S") {
		t.Fatalf("脚本应安装这个包:\n%s", content)
	}
	// 脱离启动失败要如实报错。
	launchDetached = func(string) error { return os.ErrPermission }
	if err := LaunchInstaller(installer); err == nil {
		t.Fatal("启动失败应报错")
	}
}
