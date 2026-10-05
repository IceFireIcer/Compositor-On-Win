package domain

import (
	"fmt"
	"math"
)

// Document limits (project-format.md "Limits" + per-version rules). The
// canvas-side and pixel-surface checks live in ValidateNewDocument; file and
// asset level limits (4 MiB manifest, 512 MiB assets) are ticket 05's.
const (
	MaxLayers        = 10_000
	MaxGroupDepth    = 64
	MaxClipChain     = 256
	MaxGuides        = 1_000
	MaxGuidePosition = 1_000_000
)

// Validate checks every model-level invariant of the parsed manifest:
// hierarchy (cycles, parents, depth), clipping chains, limits, appearance
// ranges and text-run rules. Asset and version-gating checks belong to
// ticket 05's ProjectStore.
func (d *Document) Validate() error {
	if d.Width < 1 || d.Height < 1 || d.Width > MaxSide || d.Height > MaxSide {
		return fmt.Errorf("画布尺寸 %d × %d 超出 1–%d px", d.Width, d.Height, MaxSide)
	}
	if len(d.Layers) > MaxLayers {
		return fmt.Errorf("图层数 %d 超过 %d 上限", len(d.Layers), MaxLayers)
	}

	byID := make(map[string]*Layer, len(d.Layers))
	for i := range d.Layers {
		l := &d.Layers[i]
		if _, dup := byID[l.ID]; dup {
			return fmt.Errorf("图层 ID 重复: %s", l.ID)
		}
		byID[l.ID] = l
	}

	for i := range d.Layers {
		if err := validateLayer(&d.Layers[i], byID); err != nil {
			return err
		}
	}

	for _, g := range d.Guides() {
		switch g.Axis {
		case GuideAxisHorizontal, GuideAxisVertical:
		default:
			return fmt.Errorf("参考线 %s 方向无效: %q", g.ID, g.Axis)
		}
		if math.IsNaN(g.Position) || math.IsInf(g.Position, 0) || math.Abs(g.Position) > MaxGuidePosition {
			return fmt.Errorf("参考线 %s 位置 %v 超出 ±%d px", g.ID, g.Position, MaxGuidePosition)
		}
	}
	return nil
}

func validateLayer(l *Layer, byID map[string]*Layer) error {
	if l.Opacity != nil {
		o := *l.Opacity
		if math.IsNaN(o) || math.IsInf(o, 0) || o < 0 || o > 1 {
			return fmt.Errorf("图层 %s 不透明度 %v 超出 0–1", l.ID, o)
		}
	}
	if l.BlendMode != nil {
		if _, ok := ParseBlendMode(string(*l.BlendMode)); !ok {
			return fmt.Errorf("图层 %s 混合模式无效: %q", l.ID, *l.BlendMode)
		}
	}

	isGroup := l.IsGroupLayer()
	if isGroup && l.ImageFile != nil {
		return fmt.Errorf("编组 %s 不能携带图像", l.ID)
	}
	if l.Adjustment != nil {
		if isGroup {
			return fmt.Errorf("编组 %s 不能是调整图层", l.ID)
		}
		if err := validateAdjustment(l.Adjustment); err != nil {
			return fmt.Errorf("图层 %s: %w", l.ID, err)
		}
	}
	if l.Text != nil {
		if isGroup {
			return fmt.Errorf("编组 %s 不能携带文字", l.ID)
		}
		if l.Adjustment != nil {
			return fmt.Errorf("调整图层 %s 不能携带文字", l.ID)
		}
		if err := validateText(l.Text); err != nil {
			return fmt.Errorf("图层 %s: %w", l.ID, err)
		}
	}
	if l.Effects != nil {
		if err := validateEffects(l.Effects); err != nil {
			return fmt.Errorf("图层 %s: %w", l.ID, err)
		}
	}

	// Hierarchy: parent must exist and be a group; walk ancestors to check
	// cycles and the 64-level depth budget (a leaf may sit under 64 groups;
	// the 64th group's own ancestors number 63).
	if l.ParentID != nil && *l.ParentID != l.ID {
		parent, ok := byID[*l.ParentID]
		if !ok {
			return fmt.Errorf("图层 %s 的父级 %s 不存在", l.ID, *l.ParentID)
		}
		if !parent.IsGroupLayer() {
			return fmt.Errorf("图层 %s 的父级 %s 不是编组", l.ID, *l.ParentID)
		}
	}
	if err := walkAncestors(l, byID); err != nil {
		return err
	}

	// Clipping masks: no self links, target exists and is not a group, and
	// the chain upward stays within 256 nodes.
	if l.MaskSourceID != nil {
		if *l.MaskSourceID == l.ID {
			return fmt.Errorf("图层 %s 的剪贴蒙版指向自身", l.ID)
		}
		source, ok := byID[*l.MaskSourceID]
		if !ok {
			return fmt.Errorf("图层 %s 的剪贴蒙版来源 %s 不存在", l.ID, *l.MaskSourceID)
		}
		if source.IsGroupLayer() {
			return fmt.Errorf("图层 %s 的剪贴蒙版来源 %s 是编组", l.ID, *l.MaskSourceID)
		}
		if err := walkClipChain(l, byID); err != nil {
			return err
		}
	}

	// An unlinked mask keeps its own placement; both fields require a mask.
	if l.MaskPlacement != nil && l.MaskFile == nil {
		return fmt.Errorf("图层 %s 有蒙版位移但没有蒙版文件", l.ID)
	}
	return nil
}

func walkAncestors(l *Layer, byID map[string]*Layer) error {
	visited := map[string]bool{l.ID: true}
	groupAncestors := 0
	parent := l.ParentID
	for parent != nil && *parent != "" {
		if visited[*parent] {
			return fmt.Errorf("图层 %s 的父级形成循环", l.ID)
		}
		visited[*parent] = true
		p, ok := byID[*parent]
		if !ok {
			return fmt.Errorf("图层 %s 的父级 %s 不存在", l.ID, *parent)
		}
		if p.IsGroupLayer() {
			groupAncestors++
		}
		limit := MaxGroupDepth // leaves may sit under 64 groups
		if l.IsGroupLayer() {
			limit = MaxGroupDepth - 1 // a group itself must fit inside 64
		}
		if groupAncestors > limit {
			return fmt.Errorf("图层 %s 的编组层级超过 %d 层", l.ID, MaxGroupDepth)
		}
		parent = p.ParentID
	}
	return nil
}

func walkClipChain(l *Layer, byID map[string]*Layer) error {
	visited := map[string]bool{}
	current := l
	for current.MaskSourceID != nil {
		id := *current.MaskSourceID
		if visited[id] {
			return fmt.Errorf("图层 %s 的剪贴链形成循环", l.ID)
		}
		visited[id] = true
		next, ok := byID[id]
		if !ok {
			return fmt.Errorf("图层 %s 的剪贴蒙版来源 %s 不存在", current.ID, id)
		}
		// Count every node in the chain, including this layer itself.
		if len(visited)+1 > MaxClipChain {
			return fmt.Errorf("图层 %s 的剪贴链超过 %d 节点", l.ID, MaxClipChain)
		}
		current = next
	}
	return nil
}

func validateAdjustment(a *Adjustment) error {
	validKind := false
	for _, k := range AllAdjustmentKinds {
		if a.Kind == k {
			validKind = true
			break
		}
	}
	if !validKind {
		return fmt.Errorf("调整类型无效: %q", a.Kind)
	}
	if err := rangeCheck("色相", a.Hue, -360, 360); err != nil {
		return err
	}
	if err := rangeCheck("饱和度", a.Saturation, -100, 100); err != nil {
		return err
	}
	if err := rangeCheck("明度", a.Lightness, -100, 100); err != nil {
		return err
	}
	if a.BlurRadius != nil {
		if err := rangeCheck("blurRadius", *a.BlurRadius, 0.1, 250); err != nil {
			return err
		}
	}
	if a.MotionAngle != nil {
		if err := rangeCheck("motionAngle", *a.MotionAngle, -90, 90); err != nil {
			return err
		}
	}
	if a.MotionDistance != nil {
		if err := rangeCheck("motionDistance", *a.MotionDistance, 1, 2000); err != nil {
			return err
		}
	}
	if a.NoiseAmount != nil {
		if err := rangeCheck("noiseAmount", *a.NoiseAmount, 0.1, 400); err != nil {
			return err
		}
	}
	if len(a.Levels.Ranges) != 4 {
		return fmt.Errorf("色阶需要 4 个通道区间，得到 %d", len(a.Levels.Ranges))
	}
	for _, r := range a.Levels.Ranges {
		if !finite(r.Black, r.Gamma, r.White, r.OutputBlack, r.OutputWhite) {
			return fmt.Errorf("色阶包含非有限数值")
		}
		if r.Black > r.White {
			return fmt.Errorf("色阶黑场 %v 大于白场 %v", r.Black, r.White)
		}
	}
	for ci, points := range a.Curves.Channels {
		for i, p := range points {
			if p.X < 0 || p.X > 255 || p.Y < 0 || p.Y > 255 {
				return fmt.Errorf("曲线通道 %d 的点 (%v, %v) 超出 0–255", ci, p.X, p.Y)
			}
			if i > 0 && p.X < points[i-1].X {
				return fmt.Errorf("曲线通道 %d 的 x 未按升序排列", ci)
			}
		}
	}
	return nil
}

func validateEffects(e *Effects) error {
	if e.Stroke != nil {
		if err := rangeCheck("描边大小", e.Stroke.Size, 0, 500); err != nil {
			return err
		}
	}
	if e.OuterGlow != nil {
		if err := rangeCheck("外发光大小", e.OuterGlow.Size, 0, 500); err != nil {
			return err
		}
	}
	if e.InnerGlow != nil {
		if err := rangeCheck("内发光大小", e.InnerGlow.Size, 0, 500); err != nil {
			return err
		}
	}
	return nil
}

// utf16Len counts UTF-16 code units — the unit text runs are measured in
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

func validateText(t *TextStyle) error {
	limit := utf16Len(t.Content)
	runs := struct {
		name  string
		items []struct{ location, length int }
	}{}
	_ = runs
	for i, r := range t.ColorRuns {
		if r.Length < 1 {
			return fmt.Errorf("colorRuns[%d] 长度必须为正", i)
		}
		if i > 0 && r.Location < t.ColorRuns[i-1].Location+t.ColorRuns[i-1].Length {
			return fmt.Errorf("colorRuns[%d] 与前一个 run 重叠或未按序", i)
		}
		if r.Location+r.Length > limit {
			return fmt.Errorf("colorRuns[%d] 超出文本范围（UTF-16 长度 %d）", i, limit)
		}
	}
	for i, r := range t.FontRuns {
		if r.Length < 1 {
			return fmt.Errorf("fontRuns[%d] 长度必须为正", i)
		}
		if i > 0 && r.Location < t.FontRuns[i-1].Location+t.FontRuns[i-1].Length {
			return fmt.Errorf("fontRuns[%d] 与前一个 run 重叠或未按序", i)
		}
		if r.Location+r.Length > limit {
			return fmt.Errorf("fontRuns[%d] 超出文本范围（UTF-16 长度 %d）", i, limit)
		}
	}
	return nil
}

func rangeCheck(name string, value, min, max float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < min || value > max {
		return fmt.Errorf("%s %v 超出 %g–%g", name, value, min, max)
	}
	return nil
}

func finite(values ...float64) bool {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}
