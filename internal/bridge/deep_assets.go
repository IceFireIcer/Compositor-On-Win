package bridge

// 16 位与 HDR 图层的资产编解码（票 68 / ADR-0007）。
//
// 关键不变量：深层像素是图层的真源，8 位位图只是它的派生副本（render.Bitmap.Deep
// 指向真源）。所以保存写的是 render.Bitmap.Deep，而不是位图本身；位图永远不反写回
// 深层像素。任何不认识深层的操作产出的新位图天然没有 Deep，图层从此就是 8 位——
// 与原版「any other operation makes a new ImportedImage without it」一致。

import (
	"fmt"
	"strings"

	"compositor-win/internal/domain"
	"compositor-win/internal/hdrio"
	"compositor-win/internal/render"
)

// encodeAsset encodes one asset for a .comp package: a deep layer's
// full-precision pixels, a mask's 8-bit grayscale, or an ordinary 8-bit PNG.
// The asset name decides nothing about depth — bmp.Deep does.
func encodeAsset(name string, bmp *render.Bitmap) ([]byte, error) {
	switch {
	case bmp == nil:
		return nil, fmt.Errorf("资产 %s 没有像素", name)
	case bmp.Deep != nil:
		return encodeDeep(bmp.Deep)
	case strings.HasSuffix(name, ".mask.png"):
		return render.EncodeMaskPNG(bmp)
	default:
		return render.EncodePNG(bmp)
	}
}

// encodeDeep writes a layer's full-precision pixels: a 16-bit PNG, or an EXR
// for an HDR layer (ADR-0007 §3). The 8-bit bitmap beside it is never written:
// it is a derived copy, and storing it would lose exactly the precision the
// deep raster exists to keep.

// alignAssetNames makes each layer's asset suffix match its pixels before a
// save. An HDR raster is an EXR and anything else a PNG (ADR-0007 §3); the
// package reader and the version stamp both decide from the name, so a
// mismatch would either fail to decode or declare the wrong version. The
// rename is on the snapshot only — the live session keeps its names until
// the write succeeds and the tab reloads.
func alignAssetNames(doc *domain.Document, bitmaps map[string]*render.Bitmap) error {
	for i := range doc.Layers {
		l := &doc.Layers[i]
		if l.ImageFile == nil {
			continue
		}
		old := *l.ImageFile
		want := layerAssetNameFor(l.ID, bitmaps[old])
		if want == old {
			continue
		}
		if _, taken := bitmaps[want]; taken {
			return fmt.Errorf("资产 %s 与图层 %s 的目标名冲突", want, l.ID)
		}
		bitmaps[want] = bitmaps[old]
		delete(bitmaps, old)
		l.ImageFile = &want
	}
	return nil
}
func encodeDeep(deep *render.DeepRaster) ([]byte, error) {
	if deep.HDR {
		// EXR holds linear light; HDRExposure proves the raster holds encoded
		// extended sRGB, so the codec converts on the way out (ADR-0007 §3).
		return hdrio.EncodeEXR(deep)
	}
	return render.EncodePNG16(deep)
}
