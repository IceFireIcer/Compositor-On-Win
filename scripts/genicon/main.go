package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

// One-off generator for the Wails build assets (app icon + Windows .ico).
// Run from repo root: go run scripts/genicon/main.go

const size = 256

func iconImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg := color.RGBA{30, 30, 32, 255}
	accent := color.RGBA{61, 126, 255, 255}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			c := bg
			dx, dy := x-128, y-128
			d := dx*dx + dy*dy
			// A "C" ring: outer radius 84, inner 58, with a gap on the right.
			if d < 84*84 && d > 58*58 && !(dx > 20 && dy > -36 && dy < 36) {
				c = accent
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func main() {
	var buf bytes.Buffer
	if err := png.Encode(&buf, iconImage()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	for _, p := range []string{
		filepath.Join("build", "appicon.png"),
		filepath.Join("build", "windows", "icon.ico"),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	if err := os.WriteFile(filepath.Join("build", "appicon.png"), buf.Bytes(), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// ICO wrapper: ICONDIR (6 bytes) + one ICONDIRENTRY (16 bytes) pointing at
	// the PNG payload (256×256 → width/height bytes are 0).
	ico := make([]byte, 22)
	ico[0], ico[1] = 0x00, 0x00 // reserved
	ico[2], ico[3] = 0x01, 0x00 // type: icon
	ico[4], ico[5] = 0x01, 0x00 // count: 1
	entry := ico[6:22]
	entry[0], entry[1] = 0, 0   // 256×256
	entry[2], entry[3] = 0, 0   // colors / reserved
	binary.LittleEndian.PutUint16(entry[4:], 1)  // planes
	binary.LittleEndian.PutUint16(entry[6:], 32) // bpp
	binary.LittleEndian.PutUint32(entry[8:], uint32(buf.Len()))
	binary.LittleEndian.PutUint32(entry[12:], 22) // offset
	ico = append(ico, buf.Bytes()...)

	if err := os.WriteFile(filepath.Join("build", "windows", "icon.ico"), ico, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("icons written")
}
