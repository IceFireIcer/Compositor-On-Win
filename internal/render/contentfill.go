package render

// Content-aware fill and spot healing — the Go ports of ContentFill.c and
// HealPixels.c (verbatim C copies in tests/golden/c are the truth). Both run
// on premultiplied RGBA8 in place. The goldens: content-fill ε=0 (a fixed
// LCG seed makes the patch search deterministic), heal-spot ε=1 (sqrt/log/cos
// in the grain term cross math libraries).

// ContentFill ports content_fill: the masked (selected) pixels are filled by
// propagating coherent patch offsets from their known neighbors, refined by
// a randomized patch search over the unselected opaque pixels. Unselected
// transparent pixels are left as they are. mask is width×height coverage
// (nonzero = fill). Returns false when no donor pixels exist.
func ContentFill(b *Bitmap, mask []uint8) bool {
	w, h := b.W, b.H
	stride := w * 4
	n := w * h
	known := make([]byte, n)
	target := make([]byte, n)
	valid := make([]byte, n)
	queued := make([]byte, n)
	donors := make([]int, n+1)
	queue := make([]int, n)
	chosen := make([]int, n)
	pixels := b.Pix

	radius := 0
	if w >= 5 && h >= 5 {
		radius = 2
	}
	missing := 0
	donorCount := 0
	head, tail, scan := 0, 0, 0

	// Selected pixels are filled. Unselected opaque pixels are the image to
	// match and copy from; unselected transparent ones are neither.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := y*w + x
			target[p] = boolByte(mask[y*w+x] != 0)
			known[p] = boolByte(target[p] == 0 && pixels[y*stride+x*4+3] == 255)
			chosen[p] = -1
			if target[p] == 1 {
				missing++
			}
		}
	}
	seed := uint32(0x6d2b79f5)
	nextRandom := func() uint32 { return contentFillRandom(&seed) }
	if missing == 0 {
		donorCount = 1 // success: nothing to fill
		goto done
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := y*w + x
			if known[p] == 0 {
				continue
			}
			ok := true
			for dy := -radius; dy <= radius && ok; dy++ {
				for dx := -radius; dx <= radius; dx++ {
					sx, sy := x+dx, y+dy
					if sx < 0 || sy < 0 || sx >= w || sy >= h || known[sy*w+sx] == 0 {
						ok = false
						break
					}
				}
			}
			if ok {
				valid[p] = 1
				donors[donorCount] = p
				donorCount++
			}
		}
	}
	if donorCount == 0 {
		goto done
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := y*w + x
			nextToKnown := (x > 0 && known[p-1] == 1) ||
				(x+1 < w && known[p+1] == 1) ||
				(y > 0 && known[p-w] == 1) ||
				(y+1 < h && known[p+w] == 1)
			if target[p] == 1 && nextToKnown {
				queue[tail] = p
				tail++
				queued[p] = 1
			}
		}
	}
	for {
		for head < tail {
			p := queue[head]
			head++
			x, y := p%w, p/w
			best := -1
			score := infinity64
			neighbors := [4]int{-1, -1, -1, -1}
			if x > 0 {
				neighbors[0] = p - 1
			}
			if x+1 < w {
				neighbors[1] = p + 1
			}
			if y > 0 {
				neighbors[2] = p - w
			}
			if y+1 < h {
				neighbors[3] = p + w
			}
			// Propagate coherent source offsets, then refine with a
			// randomized patch search.
			for k := 0; k < 28; k++ {
				var q int
				if k < 4 {
					t := neighbors[k]
					if t >= 0 {
						base := t
						if chosen[t] >= 0 {
							base = chosen[t]
						}
						q = base + (p - t)
					} else {
						q = -1
					}
				} else {
					q = donors[int(nextRandom()%uint32(donorCount))]
				}
				if q < 0 || q >= n || valid[q] == 0 {
					continue
				}
				s := contentMatch(pixels, stride, known, w, h, p, q, radius)
				if best < 0 || s < score {
					score = s
					best = q
				}
			}
			if best < 0 {
				best = donors[0]
			}
			for r := 64; r >= 1; r /= 2 {
				qx := best%w + int(nextRandom()%uint32(2*r+1)) - r
				qy := best/w + int(nextRandom()%uint32(2*r+1)) - r
				if qx < 0 || qy < 0 || qx >= w || qy >= h || valid[qy*w+qx] == 0 {
					continue
				}
				q := qy*w + qx
				s := contentMatch(pixels, stride, known, w, h, p, q, radius)
				if s < score {
					score = s
					best = q
				}
			}
			copy(pixels[y*stride+x*4:y*stride+x*4+4], pixels[(best/w)*stride+(best%w)*4:(best/w)*stride+(best%w)*4+4])
			known[p] = 1
			chosen[p] = best
			for k := 0; k < 4; k++ {
				q := neighbors[k]
				if q >= 0 && target[q] == 1 && known[q] == 0 && queued[q] == 0 {
					queued[q] = 1
					queue[tail] = q
					tail++
				}
			}
		}
		// A selected area that only transparency touches starts from the
		// best random donor, then spreads.
		for scan < n && (target[scan] == 0 || known[scan] == 1) {
			scan++
		}
		if scan >= n {
			break
		}
		queue[tail] = scan
		tail++
		queued[scan] = 1
	}
done:
	return donorCount > 0
}

// nextRandom is the kernel's LCG (next_random): a fixed seed makes the
// patch search deterministic.
func contentFillRandom(state *uint32) uint32 {
	*state = *state*1664525 + 1013904223
	return *state
}

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}

const infinity64 = 1.7976931348623157e308

// contentMatch ports the static match: mean squared difference between the
// (2r+1)² neighborhood of p and q, over known pixels only.
func contentMatch(pixels []uint8, stride int, known []byte, w, h, p, q, radius int) float64 {
	px, py := p%w, p/w
	qx, qy := q%w, q/w
	count := 0
	sum := 0.0
	for dy := -radius; dy <= radius; dy++ {
		for dx := -radius; dx <= radius; dx++ {
			x, y := px+dx, py+dy
			sx, sy := qx+dx, qy+dy
			if x < 0 || y < 0 || x >= w || y >= h || sx < 0 || sy < 0 || sx >= w || sy >= h || known[y*w+x] == 0 {
				continue
			}
			a := pixels[y*stride+x*4 : y*stride+x*4+4]
			bb := pixels[sy*stride+sx*4 : sy*stride+sx*4+4]
			for c := 0; c < 4; c++ {
				d := int(a[c]) - int(bb[c])
				sum += float64(d * d)
			}
			count++
		}
	}
	if count == 0 {
		return infinity64
	}
	return sum / float64(count)
}
