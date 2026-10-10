//go:build !windows

package update

import "errors"

// launchDetachedImpl 只在 Windows 上有意义：其他平台（CI 的 go test）明确报错，
// 而不是假装启动成功。
func launchDetachedImpl(string) error {
	return errors.New("应用内更新只在 Windows 上支持")
}
