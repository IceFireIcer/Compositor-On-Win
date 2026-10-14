package render

// PatchFill — the Go port of PatchPixels.c (patch_fill): multi-scale
// PatchMatch content-aware fill (Wexler, Shechtman & Irani 2007; Barnes et al.
// 2009). Every pixel whose 7 × 7 patch touches the hole is a target. Each
// target keeps the source patch (one lying wholly on usable image) that it
// best resembles, found by PatchMatch: a match is passed along to the next
// pixel, shifted by one, then improved by trying patches at random, nearer and
// nearer. Each hole pixel then becomes the blend of what every target patch
// covering it says it should be, closer matches counting for more, and the
// matching runs again against that. Done first on a small copy, so the hole's
// overall structure (where an edge runs through it) settles before its detail,
// then on each copy twice the size, starting from the smaller one's matches.
//
// Patches are compared on texture as well as color (Newson et al. 2014): how
// much edge there is around each pixel, across and down. Texture is measured
// once at full size and averaged down for the smaller copies rather than
// measured again on each (shrunk, busy texture blurs to a plain mid-tone,
// which would match the plainest parts of the image and fill the hole flat).
//
// The C splits the rows into 16 bands and runs them at once; a band reads
// another band's matches only from the snapshot taken at the start of the
// pass, so running the bands in order 0…15 reproduces the same bytes. This
// port does exactly that, deriving each band's RNG state the same way.

import "math"

const (
	// Patch radius (7 × 7), and the channels a pixel carries: color (RGBA)
	// and its texture across and down.
	patchRadius   = 3
	patchChannels = 6
	// How much texture counts against color.
	patchTextureWeight = float32(6.0)
	// How strongly the last blend favors the best matches (see vote).
	patchSharpFinish = float32(8.0)
	// Deepest pyramid the fill builds.
	patchMaxLevels = 12
)

// patchInfinity stands in for the C's FLT_MAX: a match that could not beat the
// best so far, or a patch with no counting pixels.
const patchInfinity = float32(math.MaxFloat32)

// patchLevel is one rung of the pyramid (the C's Level). Each pixel carries
// patchChannels floats: RGBA, then the texture across and down.
type patchLevel struct {
	w, h        int
	color       []float32 // RGBA 0–255, premultiplied; the hole holds its current estimate
	hole        []uint8   // to fill
	known       []uint8   // opaque and outside the hole: matched against and copied from
	source      []uint8   // a patch centered here lies wholly on known pixels
	target      []uint8   // a patch centered here touches the hole
	counts      []uint8   // per target: pixels of its patch that count (known or hole)
	match       []int32   // per target: the source center its patch matches
	distance    []float32 // per target: the mean squared difference to that match
	sources     []int32   // every source center, for random picks
	sourceCount int
}

func newPatchLevel(w, h int) *patchLevel {
	n := w * h
	return &patchLevel{
		w:        w,
		h:        h,
		color:    make([]float32, n*patchChannels),
		hole:     make([]uint8, n),
		known:    make([]uint8, n),
		source:   make([]uint8, n),
		target:   make([]uint8, n),
		counts:   make([]uint8, n),
		match:    make([]int32, n),
		distance: make([]float32, n),
		sources:  make([]int32, n),
	}
}

// patchRowBands runs body over rows split into bands, all at once in the C.
// Sixteen bands of equal size, or one when there are fewer than 32 rows. The
// bands run in order 0…bands-1; a band's body must not depend on another
// band's writes except through the caller's snapshot.
func patchRowBands(rows int, body func(band, start, end int)) {
	bands := 1
	if rows >= 32 {
		bands = 16
	}
	size := (rows + bands - 1) / bands
	for index := 0; index < bands; index++ {
		start := index * size
		end := start + size
		if end > rows {
			end = rows
		}
		if start < end {
			body(index, start, end)
		}
	}
}

// patchRandom is the C's next_random xorshift: a zero state would be a fixed
// point, so it falls back to the golden-ratio constant.
func patchRandom(state *uint32) uint32 {
	x := *state
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	if x == 0 {
		x = 0x9e3779b9
	}
	*state = x
	return x
}

// patchWindowSum sums over the window [x0, x1) × [y0, y1) of a summed-area
// table with a zero first row and column.
func patchWindowSum(sat []int64, w, x0, y0, x1, y1 int) int64 {
	stride := w + 1
	return sat[y1*stride+x1] - sat[y0*stride+x1] - sat[y1*stride+x0] + sat[y0*stride+x0]
}

// classify finds the sources, targets and the pixel counts behind each
// target's distance, from hole and known (the C's level_classify).
func (l *patchLevel) classify() {
	w, h, stride := l.w, l.h, l.w+1
	knownSAT := make([]int64, stride*(h+1))
	holeSAT := make([]int64, stride*(h+1))
	for y := 0; y < h; y++ {
		var knownRow, holeRow int64
		for x := 0; x < w; x++ {
			knownRow += int64(l.known[y*w+x])
			holeRow += int64(l.hole[y*w+x])
			knownSAT[(y+1)*stride+x+1] = knownSAT[y*stride+x+1] + knownRow
			holeSAT[(y+1)*stride+x+1] = holeSAT[y*stride+x+1] + holeRow
		}
	}
	l.sourceCount = 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := y*w + x
			x0, y0 := maxInt(x-patchRadius, 0), maxInt(y-patchRadius, 0)
			x1, y1 := minInt(x+patchRadius+1, w), minInt(y+patchRadius+1, h)
			holes := patchWindowSum(holeSAT, w, x0, y0, x1, y1)
			l.target[p] = 0
			if holes > 0 {
				l.target[p] = 1
			}
			l.counts[p] = uint8(holes + patchWindowSum(knownSAT, w, x0, y0, x1, y1))
			inside := x >= patchRadius && y >= patchRadius && x+patchRadius < w && y+patchRadius < h
			l.source[p] = 0
			if inside && patchWindowSum(knownSAT, w, x-patchRadius, y-patchRadius,
				x+patchRadius+1, y+patchRadius+1) == (2*patchRadius+1)*(2*patchRadius+1) {
				l.source[p] = 1
			}
			if l.source[p] != 0 {
				l.sources[l.sourceCount] = int32(p)
				l.sourceCount++
			}
		}
	}
}

// distanceTo is the mean squared difference between the target patch at p and
// the source patch at q, over the target's pixels that count. It gives up,
// returning patchInfinity, once it can't beat best.
func (l *patchLevel) distanceTo(p, q int, best float32) float32 {
	w, h := l.w, l.h
	px, py := p%w, p/w
	qx, qy := q%w, q/w
	limit := best
	if best != patchInfinity {
		limit = best * float32(l.counts[p])
	}
	sum := float32(0)
	for dy := -patchRadius; dy <= patchRadius; dy++ {
		y := py + dy
		if y < 0 || y >= h {
			continue
		}
		row := (qy + dy) * w * patchChannels
		for dx := -patchRadius; dx <= patchRadius; dx++ {
			x := px + dx
			a := y*w + x
			if x < 0 || x >= w || (l.hole[a] == 0 && l.known[a] == 0) {
				continue
			}
			t := a * patchChannels
			s := row + (qx+dx)*patchChannels
			for c := 0; c < patchChannels; c++ {
				d := l.color[t+c] - l.color[s+c]
				sum += d * d
			}
		}
		if sum >= limit {
			return patchInfinity
		}
	}
	if l.counts[p] != 0 {
		return sum / float32(l.counts[p])
	}
	return patchInfinity
}

// matchPass is one PatchMatch sweep over every target, forward or backward.
// Each band of rows works on its own; a neighbor in another band is read from
// where the sweep started (before), so no two bands touch the same match.
func (l *patchLevel) matchPass(forward bool, seed uint32, before []int32) {
	w, h := l.w, l.h
	step := 1
	if !forward {
		step = -1
	}
	reach := w
	if h > reach {
		reach = h
	}
	patchRowBands(h, func(band, start, end int) {
		state := seed ^ uint32(band)*0x9e3779b9 ^ 0x85ebca6b
		for i := 0; i < end-start; i++ {
			y := start + i
			if !forward {
				y = end - 1 - i
			}
			for j := 0; j < w; j++ {
				x := j
				if !forward {
					x = w - 1 - j
				}
				p := y*w + x
				if l.target[p] == 0 {
					continue
				}
				best := int(l.match[p])
				bestDistance := l.distance[p]
				// What the previous pixel along the row and the previous row
				// matched, moved along by one.
				nx, ny := x-step, y-step
				if nx >= 0 && nx < w && l.target[y*w+nx] != 0 {
					m := int(l.match[y*w+nx])
					c := m + step
					if m%w+step >= 0 && m%w+step < w && l.source[c] != 0 && c != best {
						if d := l.distanceTo(p, c, bestDistance); d < bestDistance {
							best, bestDistance = c, d
						}
					}
				}
				if ny >= 0 && ny < h && l.target[ny*w+x] != 0 {
					var m int
					if ny >= start && ny < end {
						m = int(l.match[ny*w+x])
					} else {
						m = int(before[ny*w+x])
					}
					c := m + step*w
					if m/w+step >= 0 && m/w+step < h && l.source[c] != 0 && c != best {
						if d := l.distanceTo(p, c, bestDistance); d < bestDistance {
							best, bestDistance = c, d
						}
					}
				}
				// Then patches at random around the best so far, nearer each time.
				for r := reach; r >= 1; r /= 2 {
					bx, by := best%w, best/w
					cx := bx + int(patchRandom(&state)%uint32(2*r+1)) - r
					cy := by + int(patchRandom(&state)%uint32(2*r+1)) - r
					if cx < 0 || cy < 0 || cx >= w || cy >= h {
						continue
					}
					c := cy*w + cx
					if l.source[c] == 0 || c == best {
						continue
					}
					if d := l.distanceTo(p, c, bestDistance); d < bestDistance {
						best, bestDistance = c, d
					}
				}
				l.match[p] = int32(best)
				l.distance[p] = bestDistance
			}
		}
	})
}

// patchExp is expf: the C computes the blend weights in float.
func patchExp(v float32) float32 { return float32(math.Exp(float64(v))) }

// vote turns each hole pixel into the blend of what every target patch
// covering it says it is, a closer match counting for more, by sharpness: 1
// while the matches settle; more for the last blend, where the best matches
// all but decide alone, as averaging patches that disagree (in busy texture,
// broken glass) smears it. Sources lie wholly outside the hole, so the hole
// can be rewritten as it's read.
func (l *patchLevel) vote(sharpness float32) {
	w, h := l.w, l.h
	total := 0.0
	count := int64(0)
	for p := 0; p < w*h; p++ {
		if l.target[p] != 0 && l.distance[p] < patchInfinity {
			total += float64(l.distance[p])
			count++
		}
	}
	mean := 1.0
	if count != 0 {
		mean = total / float64(count)
	}
	spread := (2.0*float32(mean) + 1.0) / sharpness
	patchRowBands(h, func(band, start, end int) {
		for y := start; y < end; y++ {
			for x := 0; x < w; x++ {
				a := y*w + x
				if l.hole[a] == 0 {
					continue
				}
				var sum [patchChannels]float32
				weights := float32(0)
				for dy := -patchRadius; dy <= patchRadius; dy++ {
					py := y - dy
					if py < 0 || py >= h {
						continue
					}
					for dx := -patchRadius; dx <= patchRadius; dx++ {
						px := x - dx
						p := py*w + px
						if px < 0 || px >= w || l.target[p] == 0 || l.distance[p] == patchInfinity {
							continue
						}
						q := int(l.match[p])
						s := ((q/w+dy)*w + (q%w + dx)) * patchChannels
						weight := patchExp(-l.distance[p] / spread)
						for c := 0; c < patchChannels; c++ {
							sum[c] += l.color[s+c] * weight
						}
						weights += weight
					}
				}
				if weights > 0 {
					for c := 0; c < patchChannels; c++ {
						l.color[a*patchChannels+c] = sum[c] / weights
					}
				}
			}
		}
	})
}

// predict is what the matches say the pixel at (x, y) should be, blended as
// vote blends them.
func (l *patchLevel) predict(x, y int, spread float32) ([4]float32, bool) {
	w, h := l.w, l.h
	var sum [4]float32
	weights := float32(0)
	for dy := -patchRadius; dy <= patchRadius; dy++ {
		py := y - dy
		if py < 0 || py >= h {
			continue
		}
		for dx := -patchRadius; dx <= patchRadius; dx++ {
			px := x - dx
			p := py*w + px
			if px < 0 || px >= w || l.target[p] == 0 || l.distance[p] == patchInfinity {
				continue
			}
			q := int(l.match[p])
			s := ((q/w+dy)*w + (q%w + dx)) * patchChannels
			weight := patchExp(-l.distance[p] / spread)
			for c := 0; c < 4; c++ {
				sum[c] += l.color[s+c] * weight
			}
			weights += weight
		}
	}
	if weights <= 0 {
		return sum, false
	}
	for c := 0; c < 4; c++ {
		sum[c] /= weights
	}
	return sum, true
}

// adaptColor matches the hole to its surroundings' tone, as the healing brush
// does: copied texture can come from somewhere a little lighter or darker
// (further along a sky's gradient, say), which leaves a visible rim. Along the
// hole's edge, the difference between the image and what the matches predict
// there is spread smoothly across the hole and added.
func (l *patchLevel) adaptColor() {
	w, h, n := l.w, l.h, l.w*l.h
	total := 0.0
	count := int64(0)
	for p := 0; p < n; p++ {
		if l.target[p] != 0 && l.distance[p] < patchInfinity {
			total += float64(l.distance[p])
			count++
		}
	}
	mean := 1.0
	if count != 0 {
		mean = total / float64(count)
	}
	spread := 2.0*float32(mean) + 1.0
	// healOutside elsewhere, healRing on the edge (fixed), healHole in the
	// hole (solved).
	role := make([]uint8, n)
	offset := make([]float32, n*4)
	var meanOffset [4]float64
	edges := int64(0)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := y*w + x
			if l.hole[p] != 0 {
				role[p] = healHole
				continue
			}
			if l.known[p] == 0 {
				continue
			}
			touches := false
			for j := -1; j <= 1 && !touches; j++ {
				for i := -1; i <= 1; i++ {
					qx, qy := x+i, y+j
					if qx >= 0 && qy >= 0 && qx < w && qy < h && l.hole[qy*w+qx] != 0 {
						touches = true
						break
					}
				}
			}
			predicted, ok := l.predict(x, y, spread)
			if !touches || !ok {
				continue
			}
			role[p] = healRing
			for c := 0; c < 4; c++ {
				offset[p*4+c] = l.color[p*patchChannels+c] - predicted[c]
				meanOffset[c] += float64(offset[p*4+c])
			}
			edges++
		}
	}
	if edges == 0 {
		return
	}
	for p := 0; p < n; p++ {
		if role[p] == healHole {
			for c := 0; c < 4; c++ {
				offset[p*4+c] = float32(meanOffset[c] / float64(edges))
			}
		}
	}
	HealMembrane(offset, role, int64(w), int64(h), 0)
	for p := 0; p < n; p++ {
		if role[p] == healHole {
			for c := 0; c < 4; c++ {
				l.color[p*patchChannels+c] += offset[p*4+c]
			}
		}
	}
}

// measure recomputes the distances for every target's current match.
func (l *patchLevel) measure() {
	w := l.w
	patchRowBands(l.h, func(band, start, end int) {
		for p := start * w; p < end*w; p++ {
			if l.target[p] != 0 {
				l.distance[p] = l.distanceTo(p, int(l.match[p]), patchInfinity)
			}
		}
	})
}

// settle runs the matching and blending, iterations times over.
func (l *patchLevel) settle(iterations int, seed uint32) {
	before := make([]int32, l.w*l.h)
	for i := 0; i < iterations; i++ {
		copy(before, l.match)
		l.matchPass(i%2 == 0, seed+uint32(i)*0x632be5ab, before)
		l.vote(1)
		l.measure()
	}
}

// measureTexture gives each known pixel its texture: how sharply its
// brightness changes across and down, averaged over the 5 × 5 around it, in
// the two channels after its color. Hole pixels get theirs from what they copy
// (they stay zero).
func (l *patchLevel) measureTexture() {
	w, h, stride := l.w, l.h, l.w+1
	edge := make([]float32, w*h*2)
	sat := make([]float64, stride*(h+1)*3)
	luma := func(q int) float32 {
		return (l.color[q*patchChannels] + l.color[q*patchChannels+1] + l.color[q*patchChannels+2]) / 3
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := y*w + x
			if l.known[p] == 0 {
				continue
			}
			if x > 0 && x+1 < w && l.known[p-1] != 0 && l.known[p+1] != 0 {
				edge[p*2] = patchAbs(luma(p+1)-luma(p-1)) / 2
			}
			if y > 0 && y+1 < h && l.known[p-w] != 0 && l.known[p+w] != 0 {
				edge[p*2+1] = patchAbs(luma(p+w)-luma(p-w)) / 2
			}
		}
	}
	// Summed tables of the two edges and of how many known pixels there are,
	// for 5 × 5 averages over known pixels.
	for y := 0; y < h; y++ {
		var row [3]float64
		for x := 0; x < w; x++ {
			p := y*w + x
			row[0] += float64(edge[p*2])
			row[1] += float64(edge[p*2+1])
			row[2] += float64(l.known[p])
			for k := 0; k < 3; k++ {
				sat[((y+1)*stride+x+1)*3+k] = sat[(y*stride+x+1)*3+k] + row[k]
			}
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := y*w + x
			if l.known[p] == 0 {
				continue
			}
			x0, y0 := maxInt(x-2, 0), maxInt(y-2, 0)
			x1, y1 := minInt(x+3, w), minInt(y+3, h)
			var v [3]float64
			for k := 0; k < 3; k++ {
				v[k] = sat[(y1*stride+x1)*3+k] - sat[(y0*stride+x1)*3+k] -
					sat[(y1*stride+x0)*3+k] + sat[(y0*stride+x0)*3+k]
			}
			l.color[p*patchChannels+4] = float32(v[0]/v[2]) * patchTextureWeight
			l.color[p*patchChannels+5] = float32(v[1]/v[2]) * patchTextureWeight
		}
	}
}

// downsample makes a copy half the size: a pixel is hole if any of its four
// is, known if all of them are. Texture stays as measured at full size,
// averaged down, rather than measured again here: shrunk, busy texture blurs
// to a plain mid-tone, which would match the plainest parts of the image and
// fill the hole flat.
func (l *patchLevel) downsample() *patchLevel {
	w, h := (l.w+1)/2, (l.h+1)/2
	c := newPatchLevel(w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			present, known, hole := 0, 0, uint8(0)
			var all, sure [patchChannels]float32
			for j := 0; j < 2; j++ {
				for i := 0; i < 2; i++ {
					fx, fy := 2*x+i, 2*y+j
					if fx >= l.w || fy >= l.h {
						continue
					}
					f := fy*l.w + fx
					present++
					known += int(l.known[f])
					hole |= l.hole[f]
					for ch := 0; ch < patchChannels; ch++ {
						all[ch] += l.color[f*patchChannels+ch]
						if l.known[f] != 0 {
							sure[ch] += l.color[f*patchChannels+ch]
						}
					}
				}
			}
			p := y*w + x
			c.hole[p] = hole
			if hole == 0 && known == present {
				c.known[p] = 1
			}
			for ch := 0; ch < patchChannels; ch++ {
				if known != 0 {
					c.color[p*patchChannels+ch] = sure[ch] / float32(known)
				} else {
					c.color[p*patchChannels+ch] = all[ch] / float32(present)
				}
			}
		}
	}
	c.classify()
	return c
}

// fillInward is the coarsest hole's first guess: filled in from its edge
// inward, each pixel the average of the filled neighbors around it, and
// whatever no edge reaches the average of the known image.
func (l *patchLevel) fillInward() {
	w, h, n := l.w, l.h, l.w*l.h
	done := make([]uint8, n)
	next := make([]uint8, n)
	var mean [patchChannels]float64
	known := int64(0)
	for p := 0; p < n; p++ {
		done[p] = l.known[p]
		if l.known[p] != 0 {
			known++
			for c := 0; c < patchChannels; c++ {
				mean[c] += float64(l.color[p*patchChannels+c])
			}
		}
	}
	for changed := true; changed; {
		changed = false
		copy(next, done)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				p := y*w + x
				if l.hole[p] == 0 || done[p] != 0 {
					continue
				}
				var sum [patchChannels]float32
				around := 0
				for j := -1; j <= 1; j++ {
					for i := -1; i <= 1; i++ {
						qx, qy := x+i, y+j
						if (i != 0 || j != 0) && qx >= 0 && qy >= 0 && qx < w && qy < h && done[qy*w+qx] != 0 {
							for c := 0; c < patchChannels; c++ {
								sum[c] += l.color[(qy*w+qx)*patchChannels+c]
							}
							around++
						}
					}
				}
				if around == 0 {
					continue
				}
				for c := 0; c < patchChannels; c++ {
					l.color[p*patchChannels+c] = sum[c] / float32(around)
				}
				next[p] = 1
				changed = true
			}
		}
		copy(done, next)
	}
	for p := 0; p < n; p++ {
		if l.hole[p] != 0 && done[p] == 0 {
			for c := 0; c < patchChannels; c++ {
				if known != 0 {
					l.color[p*patchChannels+c] = float32(mean[c] / float64(known))
				} else {
					l.color[p*patchChannels+c] = 0
				}
			}
		}
	}
}

// matchRandomly starts every target off on a random source.
func (l *patchLevel) matchRandomly(seed uint32) {
	state := seed | 1
	for p := 0; p < l.w*l.h; p++ {
		if l.target[p] != 0 {
			l.match[p] = l.sources[int(patchRandom(&state)%uint32(l.sourceCount))]
		}
	}
}

// upsampleLevel starts the finer copy where the coarser one finished: its hole
// from the coarser estimate, each target's match from the coarser match,
// scaled up.
func upsampleLevel(c, f *patchLevel, seed uint32) {
	w := f.w
	state := seed | 1
	for y := 0; y < f.h; y++ {
		for x := 0; x < w; x++ {
			p := y*w + x
			q := (y/2)*c.w + x/2
			if f.hole[p] != 0 {
				copy(f.color[p*patchChannels:p*patchChannels+patchChannels],
					c.color[q*patchChannels:q*patchChannels+patchChannels])
			}
			if f.target[p] == 0 {
				continue
			}
			candidate := -1
			if c.target[q] != 0 {
				m := int(c.match[q])
				mx := 2*(m%c.w) + (x & 1)
				my := 2*(m/c.w) + (y & 1)
				if mx < w && my < f.h && f.source[my*w+mx] != 0 {
					candidate = my*w + mx
				}
			}
			if candidate >= 0 {
				f.match[p] = int32(candidate)
			} else {
				f.match[p] = f.sources[int(patchRandom(&state)%uint32(f.sourceCount))]
			}
		}
	}
}

// PatchFill fills the pixels marked in hole (width×height, nonzero = fill)
// from the rest of b, in place, and reports whether it could. b is
// premultiplied RGBA8; opaque pixels outside the hole are what's matched and
// copied, transparent ones outside it are left alone and ignored. It returns
// false when there is no image to fill from — no 7 × 7 patch lying wholly on
// opaque pixels outside the hole.
func PatchFill(b *Bitmap, hole []uint8, seed uint32) bool {
	width, height := b.W, b.H
	stride := width * 4
	rgba := b.Pix
	// Work on the hole and the image around it: room to find texture, without
	// a whole large layer at every size.
	x0, y0, x1, y1 := width, height, 0, 0
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if hole[y*width+x] == 0 {
				continue
			}
			if x < x0 {
				x0 = x
			}
			if x+1 > x1 {
				x1 = x + 1
			}
			if y < y0 {
				y0 = y
			}
			if y+1 > y1 {
				y1 = y + 1
			}
		}
	}
	if x1 <= x0 {
		return true
	}
	size := x1 - x0
	if y1-y0 > size {
		size = y1 - y0
	}
	margin := size
	if margin < 64 {
		margin = 64
	}
	bx0, by0 := maxInt(x0-margin, 0), maxInt(y0-margin, 0)
	bx1, by1 := minInt(x1+margin, width), minInt(y1+margin, height)

	levels := make([]*patchLevel, patchMaxLevels)
	fine := newPatchLevel(bx1-bx0, by1-by0)
	levels[0] = fine
	for y := 0; y < fine.h; y++ {
		for x := 0; x < fine.w; x++ {
			s := (by0+y)*stride + (bx0+x)*4
			p := y*fine.w + x
			if hole[(by0+y)*width+bx0+x] != 0 {
				fine.hole[p] = 1
			}
			if fine.hole[p] == 0 && rgba[s+3] == 255 {
				fine.known[p] = 1
			}
			// The hole's texture comes from the patches it copies, never from
			// its blurred guess.
			fine.color[p*patchChannels+4] = 0
			fine.color[p*patchChannels+5] = 0
			for c := 0; c < 4; c++ {
				fine.color[p*patchChannels+c] = float32(rgba[s+c])
			}
		}
	}
	fine.classify()
	fine.measureTexture()
	count := 1
	if fine.sourceCount == 0 {
		return false
	}
	// Halved until the hole is a couple of patches across, or the image too
	// small to hold sources.
	for span := size; span > 4*(2*patchRadius+1) && count < patchMaxLevels; span /= 2 {
		f := levels[count-1]
		if f.w < 8*(2*patchRadius+1) || f.h < 8*(2*patchRadius+1) {
			break
		}
		coarse := f.downsample()
		if coarse.sourceCount == 0 {
			break
		}
		levels[count] = coarse
		count++
	}
	coarsest := levels[count-1]
	coarsest.fillInward()
	coarsest.matchRandomly(seed)
	coarsest.measure()
	coarsest.settle(12, seed)
	for i := count - 2; i >= 0; i-- {
		upsampleLevel(levels[i+1], levels[i], seed+uint32(i))
		levels[i].measure()
		levels[i].vote(1)
		levels[i].measure()
		iterations := 6
		if i == 0 {
			iterations = 4
		}
		levels[i].settle(iterations, seed+0x1000*uint32(i+1))
	}
	fine.vote(patchSharpFinish)
	fine.adaptColor()
	for y := 0; y < fine.h; y++ {
		for x := 0; x < fine.w; x++ {
			p := y*fine.w + x
			if fine.hole[p] == 0 {
				continue
			}
			t := (by0+y)*stride + (bx0+x)*4
			v := p * patchChannels
			alpha := fine.color[v+3]
			if alpha < 0 {
				alpha = 0
			} else if alpha > 255 {
				alpha = 255
			}
			rgba[t+3] = uint8(patchRound(alpha))
			for c := 0; c < 3; c++ {
				value := fine.color[v+c]
				if value < 0 {
					value = 0
				} else if value > float32(rgba[t+3]) {
					value = float32(rgba[t+3])
				}
				rgba[t+c] = uint8(patchRound(value))
			}
		}
	}
	return true
}

func patchAbs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// patchRound is the C's lrintf: it rounds to the current mode, nearest with
// ties to even (unlike lround, which rounds ties away from zero).
func patchRound(v float32) int64 { return int64(math.RoundToEven(float64(v))) }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
