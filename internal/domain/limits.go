// Package domain holds the document model and its invariants.
// Ticket 02 starts with the document limits; the full model lands in
// ticket 04 around these constants.
package domain

import (
	"fmt"
	"math"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DocumentLimits, ported from the macOS DocumentLimits:
// max side 30,000px, max single surface 200M pixels,
// resolution 1–9600ppi (manifest range), default 72ppi.
const (
	MaxSide          = 30_000
	MaxSurfacePixels = 200_000_000
	MinResolution    = 1
	MaxResolution    = 9600
)

// DefaultResolution is used when the new-canvas form leaves it empty.
const DefaultResolution = 72

// LimitsError is a user-facing validation failure; its message is shown
// directly in the new-canvas form.
type LimitsError struct{ msg string }

func (e *LimitsError) Error() string { return e.msg }

func limitsErrf(format string, args ...any) *LimitsError {
	return &LimitsError{msg: fmt.Sprintf(format, args...)}
}

// ValidateNewDocument checks a new-canvas request against the document
// limits. Sides are checked before the surface product so absurd inputs
// report 边长 rather than overflowing.
func ValidateNewDocument(width, height, resolution int) *LimitsError {
	if width < 1 || height < 1 {
		return limitsErrf("宽高必须为正数（得到 %d × %d）", width, height)
	}
	if width > MaxSide {
		return limitsErrf("边长超限：宽 %d 超过 %d px", width, MaxSide)
	}
	if height > MaxSide {
		return limitsErrf("边长超限：高 %d 超过 %d px", height, MaxSide)
	}
	if pixels := width * height; pixels > MaxSurfacePixels {
		return limitsErrf("像素超限：%d × %d = %.1fM，超过 %.0fM 上限",
			width, height, float64(pixels)/1e6, float64(MaxSurfacePixels)/1e6)
	}
	if resolution < MinResolution || resolution > MaxResolution {
		return limitsErrf("分辨率超出 %d–%d ppi", MinResolution, MaxResolution)
	}
	return nil
}

// DocumentPixelBudget is the total raster one document may hold across every
// layer and mask (DocumentLimits.documentPixelBudget): a quarter of physical
// memory at 4 bytes a pixel, never below one surface (200 MP) and never above
// 800 MP. Imports spend from this budget; a single surface stays capped by
// MaxSurfacePixels.
func DocumentPixelBudget() int {
	budgetOnce.Do(func() {
		total := physicalMemoryBytes()
		budget = int(math.Min(800_000_000, math.Max(float64(MaxSurfacePixels), float64(total/16))))
	})
	return budget
}

var (
	budgetOnce sync.Once
	budget     int
)

// memoryStatusEx mirrors MEMORYSTATUSEX (kernel32).
type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// physicalMemoryBytes reads installed physical memory; 0 falls back to the
// 200 MP floor through DocumentPixelBudget's max.
func physicalMemoryBytes() uint64 {
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	result, _, _ := proc.Call(uintptr(unsafe.Pointer(&status)))
	if result == 0 {
		return 0
	}
	return status.TotalPhys
}
