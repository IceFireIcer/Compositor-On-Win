// Package domain holds the document model and its invariants.
// Ticket 02 starts with the document limits; the full model lands in
// ticket 04 around these constants.
package domain

import (
	"fmt"
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

// MaxDocumentPixels is the ceiling on the whole document's raster: 800 MP is
// 3.2 GB of RGBA8 layers, which a 16 GB machine already reaches.
//
// Two different ceilings are in play and must not be confused:
//
//	MaxSurfacePixels  = 200 MP — one surface (canvas, export, filter target, mask render)
//	MaxDocumentPixels = 800 MP — every layer and mask of one document together
//
// Both are below MaxSide*MaxSide = 900 MP, which is what makes "a square at
// MaxSide is still oversized" true (several tests express "too large" that way;
// it is the largest area the side limit can describe).
const MaxDocumentPixels = 800_000_000

// DocumentPixelBudget is the total raster one document may hold across every
// layer and mask (DocumentLimits.documentPixelBudget): a quarter of physical
// memory at 4 bytes a pixel, never below one surface (200 MP) and never above
// 800 MP. Imports spend from this budget; a single surface stays capped by
// MaxSurfacePixels.
func DocumentPixelBudget() int {
	budgetOnce.Do(func() {
		budget = DocumentPixelBudgetFor(ReadMemoryStatus().Total)
	})
	return budget
}

// DocumentPixelBudgetFor is the budget as a pure function of installed physical
// memory, so the RAM/16 semantics can be tested without the machine's actual
// RAM. totalBytes == 0 (the OS call failed) lands on the floor, like the Swift
// max(...) does.
//
// The units are pixels, assuming 4 bytes per pixel and giving the document a
// quarter of memory. 8 GiB therefore yields 537 MP — the number the macOS
// source quotes — and 12 GiB already hits the 800 MP ceiling.
func DocumentPixelBudgetFor(totalBytes uint64) int {
	quarter := totalBytes / 16
	switch {
	case quarter < uint64(MaxSurfacePixels):
		return MaxSurfacePixels
	case quarter > MaxDocumentPixels:
		return MaxDocumentPixels
	default:
		return int(quarter)
	}
}

var (
	budgetOnce sync.Once
	budget     int
)

// MemoryStatus is the machine's physical memory picture, read from
// GlobalMemoryStatusEx. Available is what Windows says is free right now; both
// are 0 when the call fails.
type MemoryStatus struct {
	Total     uint64
	Available uint64
}

// ReadMemoryStatus samples physical memory. Cheap enough to poll (one
// syscall), which is how the memory-pressure watcher works.
func ReadMemoryStatus() MemoryStatus {
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	result, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if result == 0 {
		return MemoryStatus{}
	}
	return MemoryStatus{Total: status.TotalPhys, Available: status.AvailPhys}
}

// globalMemoryStatusEx 只解析一次：内存采样会被 5 秒一次的轮询反复调用，没必要每次
// 都重新走一遍 LazyDLL 查找。
var globalMemoryStatusEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")

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
