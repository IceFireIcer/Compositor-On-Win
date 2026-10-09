package bridge

// Ticket 45 tests: per-run text styles on the TextCommit wire endpoint.
// Spans travel in UTF-16 units of the request's content (the frozen wire
// contract), every violation is rejected without touching the document, and
// "no overrides" — omitted or [] — is stored as nil runs.

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"compositor-win/internal/domain"
)

// textCommitPayload builds a minimal valid TextCommit payload. layerID is
// omitted when empty; the run snippets are appended verbatim, and an empty
// snippet leaves the field out of the JSON entirely.
func textCommitPayload(layerID, content, colorRuns, fontRuns string) string {
	b := `{"content":"` + content + `","fontName":"Segoe UI","fontSize":32,` +
		`"red":0,"green":0,"blue":0,"alignment":"Left","tracking":0,"leading":0,` +
		`"anchor":[20,80]`
	if layerID != "" {
		b += `,"layerId":"` + layerID + `"`
	}
	if colorRuns != "" {
		b += `,"colorRuns":` + colorRuns
	}
	if fontRuns != "" {
		b += `,"fontRuns":` + fontRuns
	}
	return b + "}"
}

func TestTextCommitCarriesRunsInUTF16Spans(t *testing.T) {
	svc, _ := newTestService(t, 240, 160)
	// "a😀bc" is five UTF-16 units: a=0, the surrogate pair=1–2, b=3, c=4.
	out := mustTextCommit(t, svc, textCommitPayload("", "a😀bc",
		`[{"location":1,"length":2,"red":1,"green":0,"blue":0}]`,
		`[{"location":3,"length":2,"fontName":"Arial"}]`))
	if !strings.Contains(out, `"colorRuns":[{"location":1,"length":2,"red":1,"green":0,"blue":0}]`) {
		t.Fatalf("快照缺少按 UTF-16 单元的颜色范围：%s", out)
	}
	if !strings.Contains(out, `"fontRuns":[{"location":3,"length":2,"fontName":"Arial"}]`) {
		t.Fatalf("快照缺少按 UTF-16 单元的字体范围：%s", out)
	}
	env := parseJSON[envelope](t, out)
	style := env.Doc.Layers[1].Text
	if style == nil {
		t.Fatal("文字图层缺少 TextStyle")
	}
	if want := []domain.TextColorRun{{Location: 1, Length: 2, Red: 1}}; !reflect.DeepEqual(style.ColorRuns, want) {
		t.Fatalf("colorRuns = %+v，想要 %+v", style.ColorRuns, want)
	}
	if want := []domain.TextFontRun{{Location: 3, Length: 2, FontName: "Arial"}}; !reflect.DeepEqual(style.FontRuns, want) {
		t.Fatalf("fontRuns = %+v，想要 %+v", style.FontRuns, want)
	}
}

func TestTextCommitAcceptsSpanInsideSurrogatePair(t *testing.T) {
	svc, _ := newTestService(t, 160, 120)
	// Spans are unit ranges, not rune ranges: the second half of the emoji's
	// surrogate pair (location 2, one unit) is a legal — if typographically
	// odd — request and must not be second-guessed here.
	out := mustTextCommit(t, svc, textCommitPayload("", "a😀b",
		`[{"location":2,"length":1,"red":0,"green":0,"blue":1}]`, ""))
	env := parseJSON[envelope](t, out)
	if style := env.Doc.Layers[1].Text; style == nil || len(style.ColorRuns) != 1 ||
		style.ColorRuns[0].Location != 2 || style.ColorRuns[0].Length != 1 {
		t.Fatalf("半个代理对范围应原样保留：%s", out)
	}
}

func TestTextCommitNoRunsStoresNil(t *testing.T) {
	cases := []struct {
		name      string
		colorRuns string
		fontRuns  string
	}{
		{"字段省略", "", ""},
		{"空数组", "[]", "[]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, _ := newTestService(t, 120, 120)
			out := mustTextCommit(t, svc, textCommitPayload("", "Hello", c.colorRuns, c.fontRuns))
			if strings.Contains(out, "colorRuns") || strings.Contains(out, "fontRuns") {
				t.Fatalf("无覆盖时快照不应出现 runs 键：%s", out)
			}
			env := parseJSON[envelope](t, out)
			if style := env.Doc.Layers[1].Text; style == nil || style.ColorRuns != nil || style.FontRuns != nil {
				t.Fatalf("Text 元数据应携带 nil runs：%+v", style)
			}
		})
	}
}

func TestTextCommitRejectsInvalidRuns(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		wantSub string
	}{
		{"颜色范围重叠",
			textCommitPayload("", "Hello", `[{"location":0,"length":3,"red":1},{"location":2,"length":2,"red":1}]`, ""),
			"范围重叠"},
		{"颜色范围未按序",
			textCommitPayload("", "Hello", `[{"location":3,"length":1,"red":1},{"location":0,"length":1,"red":1}]`, ""),
			"范围重叠"},
		{"颜色范围超出文本",
			textCommitPayload("", "Hi", `[{"location":1,"length":9,"red":1}]`, ""),
			"超出文本范围"},
		{"颜色范围零长度",
			textCommitPayload("", "Hi", `[{"location":0,"length":0,"red":1}]`, ""),
			"长度必须为正"},
		{"颜色大于 1",
			textCommitPayload("", "Hi", `[{"location":0,"length":1,"red":1.5}]`, ""),
			"颜色超出 0–1"},
		{"颜色小于 0",
			textCommitPayload("", "Hi", `[{"location":0,"length":1,"blue":-0.5}]`, ""),
			"颜色超出 0–1"},
		{"字体范围重叠",
			textCommitPayload("", "Hello", "", `[{"location":0,"length":4,"fontName":"Arial"},{"location":3,"length":1,"fontName":"Arial"}]`),
			"范围重叠"},
		{"字体范围超出文本",
			textCommitPayload("", "Hi", "", `[{"location":1,"length":9,"fontName":"Arial"}]`),
			"超出文本范围"},
		{"字体名为空",
			textCommitPayload("", "Hello", "", `[{"location":0,"length":2,"fontName":""}]`),
			"字体名无效"},
		{"字体名含换行",
			textCommitPayload("", "Hello", "", `[{"location":0,"length":2,"fontName":"Ari\nal"}]`),
			"字体名无效"},
		{"字体名超过 200 字符",
			textCommitPayload("", "Hello", "", `[{"location":0,"length":2,"fontName":"`+strings.Repeat("F", 201)+`"}]`),
			"字体名无效"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, _ := newTestService(t, 120, 120)
			before := mustDocumentSnapshot(t, svc)
			_, err := svc.TextCommit(c.payload)
			if err == nil {
				t.Fatalf("应拒绝：%s", c.payload)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Fatalf("错误 = %q，缺少 %q", err, c.wantSub)
			}
			after := mustDocumentSnapshot(t, svc)
			if len(after.Doc.Layers) != len(before.Doc.Layers) || after.Rev != before.Rev {
				t.Fatalf("被拒的提交不得改动文档：图层 %d→%d，rev %d→%d",
					len(before.Doc.Layers), len(after.Doc.Layers), before.Rev, after.Rev)
			}
		})
	}
}

func TestTextStyleFromRequestRejectsNaNRunColor(t *testing.T) {
	// JSON cannot spell NaN, so this reaches textStyleFromRequest directly;
	// the shared predicate must reject it all the same.
	req := textCommitRequest{
		Content:   "Hello",
		FontName:  "Segoe UI",
		FontSize:  24,
		ColorRuns: []domain.TextColorRun{{Location: 0, Length: 1, Red: math.NaN()}},
	}
	if _, _, err := textStyleFromRequest(req); err == nil || !strings.Contains(err.Error(), "颜色超出 0–1") {
		t.Fatalf("NaN 颜色应被拒绝，得到 %v", err)
	}
}

func TestTextCommitRejectsStaleRunsAfterShortening(t *testing.T) {
	svc, _ := newTestService(t, 240, 120)
	first := parseJSON[envelope](t, mustTextCommit(t, svc, textCommitPayload("", "Hello world",
		`[{"location":0,"length":5,"red":1}]`, "")))
	layerID := first.Doc.Layers[1].ID
	before := mustDocumentSnapshot(t, svc)

	// The frontend opened the editor on the long text and deleted down to
	// "Hi" but still sends the old span — the commit must be rejected, not
	// stored with a run that no longer fits.
	_, err := svc.TextCommit(textCommitPayload(layerID, "Hi", `[{"location":0,"length":5,"red":1}]`, ""))
	if err == nil || !strings.Contains(err.Error(), "超出文本范围") {
		t.Fatalf("缩短内容后沿用旧范围应被拒绝，得到 %v", err)
	}
	after := mustDocumentSnapshot(t, svc)
	if after.Rev != before.Rev || len(after.Doc.Layers) != len(before.Doc.Layers) {
		t.Fatalf("被拒的编辑不得改动文档：rev %d→%d，图层 %d→%d",
			before.Rev, after.Rev, len(before.Doc.Layers), len(after.Doc.Layers))
	}
	// The stored style keeps its original runs untouched.
	if style := after.Doc.Layers[1].Text; style == nil || style.Content != "Hello world" ||
		!reflect.DeepEqual(style.ColorRuns, []domain.TextColorRun{{Location: 0, Length: 5, Red: 1}}) {
		t.Fatalf("被拒的编辑不得改动既有样式：%+v", style)
	}

	// An edit that carries spans matching the new content goes through.
	env := parseJSON[envelope](t, mustTextCommit(t, svc, textCommitPayload(layerID, "Hi",
		`[{"location":0,"length":2,"red":1}]`, "")))
	if style := env.Doc.Layers[1].Text; style == nil || style.Content != "Hi" ||
		!reflect.DeepEqual(style.ColorRuns, []domain.TextColorRun{{Location: 0, Length: 2, Red: 1}}) {
		t.Fatalf("缩短后的合法范围应落层：%+v", env.Doc.Layers[1].Text)
	}
}

// TestTextCommitRejectsRunsBeyondEmptyContent pins the empty-content edge:
// there is no UTF-16 unit to carry a run, so any span is out of range.
func TestTextCommitRejectsRunsBeyondEmptyContent(t *testing.T) {
	svc, _ := newTestService(t, 120, 120)
	before := mustDocumentSnapshot(t, svc)
	_, err := svc.TextCommit(textCommitPayload("", "", `[{"location":0,"length":1,"red":1}]`, ""))
	if err == nil || !strings.Contains(err.Error(), "超出文本范围") {
		t.Fatalf("空内容上的范围应被拒绝，得到 %v", err)
	}
	after := mustDocumentSnapshot(t, svc)
	if after.Rev != before.Rev || len(after.Doc.Layers) != len(before.Doc.Layers) {
		t.Fatal("被拒的提交不得改动文档")
	}
}
