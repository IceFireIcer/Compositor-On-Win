package bridge

// 参考线（票 14）：Go 的 domain.Document 一直有 GuidesList，画布缩放
// （doc_geometry.go）与 manifest 读写也都认识它，但桥里此前没有任何写入口，
// 前端拖出的参考线无处可存。SetGuides 补上这一环。
//
// 空列表存成**指向空切片的非 nil 指针**：manifest 的 json:"guides,omitempty"
// 对 nil 指针整个省略、对空切片写出 "guides":[]，"清空参考线"必须和"从未有
// 参考线"区分开，否则保存再打开参考线会复活（history.normalizeDocument 只在
// 比较时把空切片当 nil，不写回文档）。

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"compositor-win/internal/domain"
)

// SetGuides replaces the active document's guide list as one "参考线" undo
// step. guidesJSON is [{"id":"<UUID>","axis":"horizontal"|"vertical",
// "position":<number>}, …]; "" and "[]" both clear the list.
//
// The list is validated with the same limits domain.Validate enforces (≤1000
// guides, |position| ≤ 1e6, a known axis) plus the manifest's UUID rule —
// project.parseUUID rejects a malformed id at load time, so accepting one here
// would write a package that can never be opened again. A missing id gets a
// fresh UUID; ids are normalized to the uppercase on-disk spelling.
func (s *Service) SetGuides(guidesJSON string) (string, error) {
	if s.ws == nil {
		return "", errNoDocument
	}
	guides, err := parseGuides(guidesJSON)
	if err != nil {
		return "", err
	}
	return s.ws.EditActive("参考线", func(sess *session) error {
		list := guides
		sess.doc.GuidesList = &list
		return nil
	})
}

// parseGuides decodes and validates the frontend's guide payload. An empty
// payload is an empty (non-nil) list, not "no guides".
func parseGuides(raw string) ([]domain.Guide, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []domain.Guide{}, nil
	}
	var guides []domain.Guide
	if err := json.Unmarshal([]byte(trimmed), &guides); err != nil {
		return nil, fmt.Errorf("无法解析参考线: %w", err)
	}
	if len(guides) > domain.MaxGuides {
		return nil, fmt.Errorf("参考线数量 %d 超过 %d 上限", len(guides), domain.MaxGuides)
	}
	out := make([]domain.Guide, 0, len(guides))
	seen := make(map[string]bool, len(guides))
	for i, g := range guides {
		switch g.Axis {
		case domain.GuideAxisHorizontal, domain.GuideAxisVertical:
		default:
			return nil, fmt.Errorf("参考线 %d 方向无效: %q", i, g.Axis)
		}
		if math.IsNaN(g.Position) || math.IsInf(g.Position, 0) || math.Abs(g.Position) > domain.MaxGuidePosition {
			return nil, fmt.Errorf("参考线 %d 位置 %v 超出 ±%d px", i, g.Position, domain.MaxGuidePosition)
		}
		id := strings.TrimSpace(g.ID)
		if id == "" {
			id = newUUID()
		} else {
			up, ok := normalizeUUID(id)
			if !ok {
				return nil, fmt.Errorf("参考线 %d 的 id 不是有效的 UUID: %q", i, g.ID)
			}
			id = up
		}
		if seen[id] {
			return nil, fmt.Errorf("参考线 ID 重复: %s", id)
		}
		seen[id] = true
		out = append(out, domain.Guide{ID: id, Axis: g.Axis, Position: g.Position})
	}
	return out, nil
}

// normalizeUUID is project.parseUUID's rule (canonical dashed form,
// case-insensitive, uppercased) — kept here because that one is unexported and
// the bridge must not hand the manifest a guide id the loader will reject.
func normalizeUUID(s string) (string, bool) {
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
