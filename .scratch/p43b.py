p = 'internal/render/canvasops.go'
s = open(p, encoding='utf-8').read()

# DrawTransformed: add the doc-scale leg to the inverse map.
s = s.replace('''// DrawTransformed draws src through t into dst, whose top-left sits at
// document (offX, offY) — the same inverse mapping and sampling the
// compositor\\'s Place uses, at a local grid instead of the whole canvas
// (ImageResizer's per-layer re-rasterization; rotation and flips bake into
// the pixels, so the caller stores the plain origin/size).
func DrawTransformed(src *Bitmap, t domain.Transform, dst *Bitmap, offX, offY float64, sampling domain.Sampling) {
	if src == nil || dst == nil || t.Size[0] <= 0 || t.Size[1] <= 0 {
		return
	}
	rad := t.Rotation * math.Pi / 180
	c, s := math.Cos(rad), math.Sin(rad)
	cx := t.Origin[0] + t.Size[0]/2
	cy := t.Origin[1] + t.Size[1]/2
	sub := 1
	if sampling == domain.SamplingHighQuality {
		sub = 2
	}
	for py := 0; py < dst.H; py++ {
		for px := 0; px < dst.W; px++ {
			dx := float64(px) + offX + 0.5 - cx
			dy := float64(py) + offY + 0.5 - cy''','''// DrawTransformed draws src through t into dst, whose top-left sits at
// (offX, offY) in the NEW document space — the original draws each layer
// with translate(-left,-top)·scale(sx,sy) on the context, so the grid
// position is old·(sx,sy) − (offX,offY) and the inverse map undoes the
// scale first. Rotation and flips bake into the pixels; the caller stores
// the plain origin/size (ImageResizer's per-layer re-rasterization).
func DrawTransformed(src *Bitmap, t domain.Transform, sx, sy float64, dst *Bitmap, offX, offY float64, sampling domain.Sampling) {
	if src == nil || dst == nil || t.Size[0] <= 0 || t.Size[1] <= 0 || sx <= 0 || sy <= 0 {
		return
	}
	rad := t.Rotation * math.Pi / 180
	c, s := math.Cos(rad), math.Sin(rad)
	cx := t.Origin[0] + t.Size[0]/2
	cy := t.Origin[1] + t.Size[1]/2
	sub := 1
	if sampling == domain.SamplingHighQuality {
		sub = 2
	}
	for py := 0; py < dst.H; py++ {
		for px := 0; px < dst.W; px++ {
			// Grid pixel centre → old document coords: undo the grid offset,
			// then the document scale.
			dx := (float64(px)+0.5+offX)/sx - cx
			dy := (float64(py)+0.5+offY)/sy - cy''')

# DrawMaskTransformed: same scale leg; signature gains sx, sy.
s = s.replace('''func DrawMaskTransformed(gray []uint8, srcW, srcH int, t domain.Transform, w, h int, offX, offY float64, sampling domain.Sampling) []uint8 {
	out := make([]uint8, w*h)
	if srcW <= 0 || srcH <= 0 || len(gray) < srcW*srcH || t.Size[0] <= 0 || t.Size[1] <= 0 {
		return out
	}''','''func DrawMaskTransformed(gray []uint8, srcW, srcH int, t domain.Transform, sx, sy float64, w, h int, offX, offY float64, sampling domain.Sampling) []uint8 {
	out := make([]uint8, w*h)
	if srcW <= 0 || srcH <= 0 || len(gray) < srcW*srcH || t.Size[0] <= 0 || t.Size[1] <= 0 || sx <= 0 || sy <= 0 {
		return out
	}''')
s = s.replace('''	for py := 0; py < h; py++ {
		for px := 0; px < w; px++ {
			dx := float64(px) + offX + 0.5 - cx
			dy := float64(py) + offY + 0.5 - cy
			vx := c*dx + s*dy
			vy := -s*dx + c*dy
			ux := vx/t.Size[0] + 0.5
			uy := vy/t.Size[1] + 0.5
			if t.FlipX {''','''	for py := 0; py < h; py++ {
		for px := 0; px < w; px++ {
			dx := (float64(px)+0.5+offX)/sx - cx
			dy := (float64(py)+0.5+offY)/sy - cy
			vx := c*dx + s*dy
			vy := -s*dx + c*dy
			ux := vx/t.Size[0] + 0.5
			uy := vy/t.Size[1] + 0.5
			if t.FlipX {''')
open(p, 'w', encoding='utf-8', newline='\n').write(s)
print('canvasops scaled')
