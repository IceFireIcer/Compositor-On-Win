package bridge

// 票 73：上游 ProgressiveBlurTests.theLineStartsOnTheLayerAndMoves —— 打开滤镜时线
// 就落在图层上（靠近可见区域顶部到其底边），端点可以移动，提交后图层深度不变。

import (
	"encoding/json"
	"math"
	"testing"
)

func TestProgressiveBlurOpensWithItsLineOnTheLayer(t *testing.T) {
	svc, _ := newTestService(t, 200, 100)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 200, 100, 128)

	if _, err := svc.BeginFilterEdit(FilterProgressive, "", `{}`, 1, ""); err != nil {
		t.Fatalf("打开渐进模糊: %v", err)
	}
	start, end := sess.filter.params.ProgressiveStart, sess.filter.params.ProgressiveEnd
	if start == nil || end == nil {
		t.Fatal("打开时线就该落在图层上")
	}
	// 200×100 的图层铺满画布：x 取中，y 从 35% 到 100%（上游同款断言）。
	if math.Abs(start[0]-100) > 0.001 || math.Abs(start[1]-35) > 0.001 {
		t.Fatalf("起点 = %v，想要 [100 35]", *start)
	}
	if math.Abs(end[0]-100) > 0.001 || math.Abs(end[1]-100) > 0.001 {
		t.Fatalf("终点 = %v，想要 [100 100]", *end)
	}

	// 拖到别处：更新预览后会话里的端点跟着走。
	moved, err := json.Marshal(map[string]any{
		"progressiveFrom": 0, "progressiveTo": 6,
		"progressiveStart": []float64{100, 35}, "progressiveEnd": []float64{180, 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateFilterPreview(string(moved)); err != nil {
		t.Fatalf("更新预览: %v", err)
	}
	if got := sess.filter.params.ProgressiveEnd; got == nil || math.Abs((*got)[0]-180) > 0.001 {
		t.Fatalf("拖动后终点 = %v，想要 [180 50]", got)
	}
	if _, err := svc.CommitFilter(string(moved)); err != nil {
		t.Fatalf("提交: %v", err)
	}
	if sess.filter != nil {
		t.Fatal("提交后不该还有打开的滤镜会话")
	}
}

// 端点是文档坐标：缩放或平移画布不该让线离开它贴着的那处图像。
func TestProgressiveLineIsInDocumentCoordinates(t *testing.T) {
	svc, _ := newTestService(t, 200, 100)
	sess := activeSessionOf(t, svc)
	paintRect(sess, 0, 0, 200, 100, 128)
	// 图层缩到画布中间的一小块：默认线取这一块，而不是整幅画布。
	sess.doc.Layers[0].Transform.Origin = [2]float64{50, 20}
	sess.doc.Layers[0].Transform.Size = [2]float64{100, 60}

	if _, err := svc.BeginFilterEdit(FilterProgressive, "", `{}`, 1, ""); err != nil {
		t.Fatalf("打开渐进模糊: %v", err)
	}
	start := sess.filter.params.ProgressiveStart
	end := sess.filter.params.ProgressiveEnd
	if start == nil || end == nil {
		t.Fatal("缺少默认线")
	}
	// 可见区域 x 50–150、y 20–80：中 x = 100，起点 y = 20 + 60×0.35 = 41，终点 y = 80。
	if math.Abs((*start)[0]-100) > 0.001 || math.Abs((*start)[1]-41) > 0.001 {
		t.Fatalf("起点 = %v，想要 [100 41]", *start)
	}
	if math.Abs((*end)[1]-80) > 0.001 {
		t.Fatalf("终点 = %v，想要 y=80", *end)
	}
}
