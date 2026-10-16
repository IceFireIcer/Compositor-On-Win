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

// Table-run starts, from CameraRawTables.swift:17-26.
const (
	cameraRawExposureStart    = 81
	cameraRawWhitesStart      = 102
	cameraRawBlacksStart      = 111
	cameraRawSaturationStart  = 120
	cameraRawVibranceStart    = 129
	cameraRawContrastStart    = 138
	cameraRawHighlightsStart  = 187
	cameraRawShadowsStart     = 236
	cameraRawCalibrationStart = 405
	cameraRawParametricStart  = 435
)

// The adaptive sliders' measuring grid (CameraRawTables.swift:27-28): seven
// picture brightnesses and seven slider values.
var cameraRawBrightnesses = []float64{53, 80, 104, 130, 160, 190, 224}
var cameraRawAdaptiveValues = []float64{-100, -50, -25, 0, 25, 50, 100}

// cameraRawBetweenPoints is the [Double] overload of CameraRawTables.between:
// the two entries either side of `value` and how far between them it is. The
// value is clamped to the range first, so a slider past its end lands on the
// last interval.
func cameraRawBetweenPoints(value float64, points []float64) (int, float64) {
	if value < points[0] {
		value = points[0]
	}
	if last := points[len(points)-1]; value > last {
		value = last
	}
	index := 0
	for index < len(points)-2 && value > points[index+1] {
		index++
	}
	return index, (value - points[index]) / (points[index+1] - points[index])
}

// cameraRawAdaptive is CameraRawTables.adaptive: an adaptive slider at `value`
// for a picture whose measure of brightness is `brightness`, from the four
// tables around both. Contrast reads the brightest channel, highlights the
// log-average and shadows the mean luminance (CameraRawTables.swift:162-166).
func cameraRawAdaptive(set *cameraRawTableSet, start int, value, brightness float64) cameraRawStage {
	row, down := cameraRawBetweenPoints(brightness, cameraRawBrightnesses)
	column, across := cameraRawBetweenPoints(value, cameraRawAdaptiveValues)
	width := len(cameraRawAdaptiveValues)
	base := start + row*width + column
	return cameraRawBlend(
		cameraRawStagePart{table: set.tableAt(base), weight: (1 - down) * (1 - across)},
		cameraRawStagePart{table: set.tableAt(base + 1), weight: (1 - down) * across},
		cameraRawStagePart{table: set.tableAt(base + width), weight: down * (1 - across)},
		cameraRawStagePart{table: set.tableAt(base + width + 1), weight: down * across},
	)
}

// cameraRawWhiteBalance is CameraRawTables.whiteBalance: Temperature by rows
// and Tint across, each −100…100 by 25, so the four grid tables around the pair
// blend. The grid is the first 81 tables, with no start offset.
func cameraRawWhiteBalance(set *cameraRawTableSet, temperature, tint float64) cameraRawStage {
	row, down := cameraRawBetween(temperature, -100, 25, 9)
	column, across := cameraRawBetween(tint, -100, 25, 9)
	return cameraRawBlend(
		cameraRawStagePart{table: set.tableAt(row*9 + column), weight: (1 - down) * (1 - across)},
		cameraRawStagePart{table: set.tableAt(row*9 + column + 1), weight: (1 - down) * across},
		cameraRawStagePart{table: set.tableAt((row+1)*9 + column), weight: down * (1 - across)},
		cameraRawStagePart{table: set.tableAt((row+1)*9 + column + 1), weight: down * across},
	)
}

// cameraRawHasMeasuredDividers is CameraRawCurveSettings.hasMeasuredDividers
// (CameraRawColor.swift:79): the tables were measured with Photoshop's own
// dividers, so a moved divider needs the curve pass instead of them.
func cameraRawHasMeasuredDividers(c *CameraRawCurveSettings) bool {
	return c.ShadowSplit == 25 && c.DarkSplit == 50 && c.LightSplit == 75 && c.RefineSaturation == 0
}

// cameraRawTableStages is CameraRawTables.stages(for:brightness:) without the
// grading pass (upstream keeps that in gradingStages): Calibration, Exposure,
// White Balance, the adaptive Light sliders, the fixed Light and Color ones,
// then the parametric curve while its dividers sit where Photoshop's were
// measured. A slider at zero contributes nothing, so an untouched picture
// composes an empty chain — and the order matters, because these run in
// sequence over the lattice rather than as independent lookups.
func cameraRawTableStages(set *cameraRawTableSet, s *CameraRawSettings, brightness cameraRawBrightness) []cameraRawStage {
	var stages []cameraRawStage
	// Calibration first, red hue and saturation through blue (CameraRawTables.swift:154).
	cal := &s.Calibration
	for index, value := range []float64{
		cal.RedHue, cal.RedSaturation, cal.GreenHue, cal.GreenSaturation, cal.BlueHue, cal.BlueSaturation,
	} {
		if value != 0 {
			stages = append(stages, cameraRawFifty(set, cameraRawCalibrationStart+index*5, value))
		}
	}
	if s.Exposure != 0 {
		stages = append(stages, cameraRawExposureStage(set, s.Exposure))
	}
	if s.Temperature != 0 || s.Tint != 0 {
		stages = append(stages, cameraRawWhiteBalance(set, s.Temperature, s.Tint))
	}
	// Each adaptive slider reads its own measure of the picture.
	if s.Contrast != 0 {
		stages = append(stages, cameraRawAdaptive(set, cameraRawContrastStart, s.Contrast, brightness.brightest))
	}
	if s.Highlights != 0 {
		stages = append(stages, cameraRawAdaptive(set, cameraRawHighlightsStart, s.Highlights, brightness.logBrightest))
	}
	if s.Shadows != 0 {
		stages = append(stages, cameraRawAdaptive(set, cameraRawShadowsStart, s.Shadows, brightness.luminance))
	}
	for _, part := range []struct {
		start int
		value float64
	}{
		{cameraRawWhitesStart, s.Whites},
		{cameraRawBlacksStart, s.Blacks},
		{cameraRawSaturationStart, s.Saturation},
		{cameraRawVibranceStart, s.Vibrance},
	} {
		if part.value != 0 {
			stages = append(stages, cameraRawTableStage(set, part.start, part.value, -100, 25, 9))
		}
	}
	if cameraRawHasMeasuredDividers(&s.Curve) {
		for index, value := range []float64{s.Curve.Shadows, s.Curve.Darks, s.Curve.Lights, s.Curve.Highlights} {
			if value != 0 {
				stages = append(stages, cameraRawFifty(set, cameraRawParametricStart+index*5, value))
			}
		}
	}
	return stages
}

// cameraRawFifty is CameraRawTables.fifty: a slider on a run of five tables
// −100…100 by 50.
func cameraRawFifty(set *cameraRawTableSet, start int, value float64) cameraRawStage {
	return cameraRawTableStage(set, start, value, -100, 50, 5)
}
