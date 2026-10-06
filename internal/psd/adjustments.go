package psd

// PSDAdjustments (PSDReader.swift's enum): levl / curv / hue2 layer
// adjustment data parsed into the domain's adjustment records. hue2 emits
// the hsvSettings raw JSON the render package's cube parser reads — the
// same manifest encoding as macOS's HueSaturationSettings.

import (
	"encoding/json"

	"compositor-win/internal/domain"
)

// ParsedAdjustment carries the domain adjustment record.
type ParsedAdjustment struct {
	Adjustment domain.Adjustment
}

func (p *ParsedAdjustment) AdjustmentKind() string { return string(p.Adjustment.Kind) }

// parseAdjustment mirrors PSDAdjustments.parse: levl first, then curv, then
// hue2 (falling back to the legacy hue).
func parseAdjustment(extra map[string][]byte) *ParsedAdjustment {
	if data := extra["levl"]; data != nil {
		if a := parseLevels(data); a != nil {
			return a
		}
	}
	if data := extra["curv"]; data != nil {
		if a := parseCurves(data); a != nil {
			return a
		}
	}
	if data := extra["hue2"]; data != nil {
		if a := parseHue(data); a != nil {
			return a
		}
	}
	if data := extra["hue "]; data != nil {
		if a := parseHue(data); a != nil {
			return a
		}
	}
	return nil
}

func u16At(data []byte, offset int) uint16 {
	return uint16(data[offset])<<8 | uint16(data[offset+1])
}

func i16At(data []byte, offset int) int16 {
	return int16(u16At(data, offset))
}

// parseLevels ports PSDAdjustments.levels: a version, then records of input
// black, input white, output black, output white and gamma in hundredths
// (100 is 1.00) for RGB, then red, green and blue — 292 bytes.
func parseLevels(data []byte) *ParsedAdjustment {
	if len(data) < 292 {
		return nil
	}
	var settings domain.LevelsSettings
	settings.Channel = domain.LevelsRGB
	for channel := 0; channel < 4; channel++ {
		base := 2 + channel*10
		inputBlack := float64(u16At(data, base))
		inputWhite := float64(u16At(data, base+2))
		outputBlack := float64(u16At(data, base+4))
		outputWhite := float64(u16At(data, base+6))
		gamma := float64(u16At(data, base+8)) / 100
		settings.Ranges[channel] = normalizeLevelRange(domain.LevelRange{
			Black: inputBlack, Gamma: gamma, White: inputWhite,
			OutputBlack: outputBlack, OutputWhite: outputWhite,
		})
	}
	return &ParsedAdjustment{Adjustment: domain.Adjustment{
		Kind:   domain.AdjustmentLevels,
		Levels: settings,
	}}
}

// normalizeLevelRange mirrors LevelRange.normalized (the same clamps as
// render's normalizeLevelRange).
func normalizeLevelRange(r domain.LevelRange) domain.LevelRange {
	if r.Black != r.Black || r.Black < 0 {
		r.Black = 0
	}
	if r.Black > 254 {
		r.Black = 254
	}
	if r.White != r.White || r.White < r.Black+1 {
		r.White = r.Black + 1
	}
	if r.White > 255 {
		r.White = 255
	}
	if r.Gamma != r.Gamma || r.Gamma < 0.1 {
		r.Gamma = 0.1
	}
	if r.Gamma > 9.99 {
		r.Gamma = 9.99
	}
	if r.OutputBlack != r.OutputBlack || r.OutputBlack < 0 {
		r.OutputBlack = 0
	}
	if r.OutputBlack > 255 {
		r.OutputBlack = 255
	}
	if r.OutputWhite != r.OutputWhite || r.OutputWhite < 0 {
		r.OutputWhite = 255
	}
	if r.OutputWhite > 255 {
		r.OutputWhite = 255
	}
	return r
}

// parseCurves ports PSDAdjustments.curves: an optional pad byte, a version
// (1 or 4), a channel count, then per channel a point count and
// (output, input) u16 pairs. Endpoints extend to 0 and 255.
func parseCurves(data []byte) *ParsedAdjustment {
	if len(data) < 5 {
		return nil
	}
	offset := 0
	if data[offset] == 0 {
		offset++
	}
	if offset+2 > len(data) {
		return nil
	}
	version := u16At(data, offset)
	offset += 2
	if version != 1 && version != 4 {
		return nil
	}
	if offset+2 > len(data) {
		return nil
	}
	count := int(u16At(data, offset))
	offset += 2
	var settings domain.CurvesSettings
	settings.Channel = domain.LevelsRGB
	for channel := 0; channel < min(4, count); channel++ {
		if offset+2 > len(data) {
			return nil
		}
		points := int(u16At(data, offset))
		offset += 2
		var curve []domain.CurvePoint
		for i := 0; i < points; i++ {
			if offset+4 > len(data) {
				return nil
			}
			output := float64(u16At(data, offset))
			input := float64(u16At(data, offset+2))
			offset += 4
			curve = append(curve, domain.CurvePoint{
				X: min(255, max(0, input)), Y: min(255, max(0, output)),
			})
		}
		if len(curve) >= 2 {
			sortCurve(curve)
			if curve[0].X != 0 {
				curve = append([]domain.CurvePoint{{X: 0, Y: curve[0].Y}}, curve...)
			}
			if curve[len(curve)-1].X != 255 {
				curve = append(curve, domain.CurvePoint{X: 255, Y: curve[len(curve)-1].Y})
			}
			settings.Channels[channel] = curve
		}
	}
	for c := 0; c < 4; c++ {
		if len(settings.Channels[c]) < 2 {
			settings.Channels[c] = []domain.CurvePoint{{X: 0, Y: 0}, {X: 255, Y: 255}}
		}
	}
	return &ParsedAdjustment{Adjustment: domain.Adjustment{
		Kind:   domain.AdjustmentCurves,
		Curves: settings,
	}}
}

func sortCurve(curve []domain.CurvePoint) {
	for i := 1; i < len(curve); i++ {
		for j := i; j > 0 && curve[j].X < curve[j-1].X; j-- {
			curve[j], curve[j-1] = curve[j-1], curve[j]
		}
	}
}

// The hue range names, matching the render package's hsvSettings keys.
var hueRangeNames = []string{"Master", "Reds", "Yellows", "Greens", "Cyans", "Blues", "Purples", "Magentas"}

// parseHue ports PSDAdjustments.hue: a version, the Colorize switch and a
// pad byte, the Colorize hue/saturation/lightness, the Master's, then for
// Reds through Magentas the band (degrees) and its three values.
func parseHue(data []byte) *ParsedAdjustment {
	if len(data) < 16 {
		return nil
	}
	colorize := data[2] != 0
	type hsl struct {
		Hue        float64 `json:"hue"`
		Saturation float64 `json:"saturation"`
		Lightness  float64 `json:"lightness"`
	}
	adjustments := map[string]hsl{}
	bands := map[string][4]float64{}
	values := func(offset int) hsl {
		return hsl{
			Hue:        float64(i16At(data, offset)),
			Saturation: float64(i16At(data, offset+2)),
			Lightness:  float64(i16At(data, offset+4)),
		}
	}
	// Colorize has values of its own; the Master applies otherwise.
	if colorize {
		adjustments["Master"] = values(4)
	} else {
		adjustments["Master"] = values(10)
	}
	if !colorize {
		offset := 16
		for i := 1; i < len(hueRangeNames) && i <= 8; i++ {
			if offset+14 > len(data) {
				break
			}
			degrees := func(at int) float64 {
				value := float64(i16At(data, at))
				value = value - float64(int(value/360))*360
				if value < 0 {
					value += 360
				}
				return value
			}
			name := hueRangeNames[i]
			bands[name] = [4]float64{
				degrees(offset), degrees(offset + 2), degrees(offset + 4), degrees(offset + 6),
			}
			adjustments[name] = values(offset + 8)
			offset += 14
		}
	}
	payload := map[string]any{
		"range":       "Master",
		"colorize":    colorize,
		"invertRange": false,
		"adjustments": adjustments,
		"bands":       bands,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return &ParsedAdjustment{Adjustment: domain.Adjustment{
		Kind:        domain.AdjustmentHueSaturation,
		HSVSettings: raw,
	}}
}
