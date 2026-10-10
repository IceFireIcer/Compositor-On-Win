package update

import (
	"encoding/json"
	"strings"
	"testing"
)

// feedJSON 造一份格式正确的 feed（scripts/genfeed 的输出形状），mutate 用来做
// 单点破坏。
func feedJSON(t *testing.T, mutate func(*Feed)) string {
	t.Helper()
	feed := Feed{
		App:     AppID,
		Channel: "stable",
		Latest: Release{
			Version:     "0.2.0",
			PublishedAt: "2026-10-10T12:00:00Z",
			Notes:       "修了几个 bug",
			Installer: Installer{
				URL:         "https://example.test/download/Compositor-0.2.0-setup.exe",
				Size:        12345678,
				SHA256:      strings.Repeat("ab", 32),
				EdSignature: "c2lnbmF0dXJl",
			},
		},
	}
	if mutate != nil {
		mutate(&feed)
	}
	data, err := json.Marshal(feed)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestParseFeedAcceptsTheGeneratorShape(t *testing.T) {
	feed, err := ParseFeed([]byte(feedJSON(t, nil)))
	if err != nil {
		t.Fatalf("合法 feed 应通过: %v", err)
	}
	if feed.App != AppID || feed.Channel != "stable" {
		t.Fatalf("应用/通道 = %q/%q", feed.App, feed.Channel)
	}
	rel := feed.Latest
	if rel.Version != "0.2.0" || rel.Installer.Size != 12345678 {
		t.Fatalf("版本/大小 = %q/%d", rel.Version, rel.Installer.Size)
	}
	if rel.Notes == "" || rel.PublishedAt == "" {
		t.Fatalf("说明/发布时间应保留: %+v", rel)
	}
}

func TestParseFeedRejectsUnsafeFeeds(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Feed)
		wantSub string
	}{
		{"别的应用", func(f *Feed) { f.App = "other" }, "别的应用"},
		{"没有版本", func(f *Feed) { f.Latest.Version = "  " }, "版本"},
		{"ftp 地址", func(f *Feed) { f.Latest.Installer.URL = "ftp://example.test/a.exe" }, "地址无效"},
		{"http 明文地址", func(f *Feed) { f.Latest.Installer.URL = "http://example.test/a.exe" }, "https"},
		{"相对地址", func(f *Feed) { f.Latest.Installer.URL = "/download/a.exe" }, "地址无效"},
		{"没有大小", func(f *Feed) { f.Latest.Installer.Size = 0 }, "大小"},
		{"负数大小", func(f *Feed) { f.Latest.Installer.Size = -5 }, "大小"},
		{"sha256 太短", func(f *Feed) { f.Latest.Installer.SHA256 = "abcd" }, "sha256"},
		{"sha256 非十六进制", func(f *Feed) { f.Latest.Installer.SHA256 = strings.Repeat("zz", 32) }, "sha256"},
		{"没有签名", func(f *Feed) { f.Latest.Installer.EdSignature = " " }, "签名"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseFeed([]byte(feedJSON(t, tc.mutate)))
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("应被拒绝（含 %q），得到 %v", tc.wantSub, err)
			}
		})
	}

	if _, err := ParseFeed([]byte("not json")); err == nil {
		t.Fatal("非 JSON 应被拒绝")
	}
	// 未知字段也拒绝：feed 与这个版本的应用不是同一代协议时，宁可让用户手动下载，
	// 也不要按自己理解的子集去装一个签名好的陌生包。
	extra, err := json.Marshal(map[string]any{
		"app": AppID, "channel": "stable", "surprise": 1,
		"latest": map[string]any{"version": "0.2.0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseFeed(extra); err == nil {
		t.Fatal("未知字段应被拒绝")
	}
}
