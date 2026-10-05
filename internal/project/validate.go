package project

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"compositor-win/internal/domain"
)

// assetPixelBudget caps the summed source pixels of one document's images
// and, separately, of its masks. The macOS DocumentLimits.documentPixelBudget
// floats with physical memory (200M on small machines, up to 800M on 16 GB+);
// we pin the floor so validation is deterministic.
const assetPixelBudget = domain.MaxSurfacePixels

// maxTransformSize is LayerTransform.isValid's size ceiling (300,000, wider
// than the 30,000 canvas side — transforms may place oversized canvases).
const maxTransformSize = 300_000

// maxNameBytes is the UTF-8 byte budget for a layer name.
const maxNameBytes = 16_384

// validateDocument ports ProjectStore.validate, in its original order so the
// rejection class (invalid / version / tooLarge) matches the original for
// every failure. Hierarchy, clip chains, adjustment ranges, text runs,
// effects and guide axes/positions come from the domain model; this wraps
// their errors into the project error classes.
func validateDocument(doc *domain.Document) error {
	if doc == nil {
		return invalidf("清单为空")
	}
	if doc.Format != domain.FormatID {
		return invalidf("format 必须为 %q，得到 %q", domain.FormatID, doc.Format)
	}
	if doc.Version < 1 || doc.Version > domain.FormatVersion {
		return &VersionError{Version: doc.Version}
	}
	if doc.ColorSpace != domain.ColorSpaceSRGB {
		return invalidf("colorSpace 必须为 %q，得到 %q", domain.ColorSpaceSRGB, doc.ColorSpace)
	}
	if doc.Resolution != nil && (*doc.Resolution < domain.MinResolution || *doc.Resolution > domain.MaxResolution) {
		return invalidf("resolution %d 超出 %d–%d ppi", *doc.Resolution, domain.MinResolution, domain.MaxResolution)
	}
	if doc.Width < 1 || doc.Height < 1 || doc.Width > domain.MaxSide || doc.Height > domain.MaxSide {
		return toLargef("画布尺寸 %d × %d 超出 1–%d px", doc.Width, doc.Height, domain.MaxSide)
	}
	if len(doc.Layers) > domain.MaxLayers {
		return toLargef("图层数 %d 超过 %d 上限", len(doc.Layers), domain.MaxLayers)
	}

	for i := range doc.Layers {
		if err := validateLayerManifest(&doc.Layers[i], doc); err != nil {
			return err
		}
	}

	// Hierarchy, clip chains, adjustment/text/effect ranges and guide
	// axes/positions are the domain model's invariants.
	if err := doc.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}

	// Live-mask gating beyond the graph shape: a group cannot carry a clip
	// link, and a clip source cannot be an adjustment layer
	// (LiveMaskGraph.validate).
	for i := range doc.Layers {
		l := &doc.Layers[i]
		if l.MaskSourceID == nil {
			continue
		}
		if l.IsGroupLayer() {
			return invalidf("编组 %s 不能携带剪贴蒙版链接", l.ID)
		}
		if source := findLayer(doc, *l.MaskSourceID); source != nil && source.Adjustment != nil {
			return invalidf("图层 %s 的剪贴蒙版来源 %s 不能是调整图层", l.ID, *l.MaskSourceID)
		}
	}
	if doc.Version < 5 {
		for i := range doc.Layers {
			if doc.Layers[i].MaskSourceID != nil {
				return invalidf("版本 %d 不能包含 maskSourceID", doc.Version)
			}
		}
	}
	if doc.Version == 1 {
		for i := range doc.Layers {
			if doc.Layers[i].ParentID != nil || doc.Layers[i].IsGroup != nil && *doc.Layers[i].IsGroup {
				return invalidf("版本 1 不能包含 parentID 或 isGroup")
			}
		}
	}

	ids := make(map[string]bool, len(doc.Layers))
	for i := range doc.Layers {
		l := &doc.Layers[i]
		if ids[l.ID] {
			return invalidf("图层 ID 重复: %s", l.ID)
		}
		ids[l.ID] = true
		if !transformValid(l.Transform) {
			return invalidf("图层 %s 的 transform 无效", l.ID)
		}
		if strings.TrimSpace(l.Name) == "" {
			return invalidf("图层 %s 的名称不能为空白", l.ID)
		}
		if len(l.Name) > maxNameBytes {
			return invalidf("图层 %s 的名称超过 %d 字节", l.ID, maxNameBytes)
		}
		if l.ImageFile != nil && *l.ImageFile != imageName(l.ID) {
			return invalidf("图层 %s 的 imageFile 必须为 %q，得到 %q", l.ID, imageName(l.ID), *l.ImageFile)
		}
	}
	if doc.ActiveLayerID != nil && !ids[*doc.ActiveLayerID] {
		return invalidf("activeLayerID %s 不在图层列表中", *doc.ActiveLayerID)
	}
	return validateGuides(doc)
}

// validateLayerManifest ports the per-layer part of ProjectStore.validate:
// version gating for text, adjustments, masks and appearance.
func validateLayerManifest(l *domain.Layer, doc *domain.Document) error {
	version := doc.Version
	isGroup := l.IsGroupLayer()

	// Per-letter colors arrived in version 10, per-letter faces in 11; text
	// rides on pixel layers only.
	if l.Text != nil {
		if !textStyleValid(l.Text) {
			return invalidf("图层 %s 的文字元数据无效", l.ID)
		}
		if l.Text.ColorRuns != nil && version < 10 {
			return invalidf("版本 %d 不能包含 colorRuns", version)
		}
		if l.Text.FontRuns != nil && version < 11 {
			return invalidf("版本 %d 不能包含 fontRuns", version)
		}
		if l.ImageFile == nil || isGroup || l.Adjustment != nil {
			return invalidf("图层 %s 的文字元数据只能属于像素图层", l.ID)
		}
	}

	if l.Adjustment != nil {
		if version < 7 {
			return invalidf("版本 %d 不能包含调整图层", version)
		}
		if isGroup || l.ImageFile != nil {
			return invalidf("调整图层 %s 不能是编组或携带图像", l.ID)
		}
		switch l.Adjustment.Kind {
		case domain.AdjustmentGaussianBlur, domain.AdjustmentMotionBlur, domain.AdjustmentAddNoise:
			if version < 9 {
				return invalidf("调整类型 %q 需要版本 9 以上，得到 %d", l.Adjustment.Kind, version)
			}
		}
	}

	// Layer masks arrived in version 4, folder masks in version 6.
	if l.MaskFile != nil {
		minVersion := 4
		if isGroup {
			minVersion = 6
		}
		if version < minVersion {
			return invalidf("版本 %d 不能给%s添加蒙版", version, groupWord(isGroup))
		}
		if *l.MaskFile != maskName(l.ID) {
			return invalidf("图层 %s 的 maskFile 必须为 %q，得到 %q", l.ID, maskName(l.ID), *l.MaskFile)
		}
	}
	if l.MaskEnabled != nil && l.MaskFile == nil {
		return invalidf("图层 %s 有 maskEnabled 但没有 maskFile", l.ID)
	}
	if l.MaskPlacement != nil && (!transformValid(*l.MaskPlacement) || l.MaskFile == nil) {
		return invalidf("图层 %s 的 maskPlacement 无效或缺少蒙版", l.ID)
	}

	// Folders took an opacity of their own in version 8, which multiplies
	// into what is inside them; their blend mode is still pass-through, so
	// it stays Normal.
	opacity := 1.0
	if l.Opacity != nil {
		opacity = *l.Opacity
	}
	blend := domain.BlendNormal
	if l.BlendMode != nil {
		blend = *l.BlendMode
	}
	if math.IsNaN(opacity) || math.IsInf(opacity, 0) || opacity < 0 || opacity > 1 {
		return invalidf("图层 %s 的不透明度 %v 超出 0–1", l.ID, opacity)
	}
	if version < 3 && !(opacity == 1 && blend == domain.BlendNormal) {
		return invalidf("版本 %d 不能包含非默认外观（不透明度/混合模式）", version)
	}
	if isGroup && !(blend == domain.BlendNormal && (version >= 8 || opacity == 1)) {
		return invalidf("编组 %s 的混合模式必须为 Normal 且版本 %d 前不透明度须为 1", l.ID, 8)
	}
	return nil
}

// validateGuides ports ProjectStore.validateGuides. Axis validity, finiteness
// and the position ceiling are the domain model's; the count ceiling is
// file-level and maps to ErrTooLarge, as in the original.
func validateGuides(doc *domain.Document) error {
	guides := doc.Guides()
	if doc.Version < 8 {
		if len(guides) > 0 {
			return invalidf("版本 %d 不能包含参考线", doc.Version)
		}
		return nil
	}
	if len(guides) > domain.MaxGuides {
		return toLargef("参考线数量 %d 超过 %d 上限", len(guides), domain.MaxGuides)
	}
	seen := make(map[string]bool, len(guides))
	for _, g := range guides {
		if seen[g.ID] {
			return invalidf("参考线 ID 重复: %s", g.ID)
		}
		seen[g.ID] = true
	}
	return nil
}

// transformValid ports LayerTransform.isValid plus the sampling spellings
// the macOS JSONDecoder enforces at decode time (LayerSampling is an enum,
// so an unknown spelling rejects the whole manifest there).
func transformValid(t domain.Transform) bool {
	switch t.Sampling {
	case domain.SamplingNearest, domain.SamplingSmooth, domain.SamplingHighQuality:
	default:
		return false
	}
	values := [5]float64{t.Origin[0], t.Origin[1], t.Size[0], t.Size[1], t.Rotation}
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return t.Size[0] >= 1 && t.Size[0] <= maxTransformSize &&
		t.Size[1] >= 1 && t.Size[1] <= maxTransformSize &&
		math.Abs(t.Origin[0]) <= 1_000_000 && math.Abs(t.Origin[1]) <= 1_000_000
}

// textStyleValid ports LayerTextStyle.isValid plus the alignment spelling the
// macOS JSONDecoder enforces (TextAlignment is an enum).
func textStyleValid(t *domain.TextStyle) bool {
	switch t.Alignment {
	case domain.TextAlignmentLeft, domain.TextAlignmentCenter, domain.TextAlignmentRight:
	default:
		return false
	}
	if utf16Len(t.Content) > 100_000 {
		return false
	}
	if !finiteRange(t.FontSize, 1, 2000) {
		return false
	}
	if !finite01(t.Red) || !finite01(t.Green) || !finite01(t.Blue) {
		return false
	}
	if !finiteRange(t.Tracking, -100, 1000) {
		return false
	}
	if !finiteRange(t.Leading, 0, 5000) {
		return false
	}
	if t.BoxSize != nil {
		w, h := (*t.BoxSize)[0], (*t.BoxSize)[1]
		if math.IsNaN(w) || math.IsInf(w, 0) || math.IsNaN(h) || math.IsInf(h, 0) {
			return false
		}
		if w < 16 || w > domain.MaxSide || h < 16 || h > domain.MaxSide {
			return false
		}
		if w*h > domain.MaxSurfacePixels {
			return false
		}
	}
	return colorRunsValid(t) && fontRunsValid(t)
}

func colorRunsValid(t *domain.TextStyle) bool {
	if t.ColorRuns == nil {
		return true
	}
	if len(t.ColorRuns) == 0 {
		return false
	}
	end := 0
	for _, run := range t.ColorRuns {
		if run.Location < end || run.Length <= 0 {
			return false
		}
		if !finite01(run.Red) || !finite01(run.Green) || !finite01(run.Blue) {
			return false
		}
		end = run.Location + run.Length
	}
	return end <= utf16Len(t.Content)
}

func fontRunsValid(t *domain.TextStyle) bool {
	if t.FontRuns == nil {
		return true
	}
	if len(t.FontRuns) == 0 {
		return false
	}
	end := 0
	for _, run := range t.FontRuns {
		if run.Location < end || run.Length <= 0 {
			return false
		}
		// fontName.count is Swift's character count; runes are the closest
		// Go measure. Newlines never occur in a PostScript name.
		if run.FontName == "" || utf8.RuneCountInString(run.FontName) > 200 ||
			strings.ContainsAny(run.FontName, "\n\r\v\f") {
			return false
		}
		end = run.Location + run.Length
	}
	return end <= utf16Len(t.Content)
}

// utf16Len counts UTF-16 code units, the unit text runs are measured in
// (surrogate pairs count as two).
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return n
}

func finiteRange(v, min, max float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= min && v <= max
}

func finite01(v float64) bool { return finiteRange(v, 0, 1) }

func findLayer(doc *domain.Document, id string) *domain.Layer {
	for i := range doc.Layers {
		if doc.Layers[i].ID == id {
			return &doc.Layers[i]
		}
	}
	return nil
}

func groupWord(isGroup bool) string {
	if isGroup {
		return "编组"
	}
	return "图层"
}

// imageName / maskName are the asset filename conventions: derived from the
// layer's uppercase UUID ("\(id.uuidString).png" in the original).
func imageName(layerID string) string { return strings.ToUpper(layerID) + ".png" }
func maskName(layerID string) string  { return strings.ToUpper(layerID) + ".mask.png" }
