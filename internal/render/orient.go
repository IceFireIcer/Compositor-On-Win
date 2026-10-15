package render

// EXIF orientation, in one place. OrientDeep (a deep raster) and
// rasterio.ApplyOrientation (an 8-bit bitmap) are the same eight
// transformations over two pixel types, so both ask these two functions
// rather than each carrying its own copy of the switch.

// OrientedSize is the raster's size once oriented: the four transposed
// orientations (5-8) swap the sides.
func OrientedSize(orientation, w, h int) (int, int) {
	if orientation >= 5 && orientation <= 8 {
		return h, w
	}
	return w, h
}

// OrientedPoint is where pixel (x, y) of a w×h raster lands under an EXIF
// orientation. Orientation 1 and anything unknown stay put, as CIImage's
// oriented(forExifOrientation:) does.
func OrientedPoint(orientation, x, y, w, h int) (int, int) {
	switch orientation {
	case 2: // mirror horizontal
		return w - 1 - x, y
	case 3: // rotate 180
		return w - 1 - x, h - 1 - y
	case 4: // mirror vertical
		return x, h - 1 - y
	case 5: // transpose
		return y, x
	case 6: // rotate 90 CW
		return h - 1 - y, x
	case 7: // anti-transpose
		return h - 1 - y, w - 1 - x
	case 8: // rotate 270 CW
		return y, w - 1 - x
	default:
		return x, y
	}
}

// OrientDeep rewrites a deep raster the way rasterio.ApplyOrientation rewrites
// an 8-bit bitmap, for EXIF orientations 1-8. A 16-bit import has to orient the
// authoritative pixels, not only their derived copy: orienting the copy and
// dropping the raster would silently turn a 16-bit file into an 8-bit layer.
// Orientation 1 (and anything unknown) returns d unchanged.
func OrientDeep(d *DeepRaster, orientation int) *DeepRaster {
	if d == nil || orientation < 2 || orientation > 8 {
		return d
	}
	w, h := d.W, d.H
	nw, nh := OrientedSize(orientation, w, h)
	out := NewDeep(nw, nh, d.HDR)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := OrientedPoint(orientation, x, y, w, h)
			copy(out.Pix[(dy*nw+dx)*4:(dy*nw+dx)*4+4], d.Pix[(y*w+x)*4:])
		}
	}
	return out
}
