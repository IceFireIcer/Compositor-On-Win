//go:build !windows

package winchrome

// 非 Windows 上外壳三件事全是 no-op：编辑器在 Linux/macOS 上（CI 的 go test、
// 开发机）必须能编、能跑，且**一个副作用都不能有**。
//
// windows 侧的同名函数在 chrome_windows.go（cgo + COM）。

import "errors"

func noteRecent(path string) error {
	if path == "" {
		return errors.New("路径为空")
	}
	return nil
}

func clearRecent() error { return nil }

func setOverlay(on bool) error { return nil }
