package render

// OrientDeep rewrites a deep raster the way ApplyOrientation rewrites an
// 8-bit bitmap, for EXIF orientations 1–8. A 16-bit import has to orient the
// authoritative pixels, not only their derived copy: orienting the copy and
// dropping the raster would silently turn a 16-bit file into an 8-bit layer.
// Orientation 1 (and anything unknown) returns d unchanged.
func OrientDeep(d *DeepRaster, orientation int) *DeepRaster {
	if d == nil || orientation < 2 || orientation > 8 {
		return d
	}
	w, h := d.W, d.H
	nw, nh := w, h
	if orientation >= 5 {
		nw, nh = h, w
	}
	out := NewDeep(nw, nh, d.HDR)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := orientedPoint(orientation, x, y, w, h)
			copy(out.Pix[(dy*nw+dx)*4:(dy*nw+dx)*4+4], d.Pix[(y*w+x)*4:])
		}
	}
	return out
}

// orientedPoint is where pixel (x, y) of a w×h image lands under an EXIF
// orientation, matching rasterio.ApplyOrientation's per-case destinations.
func orientedPoint(orientation, x, y, w, h int) (int, int) {
	switch orientation {
	case 2:
		return w - 1 - x, y
	case 3:
		return w - 1 - x, h - 1 - y
	case 4:
		return x, h - 1 - y
	case 5:
		return y, x
	case 6:
		return h - 1 - y, x
	case 7:
		return h - 1 - y, w - 1 - x
	case 8:
		return y, w - 1 - x
	default:
		return x, y
	}
}
