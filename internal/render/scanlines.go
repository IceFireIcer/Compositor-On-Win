package render

// Scanlines — the Go port of Scanlines.swift's ScanlinesSettings.apply and
// DitherPixels.c's scanlines_apply / draw_lines: the independent Filter ›
// Scanlines kernel (ticket 60), split out of the Dither styles. The verbatim
// C in tests/golden/c is the truth (provenance in tests/golden/c/README.md).
// The C parallelizes over disjoint row/column bands (dispatch_apply); the Go
// port runs the same code serially — the bands never share writes, so the
// output is byte-identical. The kernel crosses sinf/fmodf/sqrtf/powf/exp2f
// (ε=1 goldens, recorded in the case notes).

import "math"

// ScanlinesParams mirrors the C struct (DitherPixels.h).
type ScanlinesParams struct {
	// 相邻两条扫描线之间隔多少像素。
	Spacing int
	// 满亮的线填满行距的多少，0–1；调子越暗，线画得越细。
	Thickness float32
	// 0–1：暗于该值的调子把线拆成圆点，经短划过渡到实线。
	Dots float32
	// 每行左右摇摆的像素数，沿画面向下成一道波。
	Wobble float32
	// 画面亮处把线抬起（负值则下沉）的像素数，线因此拱进画面的形状里。
	Displace float32
	// 0–1：暗于该值的调子完全不画线，屏幕留黑。
	Threshold float32
	// 红蓝各向一边移开的像素数，给线的边缘加彩色镶边。
	Split float32
	// −1…1：先压暗或提亮，再变平或变冲。
	Density  float32
	Contrast float32
	// 0–1：画面全黑处线还留多亮，像 CRT 的亮度旋钮托起黑位。
	BlackLevel float32
	// 0–1：亮度先沿线和跨行平滑多远，才拿去抬线。
	Smoothness float32
	// 0：dark 上的 light 线（直出 sRGB）。1：线取画面自己的颜色。
	OriginalColors bool
	Dark           [3]uint8
	Light          [3]uint8
}

// scanlinesIndex clamps an index into [0, width) — the C code's
// `x < 0 ? 0 : x >= width ? width - 1 : x` edge clamp.
func scanlinesIndex(v int64, width int) int {
	if v < 0 {
		return 0
	}
	if v >= int64(width) {
		return width - 1
	}
	return int(v)
}

// ScanlinesApply ports scanlines_apply: draws premultiplied RGBA pixels as
// scanlines in place, keeping alpha and leaving fully transparent pixels
// alone. Returns false if working memory couldn't be had (Go allocations
// either succeed or panic, so it mirrors the C return contract only).
func ScanlinesApply(b *Bitmap, p *ScanlinesParams) bool {
	width, height := b.W, b.H
	count := width * height
	if count == 0 {
		return true
	}
	spacing := p.Spacing
	if spacing < 2 {
		spacing = 2
	}
	lines := (height + spacing - 1) / spacing
	planes := 1
	if p.OriginalColors {
		planes = 3
	}
	tone := make([]float32, count*planes)
	alpha := make([]uint8, count)
	// 每行沿线方向的调子：它盖住那几行的平均，取样位置随行的摇摆移动。
	scan := make([]float32, lines*width*planes)

	// Density 以 gamma 压暗（正）或提亮，黑白两端不动；contrast 以中灰为轴。
	gamma := float32(math.Exp2(float64(p.Density) * 1.5))
	contrast := float32(1)
	if p.Contrast >= 0 {
		contrast = 1 / (1 - 0.95*p.Contrast)
	} else {
		contrast = 1 + p.Contrast
	}
	original := p.OriginalColors
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			px := b.Pix[(y*width+x)*4:]
			at := y*width + x
			alpha[at] = px[3]
			var r, g, bl float32
			if px[3] != 0 {
				scale := 1 / float32(px[3])
				r = float32(px[0]) * scale
				g = float32(px[1]) * scale
				bl = float32(px[2]) * scale
			}
			if original {
				tone[at] = ditherAdjustTone(r, gamma, contrast)
				tone[count+at] = ditherAdjustTone(g, gamma, contrast)
				tone[2*count+at] = ditherAdjustTone(bl, gamma, contrast)
			} else {
				tone[at] = ditherAdjustTone(0.2126*r+0.7152*g+0.0722*bl, gamma, contrast)
			}
		}
	}
	wobble := p.Wobble
	for line := 0; line < lines; line++ {
		top := line * spacing
		bottom := top + spacing
		if bottom > height {
			bottom = height
		}
		// 一道慢波叠一道快波，像 CRT 同步漂移时画面在摇。
		wave := float32(math.Sin(float64(line)*0.45))*0.7 + float32(math.Sin(float64(line)*1.7+1.3))*0.3
		shift := int64(math.Round(float64(wobble * wave)))
		for x := 0; x < width; x++ {
			var sum [3]float32
			n := 0
			sx := int64(x) - shift
			if sx >= 0 && sx < int64(width) {
				for y := top; y < bottom; y++ {
					at := y*width + int(sx)
					if alpha[at] == 0 {
						continue
					}
					for c := 0; c < planes; c++ {
						sum[c] += tone[c*count+at]
					}
					n++
				}
			}
			for c := 0; c < planes; c++ {
				if n > 0 {
					scan[(c*lines+line)*width+x] = sum[c] / float32(n)
				} else {
					scan[(c*lines+line)*width+x] = 0
				}
			}
		}
	}

	if !scanlinesDrawLines(b, p, scan, alpha, lines, spacing) {
		return false
	}

	// 色彩分离：红往一边、蓝往另一边，线的边缘因此出现彩色镶边。
	split := int64(math.Round(float64(p.Split)))
	if split > 0 {
		copyRow := make([]uint8, width*4)
		for y := 0; y < height; y++ {
			row := b.Pix[y*width*4:]
			copy(copyRow, row)
			for x := 0; x < width; x++ {
				from := int64(x) - split
				to := int64(x) + split
				a := row[x*4+3]
				var r, bl uint8
				if from >= 0 {
					r = copyRow[from*4]
				}
				if to < int64(width) {
					bl = copyRow[to*4+2]
				}
				if r > a {
					r = a
				}
				if bl > a {
					bl = a
				}
				row[x*4] = r
				row[x*4+2] = bl
			}
		}
	}
	return true
}

// scanlinesDrawLines ports draw_lines: 一列一列地把线画在扫出的调子上。
// 位移之后它是从正面看的一片地形（Rutt-Etra 视频合成器的做法）：每条线被
// 画面亮度平滑成的山丘抬起（或往另一边压低），并遮住它后面的东西，于是近处
// 的线包在形状之上，而不是穿过后面的线。最近的一条先画：线抬升时是最下面
// 那条，下沉时是最上面那条。
func scanlinesDrawLines(b *Bitmap, p *ScanlinesParams, scan []float32, alpha []uint8, lines, spacing int) bool {
	width, height := b.W, b.H
	original := p.OriginalColors
	plane := lines * width
	tone := make([]float32, plane)
	lift := make([]float32, plane)
	for at := 0; at < plane; at++ {
		if original {
			tone[at] = 0.2126*scan[at] + 0.7152*scan[plane+at] + 0.0722*scan[2*plane+at]
		} else {
			tone[at] = scan[at]
		}
	}
	// 高度：亮度沿每条线模糊（三遍方框，接近高斯），再跨相邻行平滑，于是一张
	// 脸整体抬起成圆丘，而不是它下面像素的阶梯。
	smoothness := ditherClamp01(p.Smoothness)
	radius := int64(math.Round(float64(smoothness * float32(spacing) * 2)))
	copy(lift, tone)
	if radius > 0 && p.Displace != 0 {
		source := make([]float32, width)
		for line := 0; line < lines; line++ {
			row := lift[line*width : (line+1)*width]
			for pass := 0; pass < 3; pass++ {
				copy(source, row)
				sum := float32(0)
				for x := -radius; x <= radius; x++ {
					sum += source[scanlinesIndex(x, width)]
				}
				for x := 0; x < width; x++ {
					row[x] = sum / float32(2*radius+1)
					out := int64(x) - radius
					in := int64(x) + radius + 1
					sum += source[scanlinesIndex(in, width)] - source[scanlinesIndex(out, width)]
				}
			}
		}
		across := int64(math.Round(float64(smoothness * 2)))
		spread := make([]float32, plane)
		for step := int64(0); step < across; step++ {
			for line := 0; line < lines; line++ {
				above, below := line, line
				if line > 0 {
					above = line - 1
				}
				if line+1 < lines {
					below = line + 1
				}
				a := lift[above*width:]
				h := lift[line*width:]
				bl := lift[below*width:]
				for x := 0; x < width; x++ {
					spread[line*width+x] = (a[x] + 2*h[x] + bl[x]) / 4
				}
			}
			copy(lift, spread)
		}
	}

	middle := float32(spacing) / 2
	dots := ditherClamp01(p.Dots)
	threshold := ditherClamp01(p.Threshold)
	displace := p.Displace
	thickness := ditherClamp01(p.Thickness)
	blackLevel := ditherClamp01(p.BlackLevel)
	rising := displace >= 0
	screen := [3]float32{float32(p.Dark[0]) / 255, float32(p.Dark[1]) / 255, float32(p.Dark[2]) / 255}
	phosphor := [3]float32{float32(p.Light[0]) / 255, float32(p.Light[1]) / 255, float32(p.Light[2]) / 255}
	if original {
		screen[0], screen[1], screen[2] = 0, 0, 0
	}
	cover := make([]float32, height)
	color := make([]float32, height*3)
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			cover[y] = 0
		}
		along := float32(math.Mod(float64(x)+0.5, float64(spacing))) - middle
		// 这一列已经画过的东西的边缘；更靠后的线只在它之外露出。
		horizon := float32(math.Inf(1))
		if !rising {
			horizon = float32(math.Inf(-1))
		}
		for step := 0; step < lines; step++ {
			line := step
			if rising {
				line = lines - 1 - step
			}
			// 珠点：暗于 Dots 值的线断成一颗颗珠，每行距一颗，各自取它中点的
			// 调子；刚过该值则断成短划，再亮就闭合成实线。
			beading := float32(0)
			if dots > 0 {
				beading = ditherClamp01((dots - tone[line*width+x]) / 0.2)
			}
			across := along * beading
			centered := int64(math.Round(float64(float32(x) - across)))
			at := scanlinesIndex(centered, width)
			i := line*width + at
			t := tone[i]
			lit := float32(1)
			if threshold > 0 {
				lit = ditherClamp01((t - threshold) / 0.04)
			}
			if lit <= 0 {
				continue
			}
			// 在本列这个高度与上一列的高度之间画，陡坡也不会把线画断。
			base := float32(line)*float32(spacing) + middle
			here := base - displace*lift[i]
			before := here
			if at > 0 {
				before = base - displace*lift[i-1]
			}
			lo := float32(math.Min(float64(here), float64(before)))
			hi := float32(math.Max(float64(here), float64(before)))
			// 黑位：线的最低亮度，画面全黑处它也还看得见。
			level := blackLevel + (1-blackLevel)*t
			var c [3]float32
			for k := 0; k < 3; k++ {
				if original {
					c[k] = blackLevel + (1-blackLevel)*scan[k*plane+i]
				} else {
					c[k] = screen[k] + (phosphor[k]-screen[k])*level
				}
			}
			// 线高的一半：画面暗的地方更细。
			beam := middle * thickness * (0.29 + 0.71*float32(math.Sqrt(float64(ditherClamp01(level)))))
			var top, bottom int64
			if rising {
				top = int64(math.Floor(float64(lo - beam - 1)))
				bottom = int64(math.Ceil(float64(math.Min(float64(hi+beam+1), float64(horizon+1)))))
			} else {
				top = int64(math.Floor(float64(math.Max(float64(lo-beam-1), float64(horizon-1)))))
				bottom = int64(math.Ceil(float64(hi + beam + 1)))
			}
			if top < 0 {
				top = 0
			}
			if bottom > int64(height) {
				bottom = int64(height)
			}
			for y := top; y < bottom; y++ {
				yy := float32(y) + 0.5
				off := float32(0)
				if yy < lo {
					off = lo - yy
				} else if yy > hi {
					off = yy - hi
				}
				distance := float32(math.Sqrt(float64(off*off + across*across)))
				hidden := float32(0)
				if rising {
					hidden = ditherClamp01(horizon - yy + 0.5)
				} else {
					hidden = ditherClamp01(yy - horizon + 0.5)
				}
				shown := ditherClamp01(beam-distance+0.5) * lit * hidden
				if shown > cover[y] {
					cover[y] = shown
					color[y*3] = c[0]
					color[y*3+1] = c[1]
					color[y*3+2] = c[2]
				}
			}
			if lit > 0.5 {
				if rising {
					horizon = float32(math.Min(float64(horizon), float64(lo-beam)))
				} else {
					horizon = float32(math.Max(float64(horizon), float64(hi+beam)))
				}
			}
		}
		for y := 0; y < height; y++ {
			if alpha[y*width+x] == 0 {
				continue
			}
			// 电子束比画面本身更亮，补上黑屏幕吃掉的那部分。
			shown := cover[y]
			ditherWritePixel(b.Pix[(y*width+x)*4:],
				screen[0]+(color[y*3]*1.35-screen[0])*shown,
				screen[1]+(color[y*3+1]*1.35-screen[1])*shown,
				screen[2]+(color[y*3+2]*1.35-screen[2])*shown)
		}
	}
	return true
}

// ScanlinesSettings is Scanlines.swift's filter form: the Filter panel's
// sliders, with its ranges and defaults.
type ScanlinesSettings struct {
	// 线条间距，图层像素。
	LineSpacing float64 `json:"lineSpacing"`
	// 满亮的线填满行距的百分比，5–100。
	Thickness float64 `json:"thickness"`
	// 0–100%：线周围的光晕。
	Glow float64 `json:"glow"`
	// 0–100%：暗于该值的调子把线拆成圆点。
	Dots float64 `json:"dots"`
	// 线左右摇摆的像素数。
	Wobble float64 `json:"wobble"`
	// 画面亮处把线抬起（负值下沉）的像素数。
	Displace float64 `json:"displace"`
	// 0–100%：抬线前亮度被平滑多远。
	Smoothness float64 `json:"smoothness"`
	// 0–100%：暗于该值的调子不画线。
	Threshold float64 `json:"threshold"`
	// 红蓝分开的像素数。
	Split float64 `json:"split"`
	// −100…100：先压暗/提亮、变平/变冲，再画线。
	Density  float64 `json:"density"`
	Contrast float64 `json:"contrast"`
	// 0–100%：画面全黑处线还留多亮。
	BlackLevel float64    `json:"blackLevel"`
	Colors     int        `json:"colors"`
	Dark       [3]float64 `json:"dark"`
	Light      [3]float64 `json:"light"`
}

// DefaultScanlinesSettings is Scanlines.swift's ScanlinesSettings(): the
// Filter panel's starting values. The zero value is NOT the default — every
// range's fallback only applies to NaN/Inf, so a partial payload clamps from
// zero (the same shape DitherSettings has).
func DefaultScanlinesSettings() ScanlinesSettings {
	return ScanlinesSettings{
		LineSpacing: 4, Thickness: 70, Glow: 0, Dots: 0, Wobble: 0, Displace: 0,
		Smoothness: 50, Threshold: 0, Split: 0, Density: 0, Contrast: 0,
		BlackLevel: 0, Colors: DitherColorsBlackWhite,
		Dark: [3]float64{0, 0, 0}, Light: [3]float64{1, 1, 1},
	}
}

// Normalized clamps with the Scanlines.swift ranges and defaults.
func (s ScanlinesSettings) Normalized() ScanlinesSettings {
	r := s
	r.LineSpacing = math.Round(ditherClampRange(s.LineSpacing, 2, 32, 4))
	r.Thickness = ditherClampRange(s.Thickness, 5, 100, 70)
	r.Glow = ditherClampRange(s.Glow, 0, 100, 0)
	r.Dots = ditherClampRange(s.Dots, 0, 100, 0)
	r.Wobble = ditherClampRange(s.Wobble, 0, 64, 0)
	r.Displace = ditherClampRange(s.Displace, -100, 100, 0)
	r.Smoothness = ditherClampRange(s.Smoothness, 0, 100, 50)
	r.Threshold = ditherClampRange(s.Threshold, 0, 100, 0)
	r.Split = math.Round(ditherClampRange(s.Split, 0, 16, 0))
	r.Density = ditherClampRange(s.Density, -100, 100, 0)
	r.Contrast = ditherClampRange(s.Contrast, -100, 100, 0)
	r.BlackLevel = ditherClampRange(s.BlackLevel, 0, 100, 0)
	for i := 0; i < 3; i++ {
		r.Dark[i] = min(1, max(0, s.Dark[i]))
		r.Light[i] = min(1, max(0, s.Light[i]))
	}
	return r
}

// ApplyScanlinesFilter is ScanlinesSettings.apply: the kernel draws the
// lines, then the phosphor glow is blurred across a few line spacings and
// added back over them.
func ApplyScanlinesFilter(b *Bitmap, settings ScanlinesSettings) {
	s := settings.Normalized()
	var dark, light [3]uint8
	if s.Colors == DitherColorsTwo {
		for i := 0; i < 3; i++ {
			dark[i] = uint8(min(255, max(0, s.Dark[i]*255+0.5)))
			light[i] = uint8(min(255, max(0, s.Light[i]*255+0.5)))
		}
	} else {
		dark = [3]uint8{0, 0, 0}
		light = [3]uint8{255, 255, 255}
	}
	params := ScanlinesParams{
		Spacing:        int(s.LineSpacing),
		Thickness:      float32(s.Thickness / 100),
		Dots:           float32(s.Dots / 100),
		Wobble:         float32(s.Wobble),
		Displace:       float32(s.Displace),
		Threshold:      float32(s.Threshold / 100),
		Split:          float32(s.Split),
		Density:        float32(s.Density / 100),
		Contrast:       float32(s.Contrast / 100),
		BlackLevel:     float32(s.BlackLevel / 100),
		Smoothness:     float32(s.Smoothness / 100),
		OriginalColors: s.Colors == DitherColorsOriginal,
		Dark:           dark,
		Light:          light,
	}
	if !ScanlinesApply(b, &params) {
		return
	}
	if s.Glow > 0 {
		// 线的光：跨几个行距模糊后加回去——CRT 荧光粉的辉光。原版在缩小到
		// 1/shrink 的图上做高斯再放大；这里沿用本仓 Dither 的同一近似（整幅
		// 高斯），保持两条滤镜的辉光观感一致。
		sigma := s.LineSpacing*3 + 3
		bloom := b.Clone()
		ApplyGaussianBlur(bloom, sigma)
		DitherGlow(b, bloom, float32(s.Glow/100*2.5))
	}
}
