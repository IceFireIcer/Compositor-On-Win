package render

// Camera Raw 的阶段合成（票 78）。上游把每一步滑杆表示成「至多八张测量表按权重混合」
// （CameraRawStage），再把整条链合成为一张更细的查找表，逐像素只查一次。
//
// 真值是 tests/golden/c/AdjustPixels.c 的 table_lookup / camera_raw_stage_color /
// camera_raw_compose（:246,298,269），逐行对应。

// cameraRawStage is CameraRawStage: up to eight measured tables blended by
// weight. A nil table means no change.
type cameraRawStage struct {
	tables  [8][]uint8
	weights [8]float64
}

// cameraRawBlend builds a stage from up to eight (table, weight) pairs, as
// CameraRawTables.blend does; the rest of the slots stay nil and zero, so a
// missing table passes the colour through.
func cameraRawBlend(parts ...cameraRawStagePart) cameraRawStage {
	var stage cameraRawStage
	for i, part := range parts {
		if i >= len(stage.tables) {
			break
		}
		stage.tables[i] = part.table
		stage.weights[i] = part.weight
	}
	return stage
}

type cameraRawStagePart struct {
	table  []uint8
	weight float64
}

// cameraTableLookup is table_lookup: the table's colour for an sRGB colour
// (0…1), between its eight nearest entries. Trilinear, so the result is smooth
// between the measured points.
func cameraTableLookup(table []uint8, size int, in [3]float64) [3]float64 {
	var i [3]int
	var frac [3]float64
	for c := 0; c < 3; c++ {
		x := cameraClamp(in[c]) * float64(size-1)
		if x >= float64(size-1) {
			i[c] = size - 2
		} else {
			i[c] = int(x)
		}
		frac[c] = x - float64(i[c])
	}
	var out [3]float64
	for corner := 0; corner < 8; corner++ {
		dr, dg, db := corner>>2, (corner>>1)&1, corner&1
		w := 1.0
		if dr == 1 {
			w *= frac[0]
		} else {
			w *= 1 - frac[0]
		}
		if dg == 1 {
			w *= frac[1]
		} else {
			w *= 1 - frac[1]
		}
		if db == 1 {
			w *= frac[2]
		} else {
			w *= 1 - frac[2]
		}
		if w == 0 {
			continue
		}
		p := (((i[0]+dr)*size+(i[1]+dg))*size + (i[2] + db)) * 3
		out[0] += w * float64(table[p])
		out[1] += w * float64(table[p+1])
		out[2] += w * float64(table[p+2])
	}
	return [3]float64{out[0] / 255, out[1] / 255, out[2] / 255}
}

// cameraStageColor is camera_raw_stage_color: every slot with a weight looks
// the colour up in its own table (a missing table passes it through) and the
// results are blended, then clamped.
func cameraStageColor(stage *cameraRawStage, size int, rgb [3]float64) [3]float64 {
	var next [3]float64
	for t := 0; t < len(stage.tables); t++ {
		w := stage.weights[t]
		if w == 0 {
			continue
		}
		looked := rgb
		if stage.tables[t] != nil {
			looked = cameraTableLookup(stage.tables[t], size, rgb)
		}
		for c := 0; c < 3; c++ {
			next[c] += w * looked[c]
		}
	}
	return [3]float64{cameraClamp(next[0]), cameraClamp(next[1]), cameraClamp(next[2])}
}

// cameraComposeStages is camera_raw_compose: run every stage in order over a
// grid³ lattice of sRGB colours, red slowest, three float32 a colour. The
// lattice is finer than the measured tables, so composing blurs little, and a
// pixel then costs one lookup instead of the whole chain.
func cameraComposeStages(grid int, stages []cameraRawStage, size int) []float32 {
	out := make([]float32, grid*grid*grid*3)
	div := float64(grid - 1)
	for r := 0; r < grid; r++ {
		for g := 0; g < grid; g++ {
			for b := 0; b < grid; b++ {
				i := ((r*grid)+g)*grid + b
				color := [3]float64{float64(r) / div, float64(g) / div, float64(b) / div}
				for s := range stages {
					color = cameraStageColor(&stages[s], size, color)
				}
				// The lattice is the output whatever the chain held, even an empty
				// one: only the _onto variant leaves an empty chain alone.
				out[i*3], out[i*3+1], out[i*3+2] = float32(color[0]), float32(color[1]), float32(color[2])
			}
		}
	}
	return out
}

// cameraComposeOnto is camera_raw_compose_onto: the same run over a lattice
// that already holds colours, in place. An empty chain leaves it as it is.
func cameraComposeOnto(table []float32, grid int, stages []cameraRawStage, size int) {
	if len(stages) == 0 {
		return
	}
	div := float64(grid - 1)
	for r := 0; r < grid; r++ {
		for g := 0; g < grid; g++ {
			for b := 0; b < grid; b++ {
				i := ((r*grid)+g)*grid + b
				color := [3]float64{float64(r) / div, float64(g) / div, float64(b) / div}
				for s := range stages {
					color = cameraStageColor(&stages[s], size, color)
				}
				table[i*3], table[i*3+1], table[i*3+2] = float32(color[0]), float32(color[1]), float32(color[2])
			}
		}
	}
}

// cameraComposedLookup is composed_lookup: a composed table's colour for an
// sRGB colour, between its eight nearest entries.
func cameraComposedLookup(table []float32, grid int, in [3]float64) [3]float64 {
	var i [3]int
	var frac [3]float64
	for c := 0; c < 3; c++ {
		x := cameraClamp(in[c]) * float64(grid-1)
		if x >= float64(grid-1) {
			i[c] = grid - 2
		} else {
			i[c] = int(x)
		}
		frac[c] = x - float64(i[c])
	}
	var out [3]float64
	for corner := 0; corner < 8; corner++ {
		dr, dg, db := corner>>2, (corner>>1)&1, corner&1
		w := 1.0
		if dr == 1 {
			w *= frac[0]
		} else {
			w *= 1 - frac[0]
		}
		if dg == 1 {
			w *= frac[1]
		} else {
			w *= 1 - frac[1]
		}
		if db == 1 {
			w *= frac[2]
		} else {
			w *= 1 - frac[2]
		}
		if w == 0 {
			continue
		}
		p := (((i[0]+dr)*grid+(i[1]+dg))*grid + (i[2] + db)) * 3
		out[0] += w * float64(table[p])
		out[1] += w * float64(table[p+1])
		out[2] += w * float64(table[p+2])
	}
	return [3]float64{cameraClamp(out[0]), cameraClamp(out[1]), cameraClamp(out[2])}
}

// cameraRawBetween is CameraRawTables.between: the two tables either side of
// `value` on a run of `tables` starting at `low`, `step` apart, and how far
// between them it is.
func cameraRawBetween(value, low, step float64, tables int) (int, float64) {
	position := (value - low) / step
	if position < 0 {
		position = 0
	}
	if maxPosition := float64(tables - 1); position > maxPosition {
		position = maxPosition
	}
	index := int(position)
	if index > tables-2 {
		index = tables - 2
	}
	return index, position - float64(index)
}

// cameraRawStage is CameraRawTables.stage: the two tables either side of
// `value` on a measured run, blended.
func cameraRawTableStage(set *cameraRawTableSet, start int, value, low, step float64, tables int) cameraRawStage {
	index, fraction := cameraRawBetween(value, low, step, tables)
	return cameraRawBlend(
		cameraRawStagePart{table: set.tableAt(start + index), weight: 1 - fraction},
		cameraRawStagePart{table: set.tableAt(start + index + 1), weight: fraction},
	)
}

// cameraRawExposureStage is the exposure stage: 21 tables, −5…5 by 0.5
// (CameraRawTables.swift:158). The same run of tables is the lens vignette
// correction, applied by position instead of by pixel.
func cameraRawExposureStage(set *cameraRawTableSet, exposure float64) cameraRawStage {
	return cameraRawTableStage(set, cameraRawExposureStart, exposure, -5, 0.5, 21)
}

// Table-run starts, from CameraRawTables.swift:17-26. Only the ones ported so
// far are named; the rest arrive with their sliders.
const (
	cameraRawExposureStart = 81
)
