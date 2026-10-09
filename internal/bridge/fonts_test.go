package bridge

import (
	"encoding/json"
	"strings"
	"testing"
)

// ListFonts 与文档无关：没有打开任何文档时也必须可用（工具头在空工作
// 区里也要能列出字体）。
func TestListFontsWithoutDocument(t *testing.T) {
	ws := NewWorkspace()
	svc := NewService(ws)
	if tabs := ws.Snapshot().Tabs; len(tabs) != 0 {
		t.Fatalf("测试预期没有打开的文档, got %d 个", len(tabs))
	}
	raw, err := svc.ListFonts()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"fonts":`) {
		t.Fatalf("响应缺少 fonts 字段: %s", raw)
	}
	var payload struct {
		Fonts []struct {
			Family         string `json:"family"`
			PostScriptName string `json:"postScriptName"`
			Subfamily      string `json:"subfamily"`
		} `json:"fonts"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if len(payload.Fonts) == 0 {
		t.Skip("本机字体索引为空")
	}
	for _, f := range payload.Fonts {
		if f.Family == "" || f.PostScriptName == "" {
			t.Fatalf("字体条目缺字段: %+v", f)
		}
	}
}
