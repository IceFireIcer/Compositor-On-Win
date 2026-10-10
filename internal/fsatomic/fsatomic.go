// Package fsatomic 提供 Windows 上可靠的原子文件替换。
//
// 为什么不是简单的一句 os.Rename：Windows 的替换语义与 POSIX 不同，而且"先删旧文件
// 再改名"这种常见写法会**丢数据**（删成功、改名失败 → 旧内容没了）。本机实测的事实
// （Go 1.26 / NTFS，见 fsatomic_test.go 里对应的断言）：
//
//   - 目标存在且**没有被任何进程打开**时，os.Rename(tmp, target)（内部是 MoveFileEx +
//     MOVEFILE_REPLACE_EXISTING）会**直接覆盖成功** —— 不需要先 Remove。
//   - 目标被任何进程打开着（哪怕只是 os.Open 只读、且是本进程），rename 与 remove 都会
//     失败：rename → "Access is denied"，remove → "being used by another process"。
//     应用自己只在读取的瞬间持有文件，但杀毒软件、索引器、OneDrive/云同步会在写入期间
//     短暂持有，所以写入要带一点重试。
//
// 于是本包只做一件事：同目录临时文件 + 一次 rename（必要时短暂重试）。失败时旧文件
// 原样保留，绝不会出现"两个都不是"的中间态。
package fsatomic

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// renameFn 是 os.Rename 的接缝：测试用它统计尝试次数。
var renameFn = os.Rename

// 重试策略：应用与外壳文件都不该长时间被锁，所以给一点缓冲就够了（最坏 ~175ms）。
const (
	maxAttempts = 4
	baseBackoff = 25 * time.Millisecond
)

// WriteFile 把 data 原子地写到 path：先写一个**本次调用独有**的同目录临时文件，再
// rename 覆盖 path。目录必须已存在（与 os.WriteFile 不同，本包不建目录 —— 调用方知道
// 自己的目录结构）。
//
// 临时名必须唯一：固定叫 "<path>.tmp" 时两个写者会互相截断（A 写完 tmp，B 又覆盖同一个
// tmp，A 先改名成功，B 改名时 tmp 已经不在了 → file-not-found，B 的那次提交就这么丢了）。
// 并发写同一个文件因此是"各自成功、最后一个是赢家、内容永远是完整的某一版"。
//
// 唯一的代价：进程崩在 rename 之前会留下一个 "<名字>.<随机>.tmp"，而固定名时这种残留会被
// 下一次写入覆盖掉。残留是可辨识、可清理的（都以 .tmp 结尾、都在同一个目录），比"悄悄把别的
// 写者的暂存文件截断"划算得多。
func WriteFile(path string, data []byte, perm os.FileMode) error {
	handle, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件失败（%s 所在目录）: %w", path, err)
	}
	tmp := handle.Name()
	if _, err := handle.Write(data); err != nil {
		handle.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("写临时文件 %s 失败: %w", tmp, err)
	}
	if err := handle.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("写临时文件 %s 失败: %w", tmp, err)
	}
	// CreateTemp 给的是 0600，按调用方要求落权限。
	if err := os.Chmod(tmp, perm); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("设置临时文件权限失败: %w", err)
	}
	if err := Replace(tmp, path); err != nil {
		_ = os.Remove(tmp) // 别留下临时文件；旧文件仍在
		return err
	}
	return nil
}

// Replace 提交一个**已经写好的**同目录临时文件：一次 rename 覆盖 + 一段短重试。
// 给那些自己管暂存文件的调用方用（代理契约要求清单先写成 <包>/.manifest.json.tmp，
// 更新器下载安装器时也自己落一个临时文件）。
//
// 重试只针对"文件被短暂占用"这类错误；重试耗尽时旧文件原样保留，调用方负责清理 from。
func Replace(from, to string) error {
	var renameErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		renameErr = renameFn(from, to)
		if renameErr == nil {
			return nil
		}
		if attempt < maxAttempts {
			time.Sleep(baseBackoff * time.Duration(1<<uint(attempt-1)))
		}
	}
	// 目标被别的进程打开时 Windows 给的是 "Access is denied"，提示里说清可能的去处。
	return fmt.Errorf("替换 %s 失败（文件可能正被杀毒/索引/云同步占用，已重试 %d 次）: %w",
		to, maxAttempts, renameErr)
}
