package psd

// 拼合 PSD：图层段为空时，文件尾的图像数据段里那张就是文件要显示的图。
//
// 此前 `Read` 在这种情况下直接返回**零图层** —— 打开一个正常的拼合 PSD 会得到一个**空文档**，
// 而且**不报错**。用户看到的是「打开了但什么都没有」，比报错更难查。
//
// 图像数据段的布局与图层通道不同：一个压缩方式字，然后**按通道平面**排（不是交错），
// 每个平面自上而下。所以这里单独读，不复用 decodeChannels（那条与图层记录绑定）。

import "compositor-win/internal/render"

const (
	compressionRaw = 0
	compressionRLE = 1
)

// mergedImage reads the image data section into a premultiplied RGBA bitmap.
//
// channels is the header's composite channel count: 3 for RGB, 4 when there is an
// alpha channel. A CMYK file never reaches this point — the colour-mode gate
// refuses it first (ticket 86's remaining work).
func mergedImage(c *cursor, width, height, channels int, isPSB bool) (*render.Bitmap, error) {
	if width < 1 || height < 1 || channels < 1 {
		return nil, ErrTruncated
	}
	compression, err := c.u16()
	if err != nil {
		return nil, err
	}
	planes := make([][]uint8, channels)
	for i := 0; i < channels; i++ {
		switch compression {
		case compressionRaw:
			plane, err := c.bytes(width * height)
			if err != nil {
				return nil, err
			}
			planes[i] = plane
		case compressionRLE:
			// unpackRLE 从 data 开头自己读行字节数表，所以把表与打包数据一起交给它。
			countSize := 2
			if isPSB {
				countSize = 4
			}
			counts, err := c.bytes(height * countSize)
			if err != nil {
				return nil, err
			}
			total := 0
			for row := 0; row < height; row++ {
				if isPSB {
					at := row * 4
					total += int(uint32(counts[at])<<24 | uint32(counts[at+1])<<16 | uint32(counts[at+2])<<8 | uint32(counts[at+3]))
				} else {
					at := row * 2
					total += int(uint16(counts[at])<<8 | uint16(counts[at+1]))
				}
			}
			packed, err := c.bytes(total)
			if err != nil {
				return nil, err
			}
			joined := make([]uint8, 0, len(counts)+len(packed))
			joined = append(joined, counts...)
			joined = append(joined, packed...)
			plane, err := unpackRLE(width, height, joined, isPSB, nil)
			if err != nil {
				return nil, err
			}
			planes[i] = plane
		default:
			return nil, ErrUnsupportedCompress
		}
	}

	out := render.NewBitmap(width, height)
	for i := 0; i < width*height; i++ {
		r, g, b := planes[0][i], planes[0][i], planes[0][i]
		if channels >= 3 {
			g, b = planes[1][i], planes[2][i]
		}
		a := uint8(255)
		if channels == 4 {
			a = planes[3][i]
		}
		// 预乘：全链路是不变量 #2 的预乘 RGBA。
		o := i * 4
		out.Pix[o] = uint8(uint32(r) * uint32(a) / 255)
		out.Pix[o+1] = uint8(uint32(g) * uint32(a) / 255)
		out.Pix[o+2] = uint8(uint32(b) * uint32(a) / 255)
		out.Pix[o+3] = a
	}
	return out, nil
}
