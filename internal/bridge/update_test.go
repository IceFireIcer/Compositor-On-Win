package bridge

// 自动更新（票 50）的桥接测试：feed 用 httptest 托管，公钥/feed 地址走构建注入的
// 两个包级变量（测试里临时替换），安装器启动走接缝。

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"compositor-win/internal/update"
)

// useUpdateSource 把"构建注入"的 feed 地址与公钥换成测试值，并返回一个能改 feed
// 内容的服务器。
func useUpdateSource(t *testing.T) (*httptest.Server, ed25519.PrivateKey, func(update.Release)) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	savedURL, savedKey, savedVersion := update.FeedURL, update.PublicKeyB64, BuildVersion
	savedClient := update.HTTPClient
	t.Cleanup(func() {
		update.FeedURL, update.PublicKeyB64, BuildVersion = savedURL, savedKey, savedVersion
		update.HTTPClient = savedClient
	})
	update.PublicKeyB64 = base64.StdEncoding.EncodeToString(pub)
	BuildVersion = "0.1.0"

	var current update.Feed
	// TLS 服务器：internal/update 只接受 https（与生成器规则一致），所以测试也得走
	// https，并把信任测试证书的客户端注入进去。
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feed.json":
			_ = json.NewEncoder(w).Encode(current)
		case "/setup.exe":
			_, _ = w.Write([]byte("installer-bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	update.FeedURL = server.URL + "/feed.json"
	update.HTTPClient = server.Client()

	set := func(rel update.Release) {
		payload := []byte("installer-bytes")
		sum := sha256.Sum256(payload)
		rel.Installer.URL = server.URL + "/setup.exe"
		rel.Installer.Size = int64(len(payload))
		rel.Installer.SHA256 = hex.EncodeToString(sum[:])
		rel.Installer.EdSignature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))
		current = update.Feed{App: update.AppID, Channel: "stable", Latest: rel}
	}
	return server, priv, set
}

func TestCheckForUpdatesReportsEveryStateWithoutThrowing(t *testing.T) {
	svc, _ := newTestService(t, 8, 8)
	_, _, setFeed := useUpdateSource(t)

	// 有新版本。
	setFeed(update.Release{Version: "0.2.0", Notes: "新东西", PublishedAt: "2026-10-10T12:00:00Z"})
	reply, err := svc.CheckForUpdates()
	if err != nil {
		t.Fatal(err)
	}
	got := parseJSON[updateStatusReply](t, reply)
	if got.Status != "available" || got.Version != "0.2.0" || got.Notes != "新东西" {
		t.Fatalf("有新版本时应报 available: %+v", got)
	}
	if got.CurrentVersion != "0.1.0" || got.Size == 0 {
		t.Fatalf("应带上当前版本与大小: %+v", got)
	}

	// 已经是最新。
	setFeed(update.Release{Version: "0.1.0"})
	reply, _ = svc.CheckForUpdates()
	if got := parseJSON[updateStatusReply](t, reply); got.Status != "up-to-date" {
		t.Fatalf("同版本应报 up-to-date: %+v", got)
	}

	// 没配更新源：状态而不是错误。
	saved := update.FeedURL
	update.FeedURL = ""
	reply, err = svc.CheckForUpdates()
	if err != nil {
		t.Fatal(err)
	}
	if got := parseJSON[updateStatusReply](t, reply); got.Status != "unconfigured" || got.Error == "" {
		t.Fatalf("未配置应报 unconfigured: %+v", got)
	}
	update.FeedURL = saved

	// 服务挂了：状态 error，仍然不抛。
	update.FeedURL = "http://127.0.0.1:1/feed.json"
	reply, err = svc.CheckForUpdates()
	if err != nil {
		t.Fatal(err)
	}
	if got := parseJSON[updateStatusReply](t, reply); got.Status != "error" || got.Error == "" {
		t.Fatalf("连不上应报 error: %+v", got)
	}
}

func TestDownloadUpdateVerifiesAndKeepsTheInstaller(t *testing.T) {
	svc, _ := newTestService(t, 8, 8)
	_, _, setFeed := useUpdateSource(t)
	setFeed(update.Release{Version: "0.2.0"})

	if _, err := svc.CheckForUpdates(); err != nil {
		t.Fatal(err)
	}
	reply, err := svc.DownloadUpdate()
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	var got struct {
		Path    string `json:"path"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(reply), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != "0.2.0" || got.Path == "" {
		t.Fatalf("下载回复 = %+v", got)
	}
	data, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatalf("下载的包应落盘: %v", err)
	}
	if string(data) != "installer-bytes" {
		t.Fatalf("内容不符: %q", data)
	}
	t.Cleanup(func() { _ = os.Remove(got.Path) })

	// 没有先检查也能直接下载（自己会查一次）。
	svc.updateMu.Lock()
	svc.pendingUpdate = nil
	svc.updateMu.Unlock()
	if _, err := svc.DownloadUpdate(); err != nil {
		t.Fatalf("没有 pending 时也应能下载: %v", err)
	}

	// 已是最新时明确报 ErrUpToDate。
	setFeed(update.Release{Version: "0.1.0"})
	svc.updateMu.Lock()
	svc.pendingUpdate = nil
	svc.updateMu.Unlock()
	if _, err := svc.DownloadUpdate(); !errors.Is(err, update.ErrUpToDate) {
		t.Fatalf("已是最新应报 ErrUpToDate，得到 %v", err)
	}
}

// 重入护栏：绑定调用可能被多个入口并发触发，两次下载会往同一个目标文件写。
func TestDownloadUpdateRefusesConcurrentDownloads(t *testing.T) {
	svc, _ := newTestService(t, 8, 8)
	_, _, setFeed := useUpdateSource(t)
	setFeed(update.Release{Version: "0.2.0"})
	if _, err := svc.CheckForUpdates(); err != nil {
		t.Fatal(err)
	}
	svc.updateMu.Lock()
	svc.downloading = true
	svc.updateMu.Unlock()

	if _, err := svc.DownloadUpdate(); err == nil || !strings.Contains(err.Error(), "下载中") {
		t.Fatalf("并发下载应被拒绝，得到 %v", err)
	}

	// 护栏要在失败路径上也放掉（defer 里清），否则一次失败会永久锁死下载。
	svc.updateMu.Lock()
	busy := svc.downloading
	svc.updateMu.Unlock()
	if !busy {
		t.Fatal("测试前置状态被改了")
	}
	svc.updateMu.Lock()
	svc.downloading = false
	svc.updateMu.Unlock()

	if _, err := svc.DownloadUpdate(); err != nil {
		t.Fatalf("放掉护栏后应能正常下载: %v", err)
	}
	svc.updateMu.Lock()
	released := svc.downloading
	svc.updateMu.Unlock()
	if released {
		t.Fatal("下载结束后护栏应已释放")
	}
}

func TestApplyUpdateRequiresAVerifiedDownloadAndLaunchesIt(t *testing.T) {
	svc, _ := newTestService(t, 8, 8)
	useUpdateSource(t)

	if _, err := svc.ApplyUpdate(); err == nil {
		t.Fatal("没有下载好的包时应报错")
	}

	// 摆一个"已下载"的文件，换成假的启动器：应被调用，且不真的起安装器。
	path := filepath.Join(t.TempDir(), "Compositor-0.2.0-setup.exe")
	if err := os.WriteFile(path, []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc.updateMu.Lock()
	svc.updateFile = path
	svc.updateMu.Unlock()

	saved := launchUpdateInstaller
	var launched string
	launchUpdateInstaller = func(p string) error { launched = p; return nil }
	t.Cleanup(func() { launchUpdateInstaller = saved })

	reply, err := svc.ApplyUpdate()
	if err != nil {
		t.Fatalf("应用更新失败: %v", err)
	}
	if launched != path {
		t.Fatalf("应启动下载好的包，得到 %q", launched)
	}
	if !strings.Contains(reply, "launched") {
		t.Fatalf("回复 = %s", reply)
	}

	launchUpdateInstaller = func(string) error { return errors.New("起不来") }
	if _, err := svc.ApplyUpdate(); err == nil {
		t.Fatal("启动失败应报错")
	}
}

func TestUpdateInfoReportsConfiguration(t *testing.T) {
	svc, _ := newTestService(t, 8, 8)
	useUpdateSource(t)
	reply, err := svc.UpdateInfo()
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(reply), &got); err != nil {
		t.Fatal(err)
	}
	if got["currentVersion"] != "0.1.0" || got["configured"] != true {
		t.Fatalf("更新信息 = %+v", got)
	}
	if feeds, _ := got["feedUrl"].(string); !strings.Contains(feeds, "/feed.json") {
		t.Fatalf("更新信息应带上 feed 地址: %+v", got)
	}
}
