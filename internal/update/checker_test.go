package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// feedFixture 是一个 HTTPS 测试服务器（feed 走 https，规则与生产一致），同时托管
// feed 与安装器字节。feed/字节可以在测试中途换掉 —— 只有一个 server、一份 handler。
type feedFixture struct {
	server *httptest.Server

	mu      sync.Mutex
	feed    Feed
	payload []byte
	rawBody string // 非空时直接返回这段文本（模拟坏 feed）
	status  int
}

func newFeedFixture(t *testing.T) *feedFixture {
	t.Helper()
	f := &feedFixture{payload: []byte("installer-bytes"), status: http.StatusOK}
	f.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.status != http.StatusOK {
			http.Error(w, "nope", f.status)
			return
		}
		if f.rawBody != "" {
			_, _ = w.Write([]byte(f.rawBody))
			return
		}
		switch r.URL.Path {
		case "/feed.json":
			if err := writeJSON(w, f.feed); err != nil {
				t.Errorf("写 feed 失败: %v", err)
			}
		case "/setup.exe":
			_, _ = w.Write(f.payload)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

// checker 造一个指向本 fixture 的 Checker（HTTP 客户端信任测试证书）。
func (f *feedFixture) checker(current string, pub ed25519.PublicKey) *Checker {
	return &Checker{
		FeedURL:        f.server.URL + "/feed.json",
		CurrentVersion: current,
		PublicKey:      pub,
		HTTP:           f.server.Client(),
	}
}

// setRelease 用给定版本与私钥签一份安装器（payload 为 fixture 当前字节）。
func (f *feedFixture) setRelease(t *testing.T, version string, key ed25519.PrivateKey) ed25519.PublicKey {
	t.Helper()
	f.mu.Lock()
	payload := append([]byte(nil), f.payload...)
	f.mu.Unlock()
	sum := sha256.Sum256(payload)
	f.mu.Lock()
	f.feed = Feed{
		App:     AppID,
		Channel: "stable",
		Latest: Release{
			Version:     version,
			PublishedAt: "2026-10-10T12:00:00Z",
			Notes:       "修了几个 bug",
			Installer: Installer{
				URL:         f.server.URL + "/setup.exe",
				Size:        int64(len(payload)),
				SHA256:      hex.EncodeToString(sum[:]),
				EdSignature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, payload)),
			},
		},
	}
	f.mu.Unlock()
	return key.Public().(ed25519.PublicKey)
}

func (f *feedFixture) setPayload(payload []byte) {
	f.mu.Lock()
	f.payload = payload
	f.mu.Unlock()
}

func (f *feedFixture) setRaw(body string) {
	f.mu.Lock()
	f.rawBody = body
	f.mu.Unlock()
}

func (f *feedFixture) setStatus(status int) {
	f.mu.Lock()
	f.status = status
	f.mu.Unlock()
}

func (f *feedFixture) release() Release {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.feed.Latest
}

func writeJSON(w http.ResponseWriter, value any) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(value)
}

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func TestAvailableFindsANewerVersion(t *testing.T) {
	fixture := newFeedFixture(t)
	pub := fixture.setRelease(t, "0.2.0", newKey(t))

	rel, err := fixture.checker("0.1.0", pub).Available(context.Background())
	if err != nil {
		t.Fatalf("检查更新失败: %v", err)
	}
	if rel.Version != "0.2.0" || rel.Installer.URL != fixture.server.URL+"/setup.exe" {
		t.Fatalf("发行信息 = %+v", rel)
	}
	if rel.Notes == "" || rel.PublishedAt == "" {
		t.Fatalf("说明/发布时间应保留: %+v", rel)
	}
}

func TestAvailableReportsUpToDate(t *testing.T) {
	fixture := newFeedFixture(t)
	pub := fixture.setRelease(t, "0.2.0", newKey(t))

	for _, current := range []string{"0.2.0", "0.3.0", "v0.2.0"} {
		if _, err := fixture.checker(current, pub).Available(context.Background()); err != ErrUpToDate {
			t.Fatalf("当前 %s 应是最新，得到 %v", current, err)
		}
	}
}

func TestAvailableSurfacesUnreachableOrBrokenFeed(t *testing.T) {
	fixture := newFeedFixture(t)
	pub := fixture.setRelease(t, "0.2.0", newKey(t))
	checker := fixture.checker("0.1.0", pub)

	fixture.setStatus(http.StatusNotFound)
	if _, err := checker.Available(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "404") {
		t.Fatalf("404 应报错并带上状态码，得到 %v", err)
	}

	fixture.setStatus(http.StatusOK)
	fixture.setRaw("not json")
	if _, err := checker.Available(context.Background()); err == nil {
		t.Fatal("坏 feed 应报错")
	}
}

func TestDownloadVerifiesAndWritesTheInstaller(t *testing.T) {
	fixture := newFeedFixture(t)
	pub := fixture.setRelease(t, "0.2.0", newKey(t))

	dir := t.TempDir()
	path, err := fixture.checker("0.1.0", pub).Download(context.Background(), fixture.release(), dir)
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(path), "Compositor-0.2.0-setup") {
		t.Fatalf("落盘名字应带版本: %s", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "installer-bytes" {
		t.Fatalf("字节不符: %q", got)
	}
	// 成功路径也不该留下 .part 临时文件。
	assertNoPartials(t, dir)
}

func TestDownloadRejectsTamperedOrWronglySignedInstallers(t *testing.T) {
	fixture := newFeedFixture(t)
	key := newKey(t)
	pub := fixture.setRelease(t, "0.2.0", key)
	checker := fixture.checker("0.1.0", pub)
	dir := t.TempDir()

	// 内容被换、长度相同：sha256 兜住。
	fixture.setPayload([]byte("installer-byteZ"))
	if _, err := checker.Download(context.Background(), fixture.release(), dir); err == nil ||
		!strings.Contains(err.Error(), "sha256") {
		t.Fatalf("篡改内容应被 sha256 拒绝，得到 %v", err)
	}

	// sha256 对得上、签名是别人的私钥签的：Ed25519 兜住。
	fixture.setPayload([]byte("installer-bytes"))
	otherPub := fixture.setRelease(t, "0.2.0", newKey(t))
	_ = otherPub
	if _, err := checker.Download(context.Background(), fixture.release(), dir); err == nil ||
		!strings.Contains(err.Error(), "签名") {
		t.Fatalf("错签名应被拒绝，得到 %v", err)
	}

	// 大小与 feed 不符：直接拒绝。
	fixture.setRelease(t, "0.2.0", key)
	wrongSize := fixture.release()
	wrongSize.Installer.Size += 10
	if _, err := checker.Download(context.Background(), wrongSize, dir); err == nil ||
		!strings.Contains(err.Error(), "大小") {
		t.Fatalf("大小不符应被拒绝，得到 %v", err)
	}

	// 任何失败都不留文件（含 .part）。
	assertNoPartials(t, dir)
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("校验失败后不该留下文件: %v / %v", entries, err)
	}
}

// assertNoPartials 断言没有留下 .part 临时文件。
func assertNoPartials(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".part") {
			t.Fatalf("不该留下临时文件: %s", entry.Name())
		}
	}
}

func TestDownloadRejectsUnusableFeedFields(t *testing.T) {
	checker := &Checker{PublicKey: make(ed25519.PublicKey, ed25519.PublicKeySize)}
	rel := Release{Version: "9.9.9", Installer: Installer{Size: MaxInstallerBytes + 1, SHA256: strings.Repeat("ab", 32)}}
	if _, err := checker.Download(context.Background(), rel, t.TempDir()); err == nil ||
		!strings.Contains(err.Error(), "上限") {
		t.Fatalf("超大安装器应被拒绝，得到 %v", err)
	}
	rel.Installer.Size = 10
	rel.Installer.SHA256 = "zz"
	if _, err := checker.Download(context.Background(), rel, t.TempDir()); err == nil {
		t.Fatal("坏 sha256 应被拒绝")
	}
	rel.Installer.SHA256 = strings.Repeat("ab", 32)
	rel.Installer.EdSignature = "not-base64!!"
	if _, err := checker.Download(context.Background(), rel, t.TempDir()); err == nil {
		t.Fatal("坏签名应被拒绝")
	}
	// 没有公钥时直接拒绝（不该先下载再发现没法验签）。
	noKey := &Checker{}
	if _, err := noKey.Download(context.Background(), rel, t.TempDir()); err == nil {
		t.Fatal("没有公钥应被拒绝")
	}
}

func TestNewCheckerNeedsInjectedConfiguration(t *testing.T) {
	savedURL, savedKey := FeedURL, PublicKeyB64
	t.Cleanup(func() { FeedURL, PublicKeyB64 = savedURL, savedKey })

	FeedURL, PublicKeyB64 = "", ""
	if _, err := NewChecker("0.1.0"); err == nil || !strings.Contains(err.Error(), "没有配置") {
		t.Fatalf("未配置应报 ErrNotConfigured，得到 %v", err)
	}
	if Configured() {
		t.Fatal("未配置时 Configured() 应为 false")
	}

	FeedURL, PublicKeyB64 = "https://example.test/feed.json", "not-base64!!"
	if _, err := NewChecker("0.1.0"); err == nil {
		t.Fatal("坏公钥应报错")
	}

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	PublicKeyB64 = base64.StdEncoding.EncodeToString(pub)
	if !Configured() {
		t.Fatal("配好 feed 与公钥后 Configured() 应为 true")
	}
	checker, err := NewChecker("0.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if checker.CurrentVersion != "0.1.0" || len(checker.PublicKey) != ed25519.PublicKeySize {
		t.Fatalf("checker = %+v", checker)
	}
	if !strings.Contains(checker.UserAgent, "0.1.0") {
		t.Fatalf("User-Agent 应带版本: %q", checker.UserAgent)
	}
}
