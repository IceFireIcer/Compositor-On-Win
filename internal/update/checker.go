package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"compositor-win/internal/fsatomic"
)

// ErrNotConfigured 表示这个构建没有更新源（没有注入 FeedURL/公钥）。
var ErrNotConfigured = errors.New("这个构建没有配置更新源")

// ErrUpToDate 表示已经是最新版本。
var ErrUpToDate = errors.New("已经是最新版本")

// MaxInstallerBytes 是安装器允许的最大字节数（防 feed 指到一个巨大的文件）。
//
// 上限也是内存上限：Ed25519 不是流式签名，验签必须拿到完整消息，所以校验签名时会有
// 一份与安装器等大的内存驻留（实测安装器 ~18MB；即使将来随包内置 ML 模型也远低于此）。
// feed 声明的尺寸在下载前就检查，超限会立刻失败而不是先下完再说。
const MaxInstallerBytes = 256 << 20

// MaxFeedBytes 是 feed 本身的大小上限（几 KB 的东西，给足余量防止被喂大文件）。
const MaxFeedBytes = 1 << 20

// Checker 是"检查更新"的执行者。零值不可用：至少要有 FeedURL、CurrentVersion 与
// 公钥。HTTP 为 nil 时用带超时的默认客户端。
type Checker struct {
	FeedURL        string
	CurrentVersion string
	PublicKey      ed25519.PublicKey
	HTTP           *http.Client
	// UserAgent 出现在请求里，便于 release 下载统计区分来源。
	UserAgent string
	// Progress 在下载中被调用（done, total，total 是 feed 声明的大小）。会被限流
	// 到大约每 100ms 一次，免得几十 MB 的包刷爆事件通道。
	Progress func(done, total int64)
}

// progressWriter 把写入字节数转成进度回调（限流）。
type progressWriter struct {
	w      io.Writer
	total  int64
	done   int64
	report func(done, total int64)
	last   time.Time
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	p.done += int64(n)
	if p.report != nil && (time.Since(p.last) >= 100*time.Millisecond || p.done >= p.total) {
		p.last = time.Now()
		p.report(p.done, p.total)
	}
	return n, err
}

// NewChecker 用构建注入的配置造一个 Checker（未配置时返回 ErrNotConfigured）。
func NewChecker(currentVersion string) (*Checker, error) {
	key, err := PublicKey()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotConfigured, err)
	}
	if strings.TrimSpace(FeedURL) == "" {
		return nil, fmt.Errorf("%w: 没有 feed 地址", ErrNotConfigured)
	}
	return &Checker{
		FeedURL:        FeedURL,
		CurrentVersion: currentVersion,
		PublicKey:      key,
		UserAgent:      "Compositor/" + currentVersion,
	}, nil
}

// HTTPClient 是所有 Checker 的默认 HTTP 客户端（nil = 用内置的 20s 超时客户端）。
// 生产留空：Go 的标准传输已经认 HTTP_PROXY/HTTPS_PROXY 环境变量。这个变量是给测试
// 注入"信任自签证书的客户端"用的（测试服务器走 httptest.NewTLSServer，规则与生产
// 一致：只接受 https）。
var HTTPClient *http.Client

func (c *Checker) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	if HTTPClient != nil {
		return HTTPClient
	}
	return &http.Client{Timeout: 20 * time.Second}
}

// Available 拉取 feed 并比较版本：有新版本返回它；已是最新返回 ErrUpToDate。
func (c *Checker) Available(ctx context.Context) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.FeedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("更新 feed 请求无法构造: %w", err)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("无法访问更新源: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("更新源返回 %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxFeedBytes))
	if err != nil {
		return nil, fmt.Errorf("读取更新 feed 失败: %w", err)
	}
	feed, err := ParseFeed(data)
	if err != nil {
		return nil, err
	}
	if !NewerThan(feed.Latest.Version, c.CurrentVersion) {
		return nil, ErrUpToDate
	}
	release := feed.Latest
	return &release, nil
}

// Download 下载安装器到 dir（dir 为空则用系统临时目录），逐字节校验 sha256 与
// Ed25519 签名，返回落盘路径。任何校验不过都会删掉临时文件并报错 —— 只有签名
// 对得上的安装器才会被交出去。
func (c *Checker) Download(ctx context.Context, rel Release, dir string) (string, error) {
	if len(c.PublicKey) != ed25519.PublicKeySize {
		return "", fmt.Errorf("没有可用的更新公钥")
	}
	if rel.Installer.Size > MaxInstallerBytes {
		return "", fmt.Errorf("安装器 %d 字节超过上限 %d", rel.Installer.Size, MaxInstallerBytes)
	}
	want, err := hex.DecodeString(rel.Installer.SHA256)
	if err != nil || len(want) != sha256.Size {
		return "", fmt.Errorf("feed 里的 sha256 无效")
	}
	sig, err := base64.StdEncoding.DecodeString(rel.Installer.EdSignature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return "", fmt.Errorf("feed 里的 Ed25519 签名无效")
	}
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("无法创建下载目录: %w", err)
	}
	dest := filepath.Join(dir, fmt.Sprintf("Compositor-%s-setup.exe", sanitizeVersion(rel.Version)))
	// 先写一个本次调用独有的 .part 文件，全部校验通过后再原子改名成最终名字：
	// 两次下载撞在一起（或上一次被杀掉）都不会截断/污染已经校验好的安装包。
	partial, err := os.CreateTemp(dir, "Compositor-*.part")
	if err != nil {
		return "", fmt.Errorf("无法创建下载临时文件: %w", err)
	}
	partialName := partial.Name()
	defer func() {
		if partialName != "" {
			_ = os.Remove(partialName)
		}
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.Installer.URL, nil)
	if err != nil {
		return "", fmt.Errorf("安装器请求无法构造: %w", err)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("无法下载安装器: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载安装器返回 %s", resp.Status)
	}

	file := partial
	hasher := sha256.New()
	sink := &progressWriter{w: io.MultiWriter(file, hasher), total: rel.Installer.Size, report: c.Progress}
	// 上限 +1 字节：正好读到上限说明文件比声明的还大，也拒绝。
	written, copyErr := io.Copy(sink, io.LimitReader(resp.Body, MaxInstallerBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return "", fmt.Errorf("下载中断: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("写入安装器失败: %w", closeErr)
	}
	if written > MaxInstallerBytes {
		return "", fmt.Errorf("安装器超过 %d 字节上限", MaxInstallerBytes)
	}
	if written != rel.Installer.Size {
		return "", fmt.Errorf("安装器大小不符：feed 说 %d 字节，下载到 %d", rel.Installer.Size, written)
	}
	if got := hasher.Sum(nil); !equalBytes(got, want) {
		return "", fmt.Errorf("安装器 sha256 不符（下载可能被篡改）")
	}
	body, err := os.ReadFile(partialName)
	if err != nil {
		return "", fmt.Errorf("无法读回安装器校验签名: %w", err)
	}
	if !ed25519.Verify(c.PublicKey, body, sig) {
		return "", fmt.Errorf("安装器 Ed25519 签名校验失败（下载可能被篡改）")
	}
	// 全部通过才把它变成正式文件：一次 rename 覆盖（Windows 的 os.Rename 就是
	// MoveFileEx + MOVEFILE_REPLACE_EXISTING，"先删再改名"反而会在改名失败时把上一份
	// 已校验的安装包删掉）。重试与临时名策略都在 internal/fsatomic 里。
	if err := fsatomic.Replace(partialName, dest); err != nil {
		return "", fmt.Errorf("无法提交安装包: %w", err)
	}
	partialName = "" // 已改名，defer 不要再删
	return dest, nil
}

// sanitizeVersion 把版本号里不适合做文件名的字符换掉。
func sanitizeVersion(v string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, strings.TrimSpace(v))
	if cleaned == "" {
		return "unknown"
	}
	return cleaned
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
