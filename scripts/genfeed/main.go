// genfeed 生成更新 feed（appcast 的等价物）并给安装器签 Ed25519 签名。
//
//	go run ./scripts/genfeed -installer build/bin/Compositor-0.2.0-setup.exe \
//	  -version 0.2.0 -url https://github.com/<owner>/<repo>/releases/download/v0.2.0/Compositor-0.2.0-setup.exe \
//	  -notes-file CHANGELOG-0.2.0.md -out releases/feed.json
//
// 私钥从环境变量 COMPOSITOR_UPDATE_KEY 读（base64 的 32 字节 Ed25519 seed，绝不入库）；
// 生成后会用自己的解析器（internal/update.ParseFeed + 验签）自检一遍，保证脚本与
// 应用对 feed 的理解一致。stdout 打印 publicKey=<b64>，供构建时注入：
//
//	-ldflags "-X compositor-win/internal/update.FeedURL=<url> -X compositor-win/internal/update.PublicKeyB64=<b64>"
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"compositor-win/internal/update"
)

func main() {
	installer := flag.String("installer", "", "安装器路径（要签名的那个 exe）")
	version := flag.String("version", "", "版本号，如 0.2.0")
	url := flag.String("url", "", "安装器的下载地址（https）")
	notesFile := flag.String("notes-file", "", "发行说明文件（可选，纯文本/markdown）")
	out := flag.String("out", "", "输出 feed.json 的路径")
	keyEnv := flag.String("key-env", "COMPOSITOR_UPDATE_KEY", "私钥所在的环境变量名")
	channel := flag.String("channel", "stable", "更新通道")
	// publishedAt 默认是"现在"，所以同一份输入跨秒重跑会得到不同的 feed。要逐字节
	// 可复现（重放一次发布、核对签名）就显式给这个时间。
	publishedAt := flag.String("published-at", "", "发布时间（RFC3339，默认现在）")
	printPublicKey := flag.Bool("print-public-key", false, "只打印由 COMPOSITOR_UPDATE_KEY 推出的 publicKey=<b64> 然后退出（构建时注入用）")
	flag.Parse()

	if *printPublicKey {
		seed, err := readKey(os.Getenv(*keyEnv))
		if err != nil {
			fatal(fmt.Errorf("读取私钥（环境变量 %s）失败: %w", *keyEnv, err))
		}
		fmt.Printf("publicKey=%s\n", base64.StdEncoding.EncodeToString(seed.Public().(ed25519.PublicKey)))
		return
	}

	published, err := parsePublishedAt(*publishedAt)
	if err != nil {
		fatal(err)
	}

	if *installer == "" || *version == "" || *url == "" || *out == "" {
		fatal(errors.New("-installer / -version / -url / -out 都是必填"))
	}
	keySeed, err := readKey(os.Getenv(*keyEnv))
	if err != nil {
		fatal(fmt.Errorf("读取私钥（环境变量 %s）失败: %w", *keyEnv, err))
	}
	notes, err := readNotes(*notesFile)
	if err != nil {
		fatal(err)
	}
	feed, err := buildFeed(buildInput{
		InstallerPath: *installer,
		Version:       *version,
		URL:           *url,
		Notes:         notes,
		Channel:       *channel,
		Key:           keySeed,
		Now:           published,
	})
	if err != nil {
		fatal(err)
	}
	if err := writeFeed(feed, *out); err != nil {
		fatal(err)
	}
	public := base64.StdEncoding.EncodeToString(keySeed.Public().(ed25519.PublicKey))
	fmt.Printf("feed=%s\n", *out)
	fmt.Printf("version=%s\n", feed.Latest.Version)
	fmt.Printf("publicKey=%s\n", public)
}

// buildInput 是 buildFeed 的输入（测试直接调它，不必走命令行）。
type buildInput struct {
	InstallerPath string
	Version       string
	URL           string
	Notes         string
	Channel       string
	Key           ed25519.PrivateKey
	Now           time.Time
}

// buildFeed 读安装器、算摘要、签名，并用 update.ParseFeed 自检输出。
func buildFeed(in buildInput) (*update.Feed, error) {
	payload, err := os.ReadFile(in.InstallerPath)
	if err != nil {
		return nil, fmt.Errorf("读取安装器失败: %w", err)
	}
	if len(payload) == 0 {
		return nil, errors.New("安装器是空文件")
	}
	if !strings.HasPrefix(in.URL, "https://") {
		return nil, fmt.Errorf("下载地址必须是 https: %q", in.URL)
	}
	sum := sha256.Sum256(payload)
	feed := &update.Feed{
		App:     update.AppID,
		Channel: in.Channel,
		Latest: update.Release{
			Version:     strings.TrimPrefix(strings.TrimSpace(in.Version), "v"),
			PublishedAt: in.Now.UTC().Format(time.RFC3339),
			Notes:       in.Notes,
			Installer: update.Installer{
				URL:         in.URL,
				Size:        int64(len(payload)),
				SHA256:      hex.EncodeToString(sum[:]),
				EdSignature: base64.StdEncoding.EncodeToString(ed25519.Sign(in.Key, payload)),
			},
		},
	}
	// 自检：解析器与应用用同一份规则；签名也要能验回来。
	data, err := json.Marshal(feed)
	if err != nil {
		return nil, fmt.Errorf("无法编码 feed: %w", err)
	}
	parsed, err := update.ParseFeed(data)
	if err != nil {
		return nil, fmt.Errorf("生成的 feed 没能通过应用侧解析器: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(parsed.Latest.Installer.EdSignature)
	if err != nil || !ed25519.Verify(in.Key.Public().(ed25519.PublicKey), payload, sig) {
		return nil, errors.New("生成的签名没能通过验证")
	}
	return feed, nil
}

// parsePublishedAt 解析 -published-at（空 = 现在）。
func parsePublishedAt(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Now().UTC(), nil
	}
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, fmt.Errorf("-published-at 不是 RFC3339 时间: %w", err)
	}
	return parsed.UTC(), nil
}

// writeFeed 落盘（缩进 2 空格 + 末尾换行）：main 与"可复现"测试走同一条路。
func writeFeed(feed *update.Feed, path string) error {
	data, err := json.MarshalIndent(feed, "", "  ")
	if err != nil {
		return fmt.Errorf("无法编码 feed: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("无法写入 %s: %w", path, err)
	}
	return nil
}

// readKey 解 base64 的 32 字节种子（或 64 字节私钥）。
func readKey(encoded string) (ed25519.PrivateKey, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, errors.New("为空（发布私钥不进仓库，请从 CI secret 或本地密钥环注入）")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("不是合法的 base64: %w", err)
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(raw), nil
	default:
		return nil, fmt.Errorf("长度应为 %d（seed）或 %d（私钥）字节，得到 %d",
			ed25519.SeedSize, ed25519.PrivateKeySize, len(raw))
	}
}

func readNotes(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取发行说明失败: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "genfeed: %v\n", err)
	os.Exit(1)
}
