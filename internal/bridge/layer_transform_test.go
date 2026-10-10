package bridge

// 变换提交测试（票 17 的 Enter / "Apply current canvas operation"）：
// 语义对着 reference/Swift/Compositor/Document/EditorSession.swift:451-492
// commitTransform 写 —— 只把草稿写进图层的 transform，非破坏（像素与分辨率
// 一律不动），一步可撤销。

import (
	"strings"
	"testing"

	"compositor-win/internal/domain"
)

func TestApplyLayerTransformWritesOnlyThePlacement(t *testing.T) {
	svc, _ := newTestService(t, 40, 30)
	sess := activeSessionOf(t, svc)
	id := *sess.doc.ActiveLayerID
	before := mustLayerByID(t, sess.doc, id).Transform
	imageFile := derefString(mustLayerByID(t, sess.doc, id).ImageFile)
	bitmap := sess.bitmaps[imageFile]

	want := domain.Transform{
		Origin:   [2]float64{2, 3},
		Size:     [2]float64{10, 5},
		Rotation: 45,
		FlipX:    true,
		Sampling: domain.SamplingSmooth,
	}
	out, err := svc.ApplyLayerTransform(id, payloadJSON(t, want))
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvelope(t, out)
	if env.Rev != 1 {
		t.Fatalf("提交变换后 rev = %d，想要 1", env.Rev)
	}
	after := mustLayerByID(t, env.Doc, id)
	if after.Transform != want {
		t.Fatalf("图层变换 = %+v，想要 %+v", after.Transform, want)
	}
	// 非破坏：资产名、位图尺寸都没动。
	if after.ImageFile == nil || *after.ImageFile != imageFile {
		t.Fatalf("ImageFile 被改动了: %v", after.ImageFile)
	}
	if live := activeSessionOf(t, svc).bitmaps[imageFile]; live != bitmap || live.W != 40 || live.H != 30 {
		t.Fatalf("像素被改动了: %p %dx%d", live, live.W, live.H)
	}
	if got := activeSessionOf(t, svc).hist.UndoName(); got != "变换图层" {
		t.Fatalf("历史项名 = %q，想要「变换图层」", got)
	}

	if _, err := svc.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := mustLayerByID(t, activeSessionOf(t, svc).doc, id).Transform; got != before {
		t.Fatalf("撤销后变换 = %+v，想要 %+v", got, before)
	}
}

func TestApplyLayerTransformDefaultsToTheActiveLayer(t *testing.T) {
	svc, _ := newTestService(t, 20, 10)
	sess := activeSessionOf(t, svc)
	id := *sess.doc.ActiveLayerID

	if _, err := svc.ApplyLayerTransform("", payloadJSON(t, domain.Transform{
		Origin:   [2]float64{1, 1},
		Size:     [2]float64{8, 8},
		Sampling: domain.SamplingNearest,
	})); err != nil {
		t.Fatal(err)
	}
	if got := mustLayerByID(t, activeSessionOf(t, svc).doc, id).Transform.Origin; got != [2]float64{1, 1} {
		t.Fatalf("活动图层的变换原点 = %v，想要 [1 1]", got)
	}
}

func TestApplyLayerTransformRejectsBadRequests(t *testing.T) {
	svc, _ := newTestService(t, 20, 10)
	sess := activeSessionOf(t, svc)
	id := *sess.doc.ActiveLayerID
	before := mustLayerByID(t, sess.doc, id).Transform

	cases := []struct {
		name  string
		layer string
		json  string
		want  string
	}{
		{"未知图层", "nope", `{"origin":[0,0],"size":[4,4],"sampling":"Nearest"}`, "图层不存在"},
		{"尺寸为零", id, `{"origin":[0,0],"size":[0,4],"sampling":"Nearest"}`, "变换"},
		{"尺寸超限", id, `{"origin":[0,0],"size":[300001,4],"sampling":"Nearest"}`, "变换"},
		{"原点超限", id, `{"origin":[1000001,0],"size":[4,4],"sampling":"Nearest"}`, "变换"},
		{"采样拼写错", id, `{"origin":[0,0],"size":[4,4],"sampling":"Bogus"}`, "变换"},
		{"非法 JSON", id, "{oops", "变换"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.ApplyLayerTransform(tc.layer, tc.json); err == nil {
				t.Fatalf("%s 应当失败", tc.name)
			} else if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("错误 = %q，缺少 %q", err, tc.want)
			}
			if got := mustLayerByID(t, activeSessionOf(t, svc).doc, id).Transform; got != before {
				t.Fatalf("失败的提交改动了图层变换: %+v", got)
			}
		})
	}
}

func TestApplyLayerTransformWithoutDocument(t *testing.T) {
	svc := NewService(NewWorkspace())
	if _, err := svc.ApplyLayerTransform("", `{"origin":[0,0],"size":[4,4],"sampling":"Nearest"}`); err != errNoDocument {
		t.Fatalf("无文档时错误 = %v，想要 %v", err, errNoDocument)
	}
}

func TestApplyLayerTransformUnchangedIsNotAnUndoStep(t *testing.T) {
	svc, _ := newTestService(t, 20, 10)
	sess := activeSessionOf(t, svc)
	id := *sess.doc.ActiveLayerID
	current := mustLayerByID(t, sess.doc, id).Transform

	out, err := svc.ApplyLayerTransform(id, payloadJSON(t, current))
	if err != nil {
		t.Fatal(err)
	}
	if env := parseEnvelope(t, out); env.Rev != 0 {
		t.Fatalf("原样提交不该产生历史，rev = %d", env.Rev)
	}
}
