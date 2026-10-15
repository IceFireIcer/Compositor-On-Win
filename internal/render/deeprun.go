package render

// The deep_split/deep_merge family of DeepPixels.c: how an 8-bit-shaped
// operation runs on a 16-bit layer without losing its precision. The layer's
// colors are read as 8-bit values a little below them (deep_split), the
// operation runs on those and on the ones one, two and four steps up, and the
// 16-bit result is read off between them (deep_merge). See DeepLayer.run.
//
// The arithmetic stays in float32 where the C does: Go folds untyped constants
// at higher precision, so the constants below are spelled as float32.

import (
	"fmt"
	"math"
)

// deepAlpha8 is deep_alpha8: the 8-bit alpha a 16-bit one rounds to.
func deepAlpha8(alpha uint16) int64 {
	return int64(lroundf(float32(alpha) * (255.0 / 65535.0)))
}

// deepNoise is deep_noise: a fixed noise for each pixel and channel, 0 up to 1,
// the same in every run so they all start from the same `low`.
func deepNoise(x, y, channel int) float32 {
	h := uint32(x)*0x9E3779B1 ^ uint32(y)*0x85EBCA77 ^ uint32(channel+1)*0xC2B2AE3D
	h ^= h >> 15
	h *= 0x2C1B3C6D
	h ^= h >> 12
	h *= 0x297A2D39
	h ^= h >> 15
	return float32(h>>8) / float32(uint32(1)<<24)
}

// deepLow is deep_low: a channel's `low`, a step below its 8-bit value rounded
// as dither rounds (down or up at random, more often towards the nearer), so
// the color lies 0 to 2 steps above it; kept within the pixel's alpha
// (premultiplied colors never pass it).
func deepLow(value uint16, alpha int64, x, y, channel int) int64 {
	low := int64(math.Floor(float64(float32(value)*(255.0/65535.0)+deepNoise(x, y, channel)))) - 1
	if low < 0 {
		return 0
	}
	if low > alpha {
		return alpha
	}
	return low
}

// deepPast is deep_past: how far the color is above `low`, in 8-bit steps, 0 to 2.
func deepPast(value uint16, low int64) float32 {
	return minf(2, maxf(0, float32(value)*(255.0/65535.0)-float32(low)))
}

// deepChange is the C's DeepChange: how an input channel moves an output one,
// from the runs with it at `low` (l), one, two and four steps up (s1, s2, s4),
// the color `past` steps above `low`, and the steps alpha left room for (room).
//
// A straight line is fitted through all four runs where they lie near one: the
// operation's results are 8-bit too and say too little across one step, and one
// that works at 8 bits inside (Camera Raw) wobbles from one value to the next,
// which read step by step came out as faint bands. Where they do not (a bend: a
// steep Levels clipping to white), the value is read off the straight line
// between the steps the color lies between, exact wherever the operation is
// straight across one step. `offset` is where the fit puts `low`'s result
// against that run's own rounded one.
type deepChange struct {
	change float32
	offset float32
	fitted bool
}

func deepChangeFor(l, s1, s2, s4, past float32, room int64) deepChange {
	if room <= 0 {
		return deepChange{}
	}
	steps := deepChange{}
	if room == 1 || past < 1 {
		steps.change = (s1 - l) * minf(past, 1)
	} else {
		steps.change = (s1 - l) + (s2-s1)*(past-1)
	}
	// Too near alpha for the far runs: the steps there are.
	if room < 4 {
		return steps
	}
	// Least squares through (0, l), (1, s1), (2, s2), (4, s4): the steps' mean
	// is 1.75, their spread 8.75.
	mean := (l + s1 + s2 + s4) / 4
	slope := (-1.75*l - 0.75*s1 + 0.25*s2 + 2.25*s4) / 8.75
	start := mean - slope*1.75
	// A run more than a step off the line (8-bit rounding and wobble stay under
	// one) is a bend.
	off := maxf(maxf(absf(l-start), absf(s1-start-slope)), maxf(absf(s2-start-2*slope), absf(s4-start-4*slope)))
	if off > 1 {
		return steps
	}
	return deepChange{change: slope * past, offset: start - l, fitted: true}
}

// DeepSplit is deep_split: the 8-bit raster `step` steps above a 16-bit one's
// `low` (0 for `low` itself), kept within alpha — in every color channel, or
// with channel 0-2 only in that one (-1 for all).
func DeepSplit(deep *DeepRaster, step, channel int) *Bitmap {
	out := NewBitmap(deep.W, deep.H)
	for y := 0; y < deep.H; y++ {
		for x := 0; x < deep.W; x++ {
			i := (y*deep.W + x) * 4
			alpha := deepAlpha8(deep.Pix[i+3])
			for c := 0; c < 3; c++ {
				value := deepLow(deep.Pix[i+c], alpha, x, y, c)
				if channel < 0 || channel == c {
					value += int64(step)
				}
				if value > alpha {
					value = alpha
				}
				out.Pix[i+c] = uint8(value)
			}
			out.Pix[i+3] = uint8(alpha)
		}
	}
	return out
}

// RunDeep is DeepLayer.run: an 8-bit-shaped operation run on a 16-bit raster
// without losing its precision. The raster is split into 8-bit values a little
// below its colors and into those one, two and four steps up, the operation
// runs on each, and the 16-bit result is read off between them.
//
// mixesChannels is for an operation whose output channels each depend on all
// three inputs (Camera Raw's color sliders, Tonal Contrast, Scanlines): each
// input channel is stepped on its own — ten runs rather than four — and every
// output channel adds up how each moves it. Read the other way those came out
// banded.
//
// The operation must leave each raster the size it was given.
func RunDeep(deep *DeepRaster, mixesChannels bool, op func(*Bitmap) error) (*DeepRaster, error) {
	if deep == nil {
		return nil, fmt.Errorf("没有可运行的深层像素")
	}
	type split struct{ step, channel int }
	splits := []split{{0, -1}}
	channels := []int{-1}
	if mixesChannels {
		channels = []int{0, 1, 2}
	}
	for _, step := range []int{1, 2, 4} {
		for _, channel := range channels {
			splits = append(splits, split{step, channel})
		}
	}
	results := make([]*Bitmap, len(splits))
	for i, sp := range splits {
		bmp := DeepSplit(deep, sp.step, sp.channel)
		if err := op(bmp); err != nil {
			return nil, err
		}
		results[i] = bmp
	}
	// Each step's three runs, red, green and blue stepped alone; or its one
	// run, read for all three.
	per := 1
	if mixesChannels {
		per = 3
	}
	runs := func(index int) [3]*Bitmap {
		var out [3]*Bitmap
		for c := 0; c < 3; c++ {
			k := 0
			if mixesChannels {
				k = c
			}
			out[c] = results[1+index*per+k]
		}
		return out
	}
	return DeepMerge(deep, results[0], runs(0), runs(1), runs(2), mixesChannels), nil
}

// DeepMerge is deep_merge: the 16-bit result from an operation's results on
// `low` and one, two and four steps up (DeepSplit with steps 0, 1, 2 and 4).
// Without mixes, only the first of each three is read, stepped in every
// channel; with it, the three are red, green and blue stepped alone.
func DeepMerge(deep *DeepRaster, lowResult *Bitmap, near, mid, far [3]*Bitmap, mixes bool) *DeepRaster {
	out := NewDeep(deep.W, deep.H, false)
	per := 1
	if mixes {
		per = 3
	}
	_ = per
	runs := [3][3]*Bitmap{near, mid, far}
	for y := 0; y < deep.H; y++ {
		for x := 0; x < deep.W; x++ {
			i := (y*deep.W + x) * 4
			alpha := deepAlpha8(deep.Pix[i+3])
			outAlpha := float32(lowResult.Pix[i+3]) * 257.0
			var past [3]float32
			var room [3]int64
			for k := 0; k < 3; k++ {
				low := deepLow(deep.Pix[i+k], alpha, x, y, k)
				past[k] = deepPast(deep.Pix[i+k], low)
				room[k] = alpha - low
			}
			for c := 0; c < 3; c++ {
				l := float32(lowResult.Pix[i+c])
				value := l
				var offset float32
				fits := 0
				// Each input channel's own runs move this output channel (only its
				// own, unless they mix). Each fit also says where `low`'s result
				// really lies; they all say it of the same run, so averaged.
				first := c
				last := c + 1
				if mixes {
					first, last = 0, 3
				}
				for k := first; k < last; k++ {
					r := 0
					if mixes {
						r = k
					}
					change := deepChangeFor(l,
						float32(runs[0][r].Pix[i+c]), float32(runs[1][r].Pix[i+c]), float32(runs[2][r].Pix[i+c]),
						past[k], room[k])
					value += change.change
					if change.fitted {
						offset += change.offset
						fits++
					}
				}
				if fits > 0 {
					value += offset / float32(fits)
				}
				scaled := value * 257.0
				if scaled < 0 {
					scaled = 0
				} else if scaled > outAlpha {
					scaled = outAlpha
				}
				out.Pix[i+c] = uint16(lroundf(scaled))
			}
			out.Pix[i+3] = uint16(lroundf(outAlpha))
		}
	}
	return out
}
