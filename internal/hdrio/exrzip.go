package hdrio

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
)

// exrRLE undoes the run-length encoding ImfRle.cpp writes. Each byte is a
// count: with its sign bit clear it introduces that many plus one copies of
// the byte after it, and negative it introduces that many literal bytes.
func exrRLE(src []byte, rawSize int) ([]byte, error) {
	out := make([]byte, 0, rawSize)
	for i := 0; i < len(src) && len(out) < rawSize; {
		n := int(int8(src[i]))
		i++
		if n < 0 {
			count := -n
			if i+count > len(src) {
				return nil, fmt.Errorf("%w: an RLE literal run is short by %d bytes", ErrMalformed, i+count-len(src))
			}
			out = append(out, src[i:i+count]...)
			i += count
			continue
		}
		if i >= len(src) {
			return nil, fmt.Errorf("%w: an RLE repeat run has no byte to repeat", ErrMalformed)
		}
		for k := 0; k <= n; k++ {
			out = append(out, src[i])
		}
		i++
	}
	if len(out) != rawSize {
		return nil, fmt.Errorf("%w: RLE gave %d bytes, want %d", ErrMalformed, len(out), rawSize)
	}
	return out, nil
}

// exrZip undoes ImfZip.cpp's compression: inflate the zlib stream, undo the
// running difference, then undo the split into two halves.
func exrZip(src []byte, rawSize int) ([]byte, error) {
	zr, err := zlib.NewReader(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("%w: a ZIP scanline block is not a zlib stream: %v", ErrMalformed, err)
	}
	defer zr.Close()
	tmp := make([]byte, rawSize)
	if _, err := io.ReadFull(zr, tmp); err != nil {
		return nil, fmt.Errorf("%w: a ZIP scanline block inflates to less than %d bytes: %v", ErrMalformed, rawSize, err)
	}
	// The predictor: every byte is stored as its difference from the one
	// before it, biased by 128 so the difference fits in a byte.
	for i := 1; i < rawSize; i++ {
		tmp[i] = byte(int(tmp[i-1]) + int(tmp[i]) - 128)
	}
	// The reorder: the compressor dealt the bytes alternately into the first
	// and second half of the buffer, so reading them back alternates too.
	out := make([]byte, rawSize)
	t1, t2 := 0, (rawSize+1)/2
	for s := 0; s < rawSize; {
		out[s] = tmp[t1]
		t1++
		s++
		if s < rawSize {
			out[s] = tmp[t2]
			t2++
			s++
		}
	}
	return out, nil
}
