//go:build !windows

// Package colorio is the Windows counterpart of the original's CMYK.swift.
// Windows is the only target; this file keeps the package buildable — and
// its pure-Go proof rule testable — elsewhere, where lcms2 is not linked.
package colorio

import "errors"

// ErrNotCMYK mirrors upstream `CMYKError.notCMYK`.
var ErrNotCMYK = errors.New("That profile isn't a CMYK output profile.")

// errNoColorEngine reports the missing lcms2 build on a non-Windows host.
var errNoColorEngine = errors.New("colorio: the lcms2 color engine is only built for Windows")

// IsCMYKProfile reports whether data is a CMYK ICC profile. Without lcms2
// nothing can be opened, so it is always false off Windows.
func IsCMYKProfile([]byte) bool { return false }

// isICCProfile reports whether data is an ICC profile at all. Off Windows
// there is no engine to open it with, so it is always false.
func isICCProfile([]byte) bool { return false }

// ConvertToInk is unavailable off Windows.
func ConvertToInk(rgb []uint8, w, h int, profile []byte, matte [3]uint8) ([]uint8, error) {
	return nil, errNoColorEngine
}

// ConvertToRGB is unavailable off Windows.
func ConvertToRGB(ink []uint8, w, h int, profile []byte) ([]uint8, error) {
	return nil, errNoColorEngine
}
