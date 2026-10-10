package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"compositor-win/internal/update"
)

// writeInstaller 造一个假安装器（内容是任意字节，签名只看字节）。
func writeInstaller(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Compositor-0.2.0-setup.exe")
	if err := os.WriteFile(path, []byte("MZ fake installer payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func testKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func baseInput(t *testing.T) buildInput {
	t.Helper()
	return buildInput{
		InstallerPath: writeInstaller(t),
		Version:       "v0.2.0",
		URL:           "https://github.com/o/r/releases/download/v0.2.0/Compositor-0.2.0-setup.exe",
		Notes:         "修了几个 bug",
		Channel:       "stable",
		Key:           testKey(t),
		Now:           time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC),
	}
}

func TestBuildFeedMatchesTheAppParserAndVerifies(t *testing.T) {
	in := baseInput(t) // 一次输入一个密钥一份字节：别重复调用（每次都是新的随机密钥）
	feed, err := buildFeed(in)
	if err != nil {
		t.Fatalf("生成 feed 失败: %v", err)
	}
	if feed.App != update.AppID || feed.Latest.Version != "0.2.0" {
		t.Fatalf("应用/版本 = %q/%q（版本要去掉 v 前缀）", feed.App, feed.Latest.Version)
	}
	data, err := json.Marshal(feed)
	if err != nil {
		t.Fatal(err)
	}
	// 应用侧的解析器必须能读它 —— 这是脚本与应用之间唯一的契约检查点。
	parsed, err := update.ParseFeed(data)
	if err != nil {
		t.Fatalf("应用侧解析器拒绝了生成的 feed: %v", err)
	}
	payload, err := os.ReadFile(in.InstallerPath)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := base64.StdEncoding.DecodeString(parsed.Latest.Installer.EdSignature)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(in.Key.Public().(ed25519.PublicKey), payload, sig) {
		t.Fatal("签名验不过")
	}
	if parsed.Latest.Installer.Size != int64(len(payload)) {
		t.Fatalf("大小 = %d，想要 %d", parsed.Latest.Installer.Size, len(payload))
	}
	if parsed.Latest.PublishedAt != "2026-10-10T12:00:00Z" {
		t.Fatalf("发布时间 = %q", parsed.Latest.PublishedAt)
	}
}

// 同一份输入（同密钥、同字节、同一时间）必须给出**逐字节相同**的落盘文件 ——
// 这才是"发布可复现"：重放一次发布要能得到同一个 feed（同一份签名）。
// 比较的是写出来的文件字节，而不是内存里同一个结构体 marshal 两次。
func TestBuildFeedIsReproducible(t *testing.T) {
	in := baseInput(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "feed-a.json")
	second := filepath.Join(dir, "feed-b.json")
	for _, path := range []string{first, second} {
		feed, err := buildFeed(in)
		if err != nil {
			t.Fatalf("生成 feed 失败: %v", err)
		}
		if err := writeFeed(feed, path); err != nil {
			t.Fatal(err)
		}
	}
	a, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("同样的输入应给出逐字节相同的 feed:\n%s\n%s", a, b)
	}
	if !strings.Contains(string(a), "2026-10-10T12:00:00Z") {
		t.Fatalf("publishedAt 应固定成输入里的时间:\n%s", a)
	}

	// 时间不同 → 输出不同（否则上面那条断言可能因为"根本没写时间"而假通过）。
	in.Now = in.Now.Add(time.Hour)
	third := filepath.Join(dir, "feed-c.json")
	feed, err := buildFeed(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFeed(feed, third); err != nil {
		t.Fatal(err)
	}
	c, err := os.ReadFile(third)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) == string(c) {
		t.Fatal("publishedAt 不同时 feed 应该不同")
	}
}

func TestParsePublishedAt(t *testing.T) {
	got, err := parsePublishedAt("2026-10-10T12:00:00+08:00")
	if err != nil {
		t.Fatal(err)
	}
	if got.UTC().Format(time.RFC3339) != "2026-10-10T04:00:00Z" {
		t.Fatalf("应换算成 UTC: %s", got.UTC().Format(time.RFC3339))
	}
	if _, err := parsePublishedAt("昨天"); err == nil {
		t.Fatal("坏时间应报错")
	}
	fresh, err := parsePublishedAt("  ")
	if err != nil || time.Since(fresh) > time.Minute {
		t.Fatalf("空值应取现在: %v / %v", fresh, err)
	}
}

func TestBuildFeedRejectsBadInputs(t *testing.T) {
	// 非 https。
	in := baseInput(t)
	in.URL = "http://example.test/setup.exe"
	if _, err := buildFeed(in); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("http 地址应被拒绝，得到 %v", err)
	}

	// 安装器不存在。
	in = baseInput(t)
	in.InstallerPath = filepath.Join(t.TempDir(), "没有.exe")
	if _, err := buildFeed(in); err == nil {
		t.Fatal("缺安装器应报错")
	}

	// 空安装器。
	in = baseInput(t)
	if err := os.WriteFile(in.InstallerPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := buildFeed(in); err == nil {
		t.Fatal("空安装器应报错")
	}
}

func TestReadKeyAcceptsSeedAndPrivateKey(t *testing.T) {
	priv := testKey(t)
	seed := priv.Seed()
	fromSeed, err := readKey(base64.StdEncoding.EncodeToString(seed))
	if err != nil {
		t.Fatalf("seed 应被接受: %v", err)
	}
	if !fromSeed.Equal(priv) {
		t.Fatal("从 seed 推出的私钥应与原私钥一致")
	}
	fromFull, err := readKey(base64.StdEncoding.EncodeToString(priv))
	if err != nil || !fromFull.Equal(priv) {
		t.Fatalf("完整私钥应被接受: %v", err)
	}

	for _, bad := range []string{"", "  ", "not-base64!!", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := readKey(bad); err == nil {
			t.Fatalf("%q 应被拒绝", bad)
		}
	}
}

func TestReadNotes(t *testing.T) {
	if notes, err := readNotes(""); err != nil || notes != "" {
		t.Fatalf("没有说明文件时应是空: %q / %v", notes, err)
	}
	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte("  ## 修复\n- 崩溃  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	notes, err := readNotes(path)
	if err != nil {
		t.Fatal(err)
	}
	if notes != "## 修复\n- 崩溃" {
		t.Fatalf("说明应去掉首尾空白: %q", notes)
	}
	if _, err := readNotes(filepath.Join(t.TempDir(), "没有.md")); err == nil {
		t.Fatal("缺文件应报错")
	}
}
