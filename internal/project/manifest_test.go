package project

import (
	"bytes"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"

	"compositor-win/internal/domain"
)

// testUUIDs 是测试用的固定 UUID（大写，符合 macOS uuidString 输出）。
const (
	testDocID   = "0C5E7A91-3B2D-4F6A-8E1C-9D0B7A6F5E4D"
	testLayerID = "6F1D3C2A-0B7E-4E8A-9C4D-2A1B3C4D5E6F"
	testGroupID = "A1B2C3D4-E5F6-4A7B-8C9D-0E1F2A3B4C5D"
	testGuideID = "B2C3D4E5-F6A7-4B8C-9D0E-1F2A3B4C5D6E"
)

// topKeyOrder 提取缩进恰好 2 空格的行——即最外层对象的键（嵌套对象缩进更深）。
func topKeyOrder(t *testing.T, data []byte) []string {
	t.Helper()
	var order []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "  \"") || strings.HasPrefix(line, "   ") {
			continue
		}
		rest := strings.TrimPrefix(line, "  ")
		if !strings.HasPrefix(rest, "\"") {
			continue
		}
		if end := strings.Index(rest[1:], "\""); end > 0 {
			order = append(order, rest[1:1+end])
		}
	}
	return order
}

func TestEncodeManifestSortedKeys(t *testing.T) {
	doc := minimalDoc()
	data, err := encodeManifest(doc)
	if err != nil {
		t.Fatalf("encodeManifest: %v", err)
	}
	order := topKeyOrder(t, data)
	if !sort.StringsAreSorted(order) {
		t.Fatalf("顶层键未按字典序排序: %v", order)
	}
	// 对照 JSONEncoder [.prettyPrinted, .sortedKeys] 的预期键序。
	want := []string{"activeLayerID", "colorSpace", "documentID", "format", "height", "layers", "resolution", "version", "width"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("键序 = %v, want %v", order, want)
	}
}

func TestEncodeManifestPrettyAndNoHTMLEscape(t *testing.T) {
	doc := minimalDoc()
	doc.Layers[0].Adjustment = &domain.Adjustment{Kind: domain.AdjustmentBlackWhite}
	data, err := encodeManifest(doc)
	if err != nil {
		t.Fatalf("encodeManifest: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "\n  \"format\" :") && !strings.Contains(text, "\n  \"format\":") {
		t.Fatalf("应为 2 空格缩进的 pretty JSON:\n%s", text)
	}
	if !strings.Contains(text, "Black & White") {
		t.Fatalf("HTML 转义应关闭（& 原样输出）:\n%s", text)
	}
	if strings.Contains(text, `\u0026`) {
		t.Fatalf("不应出现 \\u0026 转义:\n%s", text)
	}
	// 同一文档两次编码必须字节一致。
	again, err := encodeManifest(doc)
	if err != nil || !bytes.Equal(data, again) {
		t.Fatalf("编码不稳定: %v", err)
	}
	// 可选字段缺省时不得出现（Swift optional → 键省略）。
	for _, absent := range []string{"\"guides\"", "\"opacity\"", "\"maskFile\"", "\"text\""} {
		if strings.Contains(text, absent) {
			t.Fatalf("可选字段 %s 不应出现:\n%s", absent, text)
		}
	}
	if !strings.Contains(text, "\"isVisible\" : true") && !strings.Contains(text, "\"isVisible\": true") {
		t.Fatalf("非可选字段 isVisible 必须输出:\n%s", text)
	}
}

func TestEncodeManifestOmitEmptyPointerSemantics(t *testing.T) {
	// opacity 为显式 0（非 nil 指针）必须写出——omitempty 对指针只看 nil。
	doc := minimalDoc()
	zero := 0.0
	doc.Layers[0].Opacity = &zero
	data, err := encodeManifest(doc)
	if err != nil {
		t.Fatalf("encodeManifest: %v", err)
	}
	if !strings.Contains(string(data), "\"opacity\"") {
		t.Fatalf("显式 opacity 0 不应被省略:\n%s", data)
	}
}

func minimalDoc() *domain.Document {
	doc := &domain.Document{
		Format:        domain.FormatID,
		Version:       domain.FormatVersion,
		ColorSpace:    domain.ColorSpaceSRGB,
		DocumentID:    testDocID,
		Width:         100,
		Height:        80,
		ActiveLayerID: ptr(testLayerID),
		Resolution:    ptr(72),
		Layers: []domain.Layer{
			{ID: testLayerID, Name: "Background", IsVisible: true,
				Transform: domain.Transform{Sampling: domain.SamplingHighQuality}},
		},
	}
	return doc
}

func TestDecodeManifestHeader(t *testing.T) {
	data, err := encodeManifest(minimalDoc())
	if err != nil {
		t.Fatalf("encodeManifest: %v", err)
	}
	doc, err := decodeManifest(data)
	if err != nil {
		t.Fatalf("decodeManifest: %v", err)
	}
	if doc.Format != domain.FormatID || doc.Version != domain.FormatVersion || doc.ColorSpace != "sRGB" {
		t.Fatalf("头部字段错误: %+v", doc)
	}
}

func TestDecodeManifestRejectsBadHeader(t *testing.T) {
	base, err := encodeManifest(minimalDoc())
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		mutate  func(m map[string]any)
		wantErr error
	}{
		{"format 错误", func(m map[string]any) { m["format"] = "com.other.project" }, ErrInvalid},
		{"version 12", func(m map[string]any) { m["version"] = 12 }, &VersionError{Version: 12}},
		{"version 显式 0", func(m map[string]any) { m["version"] = 0 }, &VersionError{Version: 0}},
		{"version 缺失", func(m map[string]any) { delete(m, "version") }, ErrInvalid},
		{"layers 缺失", func(m map[string]any) { delete(m, "layers") }, ErrInvalid},
		{"documentID 缺失", func(m map[string]any) { delete(m, "documentID") }, ErrInvalid},
		{"documentID 非 UUID", func(m map[string]any) { m["documentID"] = "not-a-uuid" }, ErrInvalid},
		{"图层 ID 非 UUID", func(m map[string]any) {
			m["layers"].([]any)[0].(map[string]any)["id"] = "xyz"
		}, ErrInvalid},
	}
	for _, tc := range cases {
		mutated := map[string]any{}
		if err := json.Unmarshal(base, &mutated); err != nil {
			t.Fatal(err)
		}
		tc.mutate(mutated)
		data, _ := json.Marshal(mutated)
		_, err := decodeManifest(data)
		if !matchWant(err, tc.wantErr) {
			t.Fatalf("%s: 错误 = %v, want %v", tc.name, err, tc.wantErr)
		}
	}
	if _, err := decodeManifest([]byte("{not json")); !errors.Is(err, ErrInvalid) {
		t.Fatalf("坏 JSON 应 ErrInvalid, got %v", err)
	}
}

func matchWant(err error, want error) bool {
	if want == nil {
		return err == nil
	}
	if v, ok := want.(*VersionError); ok {
		var got *VersionError
		return errors.As(err, &got) && got.Version == v.Version
	}
	return errors.Is(err, want)
}

func TestDecodeManifestNormalizesUUIDs(t *testing.T) {
	// Foundation 的 UUID 解码不区分大小写；JSONEncoder 写出大写。读入时归一化。
	raw := `{
		"format": "com.compositor.project",
		"version": 11,
		"colorSpace": "sRGB",
		"documentID": "0c5e7a91-3b2d-4f6a-8e1c-9d0b7a6f5e4d",
		"width": 100,
		"height": 80,
		"activeLayerID": "6f1d3c2a-0b7e-4e8a-9c4d-2a1b3c4d5e6f",
		"layers": [
			{"id": "6f1d3c2a-0b7e-4e8a-9c4d-2a1b3c4d5e6f", "name": "L", "isVisible": true,
			 "transform": {"origin": [0,0], "size": [10,10], "rotation": 0, "flipX": false, "flipY": false, "sampling": "High quality"},
			 "parentID": null}
		],
		"guides": [{"id": "b2c3d4e5-f6a7-4b8c-9d0e-1f2a3b4c5d6e", "axis": "horizontal", "position": 10}]
	}`
	doc, err := decodeManifest([]byte(raw))
	if err != nil {
		t.Fatalf("decodeManifest: %v", err)
	}
	if doc.DocumentID != testDocID {
		t.Fatalf("documentID 未归一化: %s", doc.DocumentID)
	}
	if doc.Layers[0].ID != testLayerID {
		t.Fatalf("图层 ID 未归一化: %s", doc.Layers[0].ID)
	}
	if doc.Guides() == nil || doc.Guides()[0].ID != testGuideID {
		t.Fatalf("参考线 ID 未归一化: %v", doc.Guides())
	}
}

// macOSStyleManifest 对齐 reference/Swift/docs/writing-comp-files.md 的示例
// （键序为逻辑序而非字典序，验证读取不依赖排序）。
const macOSStyleManifest = `{
  "format": "com.compositor.project",
  "version": 11,
  "colorSpace": "sRGB",
  "documentID": "0C5E7A91-3B2D-4F6A-8E1C-9D0B7A6F5E4D",
  "width": 1920,
  "height": 1080,
  "resolution": 72,
  "activeLayerID": "6F1D3C2A-0B7E-4E8A-9C4D-2A1B3C4D5E6F",
  "layers": [
    {
      "id": "6F1D3C2A-0B7E-4E8A-9C4D-2A1B3C4D5E6F",
      "name": "Background",
      "imageFile": "6F1D3C2A-0B7E-4E8A-9C4D-2A1B3C4D5E6F.png",
      "isVisible": true,
      "isGroup": false,
      "opacity": 1,
      "blendMode": "Normal",
      "transform": {
        "origin": [0, 0],
        "size": [1920, 1080],
        "rotation": 0,
        "flipX": false,
        "flipY": false,
        "sampling": "High quality"
      }
    }
  ]
}`

func TestDecodeMacOSStyleManifest(t *testing.T) {
	doc, err := decodeManifest([]byte(macOSStyleManifest))
	if err != nil {
		t.Fatalf("decodeManifest(macOS 示例): %v", err)
	}
	if doc.Width != 1920 || doc.Height != 1080 || doc.Version != 11 {
		t.Fatalf("字段错误: %+v", doc)
	}
	if doc.Resolution == nil || *doc.Resolution != 72 {
		t.Fatalf("resolution 应为 72: %v", doc.Resolution)
	}
	layer := doc.Layers[0]
	if layer.ImageFile == nil || *layer.ImageFile != testLayerID+".png" {
		t.Fatalf("imageFile 错误: %v", layer.ImageFile)
	}
	if layer.IsGroup == nil || *layer.IsGroup {
		t.Fatal("isGroup 应为显式 false")
	}
	if layer.Opacity == nil || *layer.Opacity != 1 {
		t.Fatal("opacity 应为 1")
	}
	if layer.BlendMode == nil || *layer.BlendMode != domain.BlendNormal {
		t.Fatal("blendMode 应为 Normal")
	}
	if layer.Transform.Origin != [2]float64{0, 0} || layer.Transform.Size != [2]float64{1920, 1080} {
		t.Fatalf("transform 错误: %+v", layer.Transform)
	}
}

func TestParseUUID(t *testing.T) {
	if up, ok := parseUUID("6f1d3c2a-0b7e-4e8a-9c4d-2a1b3c4d5e6f"); !ok || up != testLayerID {
		t.Fatalf("小写 UUID 应归一化为大写: %q %v", up, ok)
	}
	for _, bad := range []string{"", "xyz", "6F1D3C2A0B7E4E8A9C4D2A1B3C4D5E6F", "6F1D3C2A-0B7E-4E8A-9C4D-2A1B3C4D5E6", "6F1D3C2A-0B7E-4E8A-9C4D-2A1B3C4D5E6G"} {
		if _, ok := parseUUID(bad); ok {
			t.Fatalf("%q 不应被接受为 UUID", bad)
		}
	}
}
