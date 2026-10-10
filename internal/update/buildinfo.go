package update

import (
	"encoding/base64"
	"fmt"

	"crypto/ed25519"
)

// 这两个变量由发布流水线在构建时注入（-ldflags -X），源码里是空的 = 未配置：
//
//	-X compositor-win/internal/update.FeedURL=https://…/releases/latest/download/feed.json
//	-X compositor-win/internal/update.PublicKeyB64=<base64 的 32 字节 Ed25519 公钥>
//
// 公钥来自 scripts/genfeed 的输出（它打印 publicKey=<b64>）。未配置时"检查更新"
// 会明确告诉用户"这个构建没有配置更新源"，而不是静默失败。
var (
	FeedURL      = ""
	PublicKeyB64 = ""
)

// PublicKey 解码注入的 Ed25519 公钥；未配置或格式不对时返回错误。
func PublicKey() (ed25519.PublicKey, error) {
	if PublicKeyB64 == "" {
		return nil, fmt.Errorf("这个构建没有配置更新公钥")
	}
	raw, err := base64.StdEncoding.DecodeString(PublicKeyB64)
	if err != nil {
		return nil, fmt.Errorf("更新公钥不是合法的 base64: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("更新公钥长度应为 %d 字节，得到 %d", ed25519.PublicKeySize, len(raw))
	}
	return ed25519.PublicKey(raw), nil
}

// Configured 报告这个构建是否配了更新源（feed + 公钥）。
func Configured() bool {
	if FeedURL == "" || PublicKeyB64 == "" {
		return false
	}
	_, err := PublicKey()
	return err == nil
}
