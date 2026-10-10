package update

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// installScript 生成"等应用退出后静默安装"的批处理内容。
//
// NSIS 覆盖正在运行的 Compositor.exe 会失败，所以升级不能由应用自己直接启动安装器：
// 让一个脱离的 cmd 脚本先等几秒（应用这时已经退出），再 /S 静默安装。Sparkle 在
// macOS 上是退出后替换 .app，Windows 上的等价物就是这个等待脚本。
//
// 安装器路径带引号：用户可能把安装包放在带空格的目录里。
func installScript(installerPath string) string {
	return strings.Join([]string{
		"@echo off",
		"rem 等 Compositor 退出：NSIS 覆盖正在运行的 exe 会失败",
		"timeout /t 3 /nobreak >nul",
		"\"" + installerPath + "\" /S",
		"",
	}, "\r\n")
}

// launchDetached 是真正把脚本拖起来的实现（按平台分文件）。测试替换它。
var launchDetached = launchDetachedImpl

// LaunchInstaller 把等待脚本写到临时目录并**脱离**启动它：函数立刻返回，调用方
// 随后退出应用（Service.ApplyUpdate 里 runtime.Quit）。
func LaunchInstaller(installerPath string) error {
	if strings.TrimSpace(installerPath) == "" {
		return fmt.Errorf("没有可安装的更新包")
	}
	info, err := os.Stat(installerPath)
	if err != nil {
		return fmt.Errorf("更新包不存在: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("更新包是一个目录: %s", installerPath)
	}
	dir, err := os.MkdirTemp("", "compositor-update")
	if err != nil {
		return fmt.Errorf("无法创建更新临时目录: %w", err)
	}
	script := filepath.Join(dir, "apply-update.bat")
	if err := os.WriteFile(script, []byte(installScript(installerPath)), 0o644); err != nil {
		return fmt.Errorf("无法写入更新脚本: %w", err)
	}
	if err := launchDetached(script); err != nil {
		return fmt.Errorf("无法启动更新脚本: %w", err)
	}
	return nil
}
