// Package update 实现应用内"检查更新"（票 50）：从 GitHub Releases 上的
// feed.json（appcast 的等价物）读最新版本，下载**Ed25519 签名**的安装器，校验
// sha256 与签名后交给 NSIS 静默安装。
//
// 为什么自写而不是用 go-selfupdate（docs/dependencies.md 原本的计划）：
//  1. 那个库的模型是"替换正在运行的二进制"，而票面要的是"全量安装器"——Windows
//     上由签名的 NSIS 安装器负责升级更符合系统习惯（快捷方式、关联、卸载项都在
//     安装器里）；
//  2. Ed25519 验证 Go 标准库就有（crypto/ed25519），于是**零新增依赖**，符合
//     ADR-0006 的精神。
//
// 详见 docs/tickets/50-installer-autoupdate.md。
package update

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// AppID 是 feed 里的应用标识：拒绝别的应用的 feed。
const AppID = "compositor"

// Feed 是 appcast 的等价物（scripts/genfeed 生成，GitHub Releases 托管）。
type Feed struct {
	App     string  `json:"app"`
	Channel string  `json:"channel"`
	Latest  Release `json:"latest"`
}

// Release 是一次发行。
type Release struct {
	Version     string    `json:"version"`
	PublishedAt string    `json:"publishedAt"`
	Notes       string    `json:"notes"`
	Installer   Installer `json:"installer"`
}

// Installer 是签过名的安装器。
type Installer struct {
	URL string `json:"url"`
	// Size 是安装器字节数（下载时同时用来做上限检查与完整性提示）。
	Size int64 `json:"size"`
	// SHA256 是安装器的十六进制摘要。
	SHA256 string `json:"sha256"`
	// EdSignature 是 base64(Ed25519 私钥对安装器完整字节的签名)。
	EdSignature string `json:"edSignature"`
}

// ParseFeed 解析并校验 feed。校验的是"能不能安全地拿去下载"：应用标识、版本、
// https 地址、尺寸、摘要与签名格式 —— 具体内容（摘要/签名对不对）在 Download 里
// 按字节校验。
func ParseFeed(data []byte) (*Feed, error) {
	var feed Feed
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&feed); err != nil {
		return nil, fmt.Errorf("更新 feed 无法解析: %w", err)
	}
	if feed.App != AppID {
		return nil, fmt.Errorf("更新 feed 属于别的应用（%q）", feed.App)
	}
	rel := feed.Latest
	if strings.TrimSpace(rel.Version) == "" {
		return nil, fmt.Errorf("更新 feed 缺少版本号")
	}
	// 只接受 https：生成器（scripts/genfeed）也只允许 https，两边规则必须一致，
	// 否则就白送一个"明文通道 + 跟随重定向"的降级面。
	parsed, err := url.Parse(rel.Installer.URL)
	if err != nil || parsed.Host == "" || parsed.Scheme != "https" {
		return nil, fmt.Errorf("更新 feed 的安装器地址无效（必须 https）: %q", rel.Installer.URL)
	}
	if rel.Installer.Size <= 0 {
		return nil, fmt.Errorf("更新 feed 缺少安装器大小")
	}
	if len(rel.Installer.SHA256) != 64 || !isHex(rel.Installer.SHA256) {
		return nil, fmt.Errorf("更新 feed 的 sha256 不是 64 位十六进制")
	}
	if strings.TrimSpace(rel.Installer.EdSignature) == "" {
		return nil, fmt.Errorf("更新 feed 缺少 Ed25519 签名")
	}
	return &feed, nil
}

func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
