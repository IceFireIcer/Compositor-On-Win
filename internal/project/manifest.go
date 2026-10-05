package project

import (
	"bytes"
	"encoding/json"
	"strings"

	"compositor-win/internal/domain"
)

// jsonIndent is the pretty-print indentation. The macOS store encodes with
// JSONEncoder [.prettyPrinted, .sortedKeys], which renders two-space indents
// and a " : " key separator; the exact byte style cannot be confirmed without
// a real macOS .comp fixture, so the separator stays the Go-native ": " for
// now. Acceptance for ticket 05 is "macOS files load" + "save/load
// round-trips", not byte equality.
// TODO(字节级对齐): 用 macOS 1.4.5 真实 .comp 夹具核对缩进与键值分隔符后收敛。
const jsonIndent = "  "

// encodeManifest ports the macOS JSONEncoder setup: the document is first
// marshaled, re-parsed into a generic tree and re-encoded — encoding/json
// sorts map keys lexicographically at every level, matching .sortedKeys.
// HTML escaping is off so values like the "Black & White" adjustment kind
// stay verbatim, as JSONEncoder writes them.
func encodeManifest(doc *domain.Document) ([]byte, error) {
	compact, err := json.Marshal(doc)
	if err != nil {
		return nil, encodef("清单无法编码: %v", err)
	}
	var tree any
	if err := json.Unmarshal(compact, &tree); err != nil {
		return nil, encodef("清单无法编码: %v", err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", jsonIndent)
	if err := enc.Encode(tree); err != nil {
		return nil, encodef("清单无法编码: %v", err)
	}
	// json.Encoder 结尾追加换行，JSONEncoder 的输出没有。
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// decodeManifest ports the two-stage read of readPackage: a header decode
// decides between ErrInvalid (bad format or bad JSON) and VersionError (a
// version outside 1–11), then the full decode fills the domain document.
func decodeManifest(data []byte) (*domain.Document, error) {
	var header struct {
		Format  string `json:"format"`
		Version *int   `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return nil, invalidf("manifest.json 无法解析: %v", err)
	}
	if header.Format != domain.FormatID {
		return nil, invalidf("format 必须为 %q，得到 %q", domain.FormatID, header.Format)
	}
	// version 是非可选键：缺失属于元数据损坏（Swift keyNotFound），显式
	// 超范围才是版本错误。
	if header.Version == nil {
		return nil, invalidf("manifest 缺少 version")
	}
	if *header.Version < 1 || *header.Version > domain.FormatVersion {
		return nil, &VersionError{Version: *header.Version}
	}
	var doc domain.Document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, invalidf("manifest 无法解析: %v", err)
	}
	// layers 是非可选键：缺失或 null 都拒绝（Swift keyNotFound）。
	if doc.Layers == nil {
		return nil, invalidf("manifest 缺少 layers")
	}
	if err := normalizeUUIDs(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// parseUUID accepts the canonical dashed UUID form, case-insensitively, and
// returns its uppercase form — the macOS JSONEncoder always writes
// uuidString, so uppercase is the on-disk spelling.
func parseUUID(s string) (string, bool) {
	if len(s) != 36 {
		return "", false
	}
	for i := 0; i < 36; i++ {
		switch i {
		case 8, 13, 18, 23:
			if s[i] != '-' {
				return "", false
			}
		default:
			c := s[i]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return "", false
			}
		}
	}
	return strings.ToUpper(s), true
}

// normalizeUUIDs uppercases every UUID in the manifest (document, layers,
// parents, clip sources, guides), mirroring how the macOS reader holds UUIDs
// case-insensitively and writes them uppercase. A malformed UUID rejects the
// whole manifest, as the Swift UUID decode does.
func normalizeUUIDs(doc *domain.Document) error {
	id, ok := parseUUID(doc.DocumentID)
	if !ok {
		return invalidf("documentID 不是有效的 UUID: %q", doc.DocumentID)
	}
	doc.DocumentID = id
	if doc.ActiveLayerID != nil {
		up, ok := parseUUID(*doc.ActiveLayerID)
		if !ok {
			return invalidf("activeLayerID 不是有效的 UUID: %q", *doc.ActiveLayerID)
		}
		doc.ActiveLayerID = &up
	}
	for i := range doc.Layers {
		l := &doc.Layers[i]
		up, ok := parseUUID(l.ID)
		if !ok {
			return invalidf("图层 %d 的 id 不是有效的 UUID: %q", i, l.ID)
		}
		l.ID = up
		if l.ParentID != nil {
			up, ok := parseUUID(*l.ParentID)
			if !ok {
				return invalidf("图层 %s 的 parentID 不是有效的 UUID: %q", l.ID, *l.ParentID)
			}
			l.ParentID = &up
		}
		if l.MaskSourceID != nil {
			up, ok := parseUUID(*l.MaskSourceID)
			if !ok {
				return invalidf("图层 %s 的 maskSourceID 不是有效的 UUID: %q", l.ID, *l.MaskSourceID)
			}
			l.MaskSourceID = &up
		}
	}
	if doc.GuidesList != nil {
		for i := range *doc.GuidesList {
			g := &(*doc.GuidesList)[i]
			up, ok := parseUUID(g.ID)
			if !ok {
				return invalidf("参考线 %d 的 id 不是有效的 UUID: %q", i, g.ID)
			}
			g.ID = up
		}
	}
	return nil
}
