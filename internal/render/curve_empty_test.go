package render

// 空曲线通道的兜底：identity，而不是 make([]float64, -1) 的 panic。
// 可达路径：图像 ▸ 曲线… 的会话只带 kind；或 .comp 里存了空的 channels。
import (
	"math"
	"testing"

	"compositor-win/internal/domain"
)

func TestCurvesLUTEmptyChannelsAreIdentity(t *testing.T) {
	lut := BuildCurvesLUT(domain.CurvesSettings{}, 256)
	for c := 0; c < 3; c++ {
		for i, e := range lut.Tables[c] {
			if math.Abs(float64(e)-float64(i)/255) > 1e-6 {
				t.Fatalf("空通道 LUT[%d][%d] = %v，想要 %v", c, i, e, float64(i)/255)
			}
		}
	}
}

func TestCurvesLUTSingleAnchorChannelIsIdentity(t *testing.T) {
	s := domain.CurvesSettings{}
	s.Channels[0] = []domain.CurvePoint{{X: 128, Y: 128}}
	lut := BuildCurvesLUT(s, 256)
	for i, e := range lut.Tables[0] {
		if math.Abs(float64(e)-float64(i)/255) > 1e-6 {
			t.Fatalf("单锚点通道 LUT[%d] = %v，想要 %v", i, e, float64(i)/255)
		}
	}
}
