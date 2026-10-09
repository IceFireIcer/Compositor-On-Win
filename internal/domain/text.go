package domain

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// TextAlignment is the paragraph alignment. Raw values are capitalized.
type TextAlignment string

const (
	TextAlignmentLeft   TextAlignment = "Left"
	TextAlignmentCenter TextAlignment = "Center"
	TextAlignmentRight  TextAlignment = "Right"
)

// TextStyle keeps text layers editable. The PNG remains the display and
// export fallback; colorRuns/fontRuns (versions 10/11) carry per-run color
// and face overrides measured in UTF-16 units of content.
type TextStyle struct {
	Content   string         `json:"content"`
	FontName  string         `json:"fontName"`
	FontSize  float64        `json:"fontSize"`
	Red       float64        `json:"red"`
	Green     float64        `json:"green"`
	Blue      float64        `json:"blue"`
	Alignment TextAlignment  `json:"alignment"`
	Tracking  float64        `json:"tracking"`
	Leading   float64        `json:"leading"`
	BoxSize   *[2]float64    `json:"boxSize,omitempty"`
	ColorRuns []TextColorRun `json:"colorRuns,omitempty"`
	FontRuns  []TextFontRun  `json:"fontRuns,omitempty"`
}

// TextColorRun paints a span in another color.
type TextColorRun struct {
	Location int     `json:"location"`
	Length   int     `json:"length"`
	Red      float64 `json:"red"`
	Green    float64 `json:"green"`
	Blue     float64 `json:"blue"`
}

// TextFontRun sets a span in another face.
type TextFontRun struct {
	Location int    `json:"location"`
	Length   int    `json:"length"`
	FontName string `json:"fontName"`
}

// TextUTF16Len counts UTF-16 code units — the unit text runs are measured in
// (surrogate pairs count as two). The exported twin of validate.go's utf16Len,
// for callers outside this package (the bridge, tests).
func TextUTF16Len(s string) int { return utf16Len(s) }

// ValidateRuns checks the per-run style invariants shared by every carrier of
// a TextStyle (the bridge's TextCommit, the document model and the .comp
// manifest): nil means the whole text uses the base style; a non-nil list must
// not be empty, and its runs must be sorted, non-overlapping, of positive
// length, ending within the content's UTF-16 units, with finite 0–1 colors and
// non-empty font names of at most 200 characters without newlines. It mirrors
// ProjectStore's run predicates (project/validate.go textStyleValid) so a
// style cannot be valid in one place and invalid in another.
func (t TextStyle) ValidateRuns() error {
	limit := TextUTF16Len(t.Content)
	if err := validateColorRuns(t.ColorRuns, limit); err != nil {
		return err
	}
	return validateFontRuns(t.FontRuns, limit)
}

func validateColorRuns(runs []TextColorRun, limit int) error {
	if runs != nil && len(runs) == 0 {
		return fmt.Errorf("文字样式范围无效：colorRuns 为空数组")
	}
	end := 0
	for i, run := range runs {
		next, err := checkRunSpan("colorRuns", i, run.Location, run.Length, end, limit)
		if err != nil {
			return err
		}
		if !unitInterval(run.Red) || !unitInterval(run.Green) || !unitInterval(run.Blue) {
			return fmt.Errorf("文字样式颜色超出 0–1（colorRuns[%d]）", i)
		}
		end = next
	}
	return nil
}

func validateFontRuns(runs []TextFontRun, limit int) error {
	if runs != nil && len(runs) == 0 {
		return fmt.Errorf("文字样式范围无效：fontRuns 为空数组")
	}
	end := 0
	for i, run := range runs {
		next, err := checkRunSpan("fontRuns", i, run.Location, run.Length, end, limit)
		if err != nil {
			return err
		}
		// fontName.count is Swift's character count; runes are the closest Go
		// measure. Newlines never occur in a PostScript name.
		if run.FontName == "" || utf8.RuneCountInString(run.FontName) > 200 ||
			strings.ContainsAny(run.FontName, "\n\r\v\f") {
			return fmt.Errorf("文字样式字体名无效（fontRuns[%d]）", i)
		}
		end = next
	}
	return nil
}

// checkRunSpan applies the span predicates both run kinds share and returns
// the run's end offset, which becomes the next run's lower bound.
func checkRunSpan(name string, index, location, length, previousEnd, limit int) (int, error) {
	if location < previousEnd || location < 0 {
		return 0, fmt.Errorf("文字样式范围无效：范围重叠或未按序（%s[%d]）", name, index)
	}
	if length <= 0 {
		return 0, fmt.Errorf("文字样式范围无效：长度必须为正（%s[%d]）", name, index)
	}
	// Guard the sum before comparing, as the original does
	// (run.location <= Int.max - run.length).
	if location > math.MaxInt-length {
		return 0, fmt.Errorf("文字样式范围无效：超出文本范围（%s[%d]）", name, index)
	}
	end := location + length
	if end > limit {
		return 0, fmt.Errorf("文字样式范围无效：超出文本范围（%s[%d]，结束于 %d，文本 UTF-16 长度 %d）",
			name, index, end, limit)
	}
	return end, nil
}

func unitInterval(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1
}
