package bridge

// 票 70/71 与上游 DeepLayerTests 互证：
// `levelsDimsHighlightsInProportion` —— Levels 把白点拉到 127.5 时，HDR 高光按
// 比例减半，而不是被裁到白。

import (
	"math"
	"testing"

	"compositor-win/internal/domain"
)

func hdrLevelsPayload(t *testing.T, outputWhite float64) string {
	t.Helper()
	var ranges [4]domain.LevelRange
	for i := range ranges {
		ranges[i] = domain.IdentityLevelRange()
	}
	ranges[0].OutputWhite = outputWhite
	return payloadJSON(t, map[string]any{"adjustment": map[string]any{
		"kind": "Levels", "levels": domain.LevelsSettings{Ranges: ranges},
	}})
}

func TestLevelsDimsHDRHighlightsInProportion(t *testing.T) {
	svc, _ := newTestService(t, 8, 8)
	sess := activeSessionOf(t, svc)
	key := attachHDRField(t, sess, 4)
	before := sess.bitmaps[key].Deep.Peak()

	if _, err := svc.ApplyFilter("adjust:Levels", "", hdrLevelsPayload(t, 127.5), 7, ""); err != nil {
		t.Fatalf("色阶: %v", err)
	}
	after := sess.bitmaps[key]
	if after.Deep == nil || !after.Deep.HDR {
		t.Fatal("色阶后 HDR 图层不该退化成 8 位")
	}
	got := after.Deep.Peak()
	if math.Abs(float64(got-before/2)) > 0.02 {
		t.Fatalf("高光峰值 %g → %g，想要按比例减半到 %g", before, got, before/2)
	}
}
