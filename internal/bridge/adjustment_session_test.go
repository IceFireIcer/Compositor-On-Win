package bridge

// 图像菜单的（破坏式）调整会话回归。
//
// 真机验收实测：图像 ▸ 曲线… 走 beginFilter(kind, label, true) —— 只发 kind、
// adjustment 为 null，而 BeginFilterEdit 当时直接报"调整会话缺少 adjustment 参数"，
// 于是九条调整（曲线/色阶/色相饱和度/曝光/渐变映射/颗粒/添加杂色/黑白/色彩平衡）
// 一条都打不开。修法是 Go 侧按 kind 补 identity 默认值：菜单路径不需要知道
// 每种调整的完整参数表。

import (
	"bytes"
	"testing"

	"compositor-win/internal/domain"
)

const menuAdjustmentPayload = "{\"adjustment\":null}"

func TestAdjustmentSessionStartsWithIdentityDefaults(t *testing.T) {
	cases := []struct {
		kind  string
		check func(*testing.T, *domain.Adjustment)
	}{
		{"adjust:Curves", func(t *testing.T, a *domain.Adjustment) {
			if a.Kind != domain.AdjustmentCurves {
				t.Fatalf("kind = %q", a.Kind)
			}
			for i, ch := range a.Curves.Channels {
				if len(ch) < 2 {
					t.Fatalf("曲线通道 %d 没有默认锚点: %v（空通道会让 curveValue panic）", i, ch)
				}
				if ch[0] != (domain.CurvePoint{X: 0, Y: 0}) || ch[1] != (domain.CurvePoint{X: 255, Y: 255}) {
					t.Fatalf("曲线通道 %d 的默认锚点 = %v，想要 0→0 / 255→255", i, ch)
				}
			}
		}},
		{"adjust:Levels", func(t *testing.T, a *domain.Adjustment) {
			if a.Kind != domain.AdjustmentLevels {
				t.Fatalf("kind = %q", a.Kind)
			}
			want := domain.IdentityLevelRange()
			for i, r := range a.Levels.Ranges {
				if r != want {
					t.Fatalf("色阶通道 %d = %+v，想要 identity %+v（全零会被 normalize 成 white=1/gamma=0.1）", i, r, want)
				}
			}
		}},
		{"adjust:Hue/Saturation", func(t *testing.T, a *domain.Adjustment) {
			if a.Kind != domain.AdjustmentHueSaturation {
				t.Fatalf("kind = %q", a.Kind)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			svc, _ := newTestService(t, 16, 16)
			// 菜单路径的真实载荷：adjustment 为 null。
			if _, err := svc.BeginFilterEdit(c.kind, "", menuAdjustmentPayload, 3, ""); err != nil {
				t.Fatalf("开启调整会话失败: %v", err)
			}
			sess := activeSessionOf(t, svc)
			if sess.filter == nil {
				t.Fatal("会话没有建立")
			}
			if sess.filter.params.Adjustment == nil {
				t.Fatal("会话缺少 adjustment")
			}
			c.check(t, sess.filter.params.Adjustment)
			// 预览也要能跑（identity 不该报错）。
			if _, err := svc.UpdateFilterPreview(menuAdjustmentPayload); err != nil {
				t.Fatalf("identity 预览失败: %v", err)
			}
		})
	}
}

// 缺 per-kind 块时其余种类按"原样通过"渲染（Go 侧各自有 nil 守卫 /
// orDefault 助手），这里只钉住入口不再拒绝。
func TestAllImageMenuAdjustmentsOpenFromMenuPayload(t *testing.T) {
	for _, k := range domain.AllAdjustmentKinds {
		t.Run(string(k), func(t *testing.T) {
			svc, _ := newTestService(t, 16, 16)
			kind := FilterAdjustPrefix + string(k)
			if _, err := svc.BeginFilterEdit(kind, "", menuAdjustmentPayload, 1, ""); err != nil {
				t.Fatalf("BeginFilterEdit(%s): %v", kind, err)
			}
		})
	}
}

// 预览与提交路径同样要补默认值 —— 评审发现第一版修复只落在 BeginFilterEdit /
// ApplyFilter，而对话框自己发的载荷是"部分"的：色阶吸管只给 3 个 range
// （Go 的 [4]LevelRange 第 4 个是零值 → normalize 成 white=1/gamma=0.1 的过曝图），
// setCurvePoints 只给一个通道、其余三个空。
// 这个测试直接盯住"identity 色阶提交后像素不变"这条硬结论。
func TestAdjustmentPreviewAndCommitFillDefaults(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 16, 16, 128)
	before := firstLayerBitmap(t, sess).Clone()

	identity := `{"black":0,"gamma":1,"white":255,"outputBlack":0,"outputWhite":255}`
	// 对话框吸管的形状：只给 3 个通道。
	partial := `{"adjustment":{"kind":"Levels","levels":{"ranges":[` + identity + `,` + identity + `,` + identity + `]}}}`

	if _, err := svc.BeginFilterEdit("adjust:Levels", "", partial, 1, ""); err != nil {
		t.Fatalf("BeginFilterEdit: %v", err)
	}
	want := domain.IdentityLevelRange()
	s := activeSessionOf(t, svc)
	if s.filter == nil || s.filter.params.Adjustment == nil {
		t.Fatal("会话缺少 adjustment")
	}
	for i, r := range s.filter.params.Adjustment.Levels.Ranges {
		if r != want {
			t.Fatalf("开启时 ranges[%d] = %+v，想要 identity", i, r)
		}
	}

	if _, err := svc.UpdateFilterPreview(partial); err != nil {
		t.Fatalf("UpdateFilterPreview: %v", err)
	}
	if got := activeSessionOf(t, svc).filter.params.Adjustment.Levels.Ranges[3]; got != want {
		t.Fatalf("预览参数 ranges[3] = %+v，想要 identity（零值会过曝）", got)
	}

	if _, err := svc.CommitFilter(partial); err != nil {
		t.Fatalf("CommitFilter: %v", err)
	}
	after := firstLayerBitmap(t, activeSessionOf(t, svc))
	if !bytes.Equal(after.Pix, before.Pix) {
		t.Fatal("identity 色阶提交后像素被改了 —— 零值 range 过曝的典型症状")
	}
}

// Existing non-destructive adjustment layers have no image asset. Editing one
// must use the metadata session rather than the destructive pixel-filter path.
func TestAdjustmentLayerEditPreviewsAndCancelsWithoutPixels(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	sess := activeSessionOf(t, svc)
	layer := &sess.doc.Layers[0]
	layer.ImageFile = nil
	layer.Adjustment = &domain.Adjustment{
		Kind:   domain.AdjustmentLevels,
		Levels: domain.LevelsSettings{Ranges: [4]domain.LevelRange{domain.IdentityLevelRange(), domain.IdentityLevelRange(), domain.IdentityLevelRange(), domain.IdentityLevelRange()}},
	}

	if _, err := svc.BeginFilterEdit("adjust:Levels", layer.ID,
		`{"adjustment":{"kind":"Levels","levels":{"ranges":[{"black":0,"gamma":1,"white":255,"outputBlack":0,"outputWhite":255},{"black":0,"gamma":1,"white":255,"outputBlack":0,"outputWhite":255},{"black":0,"gamma":1,"white":255,"outputBlack":0,"outputWhite":255},{"black":0,"gamma":1,"white":255,"outputBlack":0,"outputWhite":255}]}}}`,
		1, ""); err != nil {
		t.Fatalf("adjustment layer edit should open: %v", err)
	}
	if _, err := svc.UpdateFilterPreview(`{"adjustment":{"kind":"Levels","levels":{"ranges":[{"black":32,"gamma":1,"white":255,"outputBlack":0,"outputWhite":255},{"black":0,"gamma":1,"white":255,"outputBlack":0,"outputWhite":255},{"black":0,"gamma":1,"white":255,"outputBlack":0,"outputWhite":255},{"black":0,"gamma":1,"white":255,"outputBlack":0,"outputWhite":255}]}}}`); err != nil {
		t.Fatalf("adjustment layer preview should update: %v", err)
	}
	if got := sess.doc.Layers[0].Adjustment.Levels.Ranges[0].Black; got != 32 {
		t.Fatalf("preview black = %v, want 32", got)
	}
	if _, err := svc.CancelFilterEdit(); err != nil {
		t.Fatalf("adjustment layer cancel: %v", err)
	}
	if got := sess.doc.Layers[0].Adjustment.Levels.Ranges[0].Black; got != 0 {
		t.Fatalf("cancel restored black = %v, want 0", got)
	}
}

// A menu-created adjustment is provisional until the editor is committed.
// Cancel must remove that layer rather than leave an identity adjustment that
// is still present in the document and undo stack.
func TestNewAdjustmentLayerCancelDiscardsLayerAndHistory(t *testing.T) {
	svc, _ := newTestService(t, 16, 16)
	before := mustDocumentSnapshot(t, svc)
	if before.Doc == nil || before.Doc.ActiveLayerID == nil {
		t.Fatal("addLayer did not return an active layer")
	}
	priorActive := *before.Doc.ActiveLayerID

	created := mustLayerOp(t, svc, "addAdjustment", payloadJSON(t, map[string]any{
		"kind":        string(domain.AdjustmentLevels),
		"provisional": true,
	}))
	if created.Doc == nil || len(created.Doc.Layers) != len(before.Doc.Layers)+1 {
		t.Fatalf("created document layers = %d, want %d", len(created.Doc.Layers), len(before.Doc.Layers)+1)
	}
	createdID := *created.Doc.ActiveLayerID
	if _, err := svc.BeginFilterEdit("adjust:Levels", createdID, menuAdjustmentPayload, 1, ""); err != nil {
		t.Fatalf("begin new adjustment edit: %v", err)
	}
	if _, err := svc.UpdateFilterPreview(`{"adjustment":{"kind":"Levels","levels":{"ranges":[{"black":32,"gamma":1,"white":255,"outputBlack":0,"outputWhite":255}]}}}`); err != nil {
		t.Fatalf("preview new adjustment edit: %v", err)
	}

	after := parseEnvelope(t, mustCancelFilter(t, svc))
	if after.Doc == nil || len(after.Doc.Layers) != len(before.Doc.Layers) {
		t.Fatalf("cancel left %d layers, want %d", len(after.Doc.Layers), len(before.Doc.Layers))
	}
	if after.Doc.ActiveLayerID == nil || *after.Doc.ActiveLayerID != priorActive {
		t.Fatalf("cancel active layer = %v, want %q", after.Doc.ActiveLayerID, priorActive)
	}
	for _, layer := range after.Doc.Layers {
		if layer.ID == createdID {
			t.Fatalf("cancel left provisional adjustment layer %q", createdID)
		}
	}
	if _, err := svc.Undo(); err == nil {
		t.Fatal("cancelled new adjustment should not leave a creation undo entry")
	}
}

func mustCancelFilter(t *testing.T, svc *Service) string {
	t.Helper()
	raw, err := svc.CancelFilterEdit()
	if err != nil {
		t.Fatalf("cancel filter edit: %v", err)
	}
	return raw
}
