package render

// SpotHeal — the Go port of HealPixels.c (spot_heal): the healing brush's
// three modes (0 Content-Aware, 1 Create Texture, 2 Proximity Match) over a
// coverage mask, coverage×opacity blended. Verbatim C in tests/golden/c is
// the truth; the grain term crosses sqrt/log/cos, so its golden case runs
// at ε=1.

import "math"

const (
	healOutside = 0
	healRing    = 1
	healHole    = 2
)

// HealCoverageBounds ports heal_coverage_bounds: [left, top, right, bottom]
// of the nonzero coverage, all zero when empty.
func HealCoverageBounds(gray []uint8, width, height int) [4]int {
	x0, y0, x1, y1 := int64(width), int64(height), int64(0), int64(0)
	for y := 0; y < height; y++ {
		row := gray[y*width : y*width+width]
		for x := 0; x < width; x++ {
			if row[x] == 0 {
				continue
			}
			if int64(x) < x0 {
				x0 = int64(x)
			}
			if int64(x)+1 > x1 {
				x1 = int64(x) + 1
			}
			if int64(y) < y0 {
				y0 = int64(y)
			}
			if int64(y)+1 > y1 {
				y1 = int64(y) + 1
			}
		}
	}
	if x1 <= x0 || y1 <= y0 {
		x0, y0, x1, y1 = 0, 0, 0, 0
	}
	return [4]int{int(x0), int(y0), int(x1), int(y1)}
}

func healHash(x uint32) uint32 {
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return x
}

func healUnit(key uint32) float64 {
	return float64(healHash(key)>>8) / 16777216.0
}

// healScore is the mean squared difference between the ring around the spot
// and the ring around the patch offset by (dx, dy) — infinite when the
// patch would overlap the spot or leave the image.
func healScore(rgba []uint8, stride int64, role []byte, wx0, wy0, ww, wh, dx, dy, W, H int64) float64 {
	if abs64(dx) < ww && abs64(dy) < wh {
		return math.Inf(1)
	}
	if wx0+dx < 0 || wy0+dy < 0 || wx0+ww+dx > W || wy0+wh+dy > H {
		return math.Inf(1)
	}
	sum := 0.0
	n := int64(0)
	for y := int64(0); y < wh; y++ {
		for x := int64(0); x < ww; x++ {
			if role[y*ww+x] != healRing {
				continue
			}
			t := rgba[(wy0+y)*stride+(wx0+x)*4:]
			s := rgba[(wy0+y+dy)*stride+(wx0+x+dx)*4:]
			for c := 0; c < 4; c++ {
				d := float64(t[c]) - float64(s[c])
				sum += d * d
			}
			n++
		}
	}
	if n == 0 {
		return math.Inf(1)
	}
	return sum / float64(n)
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// healSolve relaxes smooth values over HOLE pixels, fixed to the RING values
// around them. A coarser copy is solved first and used as the starting
// point, so large spots settle in few passes.
func healSolve(value []float32, role []byte, w, h int64, depth int) {
	iterations := 300
	if w > 32 && h > 32 && depth < 16 {
		cw, ch := (w+1)/2, (h+1)/2
		coarse := make([]float32, cw*ch*4)
		coarseRole := make([]byte, cw*ch)
		for y := int64(0); y < ch; y++ {
			for x := int64(0); x < cw; x++ {
				known, hole := 0, 0
				var knownSum, holeSum [4]float32
				for j := int64(0); j < 2; j++ {
					for i := int64(0); i < 2; i++ {
						fx, fy := x*2+i, y*2+j
						if fx >= w || fy >= h {
							continue
						}
						p := fy*w + fx
						if role[p] == healRing {
							known++
							for c := 0; c < 4; c++ {
								knownSum[c] += value[int(p*4)+c]
							}
						} else if role[p] == healHole {
							hole++
							for c := 0; c < 4; c++ {
								holeSum[c] += value[int(p*4)+c]
							}
						}
					}
				}
				q := y*cw + x
				if known > 0 {
					coarseRole[q] = healRing
					for c := 0; c < 4; c++ {
						coarse[int(q*4)+c] = knownSum[c] / float32(known)
					}
				} else if hole > 0 {
					coarseRole[q] = healHole
					for c := 0; c < 4; c++ {
						coarse[int(q*4)+c] = holeSum[c] / float32(hole)
					}
				}
			}
		}
		healSolve(coarse, coarseRole, cw, ch, depth+1)
		for y := int64(0); y < h; y++ {
			for x := int64(0); x < w; x++ {
				p := y*w + x
				q := (y/2)*cw + x/2
				if role[p] == healHole && coarseRole[q] == healHole {
					copy(value[int(p*4):int(p*4)+4], coarse[int(q*4):int(q*4)+4])
				}
			}
		}
		iterations = 40
	}
	const omega = float32(1.8)
	for it := 0; it < iterations; it++ {
		for y := int64(0); y < h; y++ {
			for x := int64(0); x < w; x++ {
				p := y*w + x
				if role[p] != healHole {
					continue
				}
				var sum [4]float32
				n := 0
				neighbors := [4][2]int64{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}}
				for k := 0; k < 4; k++ {
					nx, ny := neighbors[k][0], neighbors[k][1]
					if nx < 0 || ny < 0 || nx >= w || ny >= h {
						continue
					}
					q := ny*w + nx
					if role[q] == healOutside {
						continue
					}
					for c := 0; c < 4; c++ {
						sum[c] += value[int(q*4)+c]
					}
					n++
				}
				if n == 0 {
					continue
				}
				for c := 0; c < 4; c++ {
					value[int(p*4)+c] += omega * (sum[c]/float32(n) - value[int(p*4)+c])
				}
			}
		}
	}
}

// SpotHeal ports spot_heal. coverage is width×height gray; opacity 0–1.
// Returns false when the coverage is empty or no ring pixels exist.
func SpotHeal(b *Bitmap, coverage []uint8, opacity float32, mode int, seed uint32) bool {
	W, H := int64(b.W), int64(b.H)
	stride := W * 4
	rgba := b.Pix
	bounds := HealCoverageBounds(coverage, int(W), int(H))
	if bounds[2] <= bounds[0] {
		return false
	}
	bx0, by0, bx1, by1 := int64(bounds[0]), int64(bounds[1]), int64(bounds[2]), int64(bounds[3])
	bw, bh := bx1-bx0, by1-by0
	size := bw
	if bh > size {
		size = bh
	}
	ring := size / 8
	if ring < 2 {
		ring = 2
	}
	if ring > 16 {
		ring = 16
	}
	// Work box: the spot plus its ring, clipped to the image.
	wx0 := max64(bx0-ring, 0)
	wy0 := max64(by0-ring, 0)
	wx1 := min64(bx1+ring, W)
	wy1 := min64(by1+ring, H)
	ww, wh := wx1-wx0, wy1-wy0
	wn := ww * wh

	role := make([]byte, wn)
	near := make([]byte, wn)
	value := make([]float32, wn*4)
	for y := int64(0); y < wh; y++ {
		for x := int64(0); x < ww; x++ {
			role[y*ww+x] = boolByte(coverage[int((wy0+y)*W+(wx0+x))] != 0) * healHole
		}
	}
	// The ring: pixels within `ring` of the spot (a square dilation, row
	// pass then column pass).
	prefix := make([]int64, max64(ww, wh)+1)
	for y := int64(0); y < wh; y++ {
		prefix[0] = 0
		for x := int64(0); x < ww; x++ {
			prefix[x+1] = prefix[x] + int64(boolToInt64(role[y*ww+x] == healHole))
		}
		for x := int64(0); x < ww; x++ {
			lo := max64(x-ring, 0)
			hi := min64(x+ring+1, ww)
			near[y*ww+x] = boolByte(prefix[hi]-prefix[lo] > 0)
		}
	}
	for x := int64(0); x < ww; x++ {
		prefix[0] = 0
		for y := int64(0); y < wh; y++ {
			prefix[y+1] = prefix[y] + int64(boolToInt64(near[y*ww+x] != 0))
		}
		for y := int64(0); y < wh; y++ {
			lo := max64(y-ring, 0)
			hi := min64(y+ring+1, wh)
			if role[y*ww+x] == healOutside && prefix[hi]-prefix[lo] > 0 {
				role[y*ww+x] = healRing
			}
		}
	}
	ringCount := int64(0)
	for p := int64(0); p < wn; p++ {
		if role[p] == healRing {
			ringCount++
		}
	}
	if ringCount == 0 {
		return false
	}

	// Source patch for Content-Aware and Proximity Match.
	ox, oy := int64(0), int64(0)
	haveSource := false
	if mode != 1 {
		factors := [5]float64{1.05, 1.35, 1.75, 2.25, 2.8}
		count := 5
		if mode == 2 {
			count = 2
		}
		best := math.Inf(1)
		for f := 0; f < count; f++ {
			for a := 0; a < 24; a++ {
				angle := float64(a) * math.Pi / 12.0
				dx := round64(math.Cos(angle) * factors[f] * float64(ww))
				dy := round64(math.Sin(angle) * factors[f] * float64(wh))
				score := healScore(rgba, stride, role, wx0, wy0, ww, wh, dx, dy, W, H)
				if math.IsInf(score, 1) {
					continue
				}
				if mode == 2 {
					score *= 1.0 + 0.6*float64(f) // nearer patches win ties
				} else {
					score *= 1.0 + 0.1*float64(f)
				}
				if score < best {
					best = score
					ox, oy = dx, dy
				}
			}
		}
		if !math.IsInf(best, 1) {
			// Fine-tune the alignment so repeating texture lines up.
			cx, cy := ox, oy
			refined := healScore(rgba, stride, role, wx0, wy0, ww, wh, cx, cy, W, H)
			for j := int64(-3); j <= 3; j++ {
				for i := int64(-3); i <= 3; i++ {
					score := healScore(rgba, stride, role, wx0, wy0, ww, wh, cx+i, cy+j, W, H)
					if score < refined {
						refined = score
						ox, oy = cx+i, cy+j
					}
				}
			}
			haveSource = true
		}
	}

	// Membrane: the edge difference between the original and the patch (or
	// the original itself for a smooth fill), spread across the spot.
	var mean [4]float64
	var detail [3]float64
	for y := int64(0); y < wh; y++ {
		for x := int64(0); x < ww; x++ {
			p := y*ww + x
			if role[p] != healRing {
				for c := 0; c < 4; c++ {
					value[int(p*4)+c] = 0
				}
				continue
			}
			ix, iy := wx0+x, wy0+y
			t := rgba[iy*stride+ix*4:]
			var s []uint8
			if haveSource {
				s = rgba[(iy+oy)*stride+(ix+ox)*4:]
			}
			for c := 0; c < 4; c++ {
				if s != nil {
					value[int(p*4)+c] = float64ToFloat32(float64(t[c]) - float64(s[c]))
				} else {
					value[int(p*4)+c] = float64ToFloat32(float64(t[c]))
				}
				mean[c] += float64(value[int(p*4)+c])
			}
			if !haveSource {
				// Fine detail around the spot: each pixel against the average
				// of its neighbours.
				for c := 0; c < 3; c++ {
					around := 0.0
					n := 0
					offsets := [4][2]int64{{ix - 1, iy}, {ix + 1, iy}, {ix, iy - 1}, {ix, iy + 1}}
					for k := 0; k < 4; k++ {
						if offsets[k][0] < 0 || offsets[k][1] < 0 || offsets[k][0] >= W || offsets[k][1] >= H {
							continue
						}
						around += float64(rgba[int(offsets[k][1]*stride+offsets[k][0]*4)+c])
						n++
					}
					if n > 0 {
						d := float64(t[c]) - around/float64(n)
						detail[c] += d * d
					}
				}
			}
		}
	}
	for c := 0; c < 4; c++ {
		mean[c] /= float64(ringCount)
	}
	for p := int64(0); p < wn; p++ {
		if role[p] == healHole {
			for c := 0; c < 4; c++ {
				value[int(p*4)+c] = float64ToFloat32(mean[c])
			}
		}
	}
	healSolve(value, role, ww, wh, 0)
	for c := 0; c < 3; c++ {
		detail[c] = math.Sqrt(detail[c]/float64(ringCount)) * 0.9
	}

	for y := int64(0); y < wh; y++ {
		for x := int64(0); x < ww; x++ {
			p := y*ww + x
			if role[p] != healHole {
				continue
			}
			ix, iy := wx0+x, wy0+y
			t := rgba[iy*stride+ix*4:]
			var s []uint8
			if haveSource {
				s = rgba[(iy+oy)*stride+(ix+ox)*4:]
			}
			amount := float64(coverage[int(iy*W+ix)]) / 255.0 * float64(opacity)
			grain := 0.0
			if !haveSource {
				key := healHash(seed ^ healHash(uint32(iy*W+ix)))
				u1 := healUnit(key)
				u2 := healUnit(key ^ 0x68e31da4)
				grain = math.Sqrt(-2.0*math.Log(1.0-u1)) * math.Cos(2.0*math.Pi*u2)
			}
			var out [4]float64
			for c := 0; c < 4; c++ {
				// C promotion order: the patch term adds in float, the grain
				// detail joins in double after it.
				source := float32(0)
				if s != nil {
					source = float32(s[c])
				}
				healed := float64(source + value[int(p*4)+c])
				if c < 3 {
					healed += grain * detail[c]
				}
				out[c] = float64(t[c]) + (healed-float64(t[c]))*amount
			}
			alpha := out[3]
			if alpha < 0 {
				alpha = 0
			}
			if alpha > 255 {
				alpha = 255
			}
			t[3] = uint8(round64(alpha))
			for c := 0; c < 3; c++ {
				v := out[c]
				if v < 0 {
					v = 0
				}
				if v > float64(t[3]) {
					v = float64(t[3])
				}
				t[c] = uint8(round64(v))
			}
		}
	}
	return true
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func round64(v float64) int64 {
	return int64(math.Round(v))
}

func boolToInt64(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

// float64ToFloat32 mirrors the C's implicit double→float stores in the
// membrane setup (value[] is float in the C too).
func float64ToFloat32(v float64) float32 { return float32(v) }
