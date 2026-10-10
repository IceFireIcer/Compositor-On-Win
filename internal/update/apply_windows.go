//go:build windows

package update

import (
	"os/exec"
	"syscall"
)

// launchDetachedImpl 用 DETACHED_PROCESS 起脚本：父进程（编辑器）马上退出，脚本
// 继续跑并完成安装；隐藏窗口免得闪一个黑框。
func launchDetachedImpl(scriptPath string) error {
	cmd := exec.Command("cmd", "/c", scriptPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x00000008, // DETACHED_PROCESS
	}
	return cmd.Start()
}
