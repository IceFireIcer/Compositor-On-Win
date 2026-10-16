package render

// Camera Raw 自适应滑杆读的画面统计量（票 78）：对比、高光、阴影各自按画面的一个
// 亮度度量取表，所以先把这三个度量算出来（CameraRawTables.swift:125-143）。
//
// 真值是 tests/golden/c/AdjustPixels.c 的 camera_raw_statistics（:340-378），逐行对应。

import "math"

// 这里用仓里既有的传递函数，不另抄一份：lut.go 的 srgbToLinear/linearToSrgb 就是
// AdjustPixels.c:215-223 的 srgb_decode/srgb_encode（常数与钳制都逐字相同），
// cameraraw.go:506 的 camRec709 就是 :225 的 rec709。

// cameraRawBrightness is CameraRawTables.Brightness: what the adaptive sliders
// read from the picture before any of them, as sRGB levels 0…255.
type cameraRawBrightness struct {
	brightest    float64
	luminance    float64
	logBrightest float64
}

// cameraRawStatistics is camera_raw_statistics (AdjustPixels.c:340-378): the
// mean of each pixel's brightest channel, the mean luminance, and the
// log-average of the brightest channel — all in linear light, then back to sRGB
// levels. A transparent pixel is skipped entirely, so clear areas neither
// brighten nor darken what the adaptive sliders see.
//
// The C accumulates into 32 row bands and sums the bands afterwards; the same
// structure is kept here rather than one running total, because the order of a
// floating-point sum is part of the answer when this is compared bit for bit.
func cameraRawStatistics(b *Bitmap) cameraRawBrightness {
	const bands = 32
	var sums [bands][4]float64
	if b == nil || b.W <= 0 || b.H <= 0 {
		return cameraRawBrightness{brightest: 0.5, luminance: 0.5, logBrightest: 0.5}
	}
	decoded := make([]float64, 256)
	for i := range decoded {
		decoded[i] = srgbToLinear(float64(i) / 255)
	}
	rows := (b.H + bands - 1) / bands
	for band := 0; band < bands; band++ {
		var brightest, luminance, logBrightest, weight float64
		end := (band + 1) * rows
		if end > b.H {
			end = b.H
		}
		for y := band * rows; y < end; y++ {
			for x := 0; x < b.W; x++ {
				p := (y*b.W + x) * 4
				alpha := float64(b.Pix[p+3])
				if alpha == 0 {
					continue
				}
				var r, g, bl float64
				if b.Pix[p+3] == 255 {
					r, g, bl = decoded[b.Pix[p]], decoded[b.Pix[p+1]], decoded[b.Pix[p+2]]
				} else {
					r = srgbToLinear(min(1.0, float64(b.Pix[p])/alpha))
					g = srgbToLinear(min(1.0, float64(b.Pix[p+1])/alpha))
					bl = srgbToLinear(min(1.0, float64(b.Pix[p+2])/alpha))
				}
				top := max(r, max(g, bl))
				w := alpha / 255
				brightest += w * top
				luminance += w * camRec709(r, g, bl)
				logBrightest += w * math.Log(top+3e-4)
				weight += w
			}
		}
		sums[band][0], sums[band][1], sums[band][2], sums[band][3] = brightest, luminance, logBrightest, weight
	}
	var brightest, luminance, logBrightest, weight float64
	for band := 0; band < bands; band++ {
		brightest += sums[band][0]
		luminance += sums[band][1]
		logBrightest += sums[band][2]
		weight += sums[band][3]
	}
	if weight <= 0 {
		return cameraRawBrightness{brightest: 0.5, luminance: 0.5, logBrightest: 0.5}
	}
	return cameraRawBrightness{
		brightest:    linearToSrgb(brightest / weight),
		luminance:    linearToSrgb(luminance / weight),
		logBrightest: linearToSrgb(math.Exp(logBrightest/weight) - 3e-4),
	}
}
