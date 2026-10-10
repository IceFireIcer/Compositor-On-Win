package bridge

// 参考线测试（票 14）：SetGuides 是桥里唯一写参考线的 RPC。空列表必须存成
// **非 nil 的空切片**，否则 manifest 的 omitempty 会把"清空参考线"和"从未有
// 参考线"混为一谈，保存后参考线会复活。

import (
	"encoding/json"
	"strings"
	"testing"

	"compositor-win/internal/domain"
)

const (
	guideA = "B2C3D4E5-F6A7-4B8C-9D0E-1F2A3B4C5D6E"
	guideB = "C3D4E5F6-A7B8-4C9D-8E1F-2A3B4C5D6E7F"
)

func TestSetGuidesReplacesTheList(t *testing.T) {
	svc, _ := newTestService(t, 40, 30)

	raw, err := json.Marshal([]domain.Guide{
		{ID: guideA, Axis: domain.GuideAxisHorizontal, Position: 10.5},
		{ID: guideB, Axis: domain.GuideAxisVertical, Position: -3},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.SetGuides(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvelope(t, out)
	if env.Rev != 1 {
		t.Fatalf("设置参考线后 rev = %d，想要 1", env.Rev)
	}
	guides := env.Doc.Guides()
	if len(guides) != 2 {
		t.Fatalf("参考线数 = %d，想要 2", len(guides))
	}
	if guides[0].ID != guideA || guides[0].Axis != domain.GuideAxisHorizontal || guides[0].Position != 10.5 {
		t.Fatalf("第一条参考线 = %+v", guides[0])
	}
	if guides[1].ID != guideB || guides[1].Axis != domain.GuideAxisVertical || guides[1].Position != -3 {
		t.Fatalf("第二条参考线 = %+v", guides[1])
	}
	if got := activeSessionOf(t, svc).hist.UndoName(); got != "参考线" {
		t.Fatalf("历史项名 = %q，想要「参考线」", got)
	}

	// 再设一次是替换而不是追加。
	out, err = svc.SetGuides(`[{"id":"` + guideA + `","axis":"vertical","position":7}]`)
	if err != nil {
		t.Fatal(err)
	}
	env = parseEnvelope(t, out)
	if len(env.Doc.Guides()) != 1 || env.Doc.Guides()[0].Position != 7 {
		t.Fatalf("替换后 = %+v", env.Doc.Guides())
	}

	// 两步都能撤销。
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := activeSessionOf(t, svc).doc.Guides(); len(got) != 2 {
		t.Fatalf("撤销一步后参考线数 = %d，想要 2", len(got))
	}
}

func TestSetGuidesNormalizesUUIDCase(t *testing.T) {
	svc, _ := newTestService(t, 40, 30)
	lower := strings.ToLower(guideA)
	out, err := svc.SetGuides(`[{"id":"` + lower + `","axis":"horizontal","position":1}]`)
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvelope(t, out)
	if got := env.Doc.Guides()[0].ID; got != guideA {
		t.Fatalf("参考线 ID = %q，想要大写 %q（manifest 的 on-disk 拼写）", got, guideA)
	}
}

func TestSetGuidesGeneratesAMissingID(t *testing.T) {
	svc, _ := newTestService(t, 40, 30)
	out, err := svc.SetGuides(`[{"axis":"horizontal","position":4}]`)
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvelope(t, out)
	guides := env.Doc.Guides()
	if len(guides) != 1 || !uuidRe.MatchString(guides[0].ID) {
		t.Fatalf("缺 id 的参考线应拿到新 UUID，得到 %+v", guides)
	}
}

func TestSetGuidesEmptyListKeepsANonNilPointer(t *testing.T) {
	svc, _ := newTestService(t, 40, 30)

	if _, err := svc.SetGuides(`[{"id":"` + guideA + `","axis":"horizontal","position":2}]`); err != nil {
		t.Fatal(err)
	}
	out, err := svc.SetGuides("[]")
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvelope(t, out)
	if env.Doc.GuidesList == nil {
		t.Fatal("清空参考线后 GuidesList 必须是空切片指针，不能是 nil")
	}
	if len(*env.Doc.GuidesList) != 0 {
		t.Fatalf("清空后仍有 %d 条参考线", len(*env.Doc.GuidesList))
	}
	// 序列化必须写出 "guides":[]，omitempty 不能把它抹掉。
	raw, err := json.Marshal(env.Doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"guides":[]`) {
		t.Fatalf("空参考线列表没有序列化成 guides:[]：%s", raw)
	}

	// 撤销把列表还原。
	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := activeSessionOf(t, svc).doc.Guides(); len(got) != 1 {
		t.Fatalf("撤销后参考线数 = %d，想要 1", len(got))
	}
}

func TestSetGuidesRejectsBadRequests(t *testing.T) {
	svc, _ := newTestService(t, 40, 30)

	cases := []struct {
		name string
		json string
		want string
	}{
		{"非法 JSON", "{oops", "参考线"},
		{"方向无效", `[{"id":"` + guideA + `","axis":"diagonal","position":0}]`, "方向"},
		{"位置超限", `[{"id":"` + guideA + `","axis":"horizontal","position":1000001}]`, "位置"},
		{"ID 不是 UUID", `[{"id":"not-a-uuid","axis":"horizontal","position":0}]`, "UUID"},
		{"ID 重复", `[{"id":"` + guideA + `","axis":"horizontal","position":0},{"id":"` + strings.ToLower(guideA) + `","axis":"vertical","position":1}]`, "重复"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.SetGuides(tc.json); err == nil {
				t.Fatalf("%s 应当失败", tc.name)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("错误 = %q，缺少 %q", err, tc.want)
			}
		})
	}

	// 失败不留下历史，也不改动文档。
	if activeSessionOf(t, svc).hist.UndoCount() != 0 {
		t.Fatal("失败的 SetGuides 不该产生历史项")
	}
	if activeSessionOf(t, svc).doc.GuidesList != nil {
		t.Fatal("失败的 SetGuides 不该改动参考线")
	}
}

func TestSetGuidesRejectsTooMany(t *testing.T) {
	svc, _ := newTestService(t, 40, 30)
	many := make([]domain.Guide, domain.MaxGuides+1)
	for i := range many {
		many[i] = domain.Guide{ID: newUUID(), Axis: domain.GuideAxisVertical, Position: float64(i)}
	}
	raw, err := json.Marshal(many)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetGuides(string(raw)); err == nil {
		t.Fatal("超过上限应当失败")
	} else if !strings.Contains(err.Error(), "上限") {
		t.Fatalf("错误 = %q，缺少「上限」", err)
	}
}

func TestSetGuidesWithoutDocument(t *testing.T) {
	svc := NewService(NewWorkspace())
	if _, err := svc.SetGuides("[]"); err != errNoDocument {
		t.Fatalf("无文档时错误 = %v，想要 %v", err, errNoDocument)
	}
}
