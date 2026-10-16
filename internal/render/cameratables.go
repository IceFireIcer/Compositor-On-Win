package render

// Camera Raw 的测量表（票 78）。上游把它作为 Resources/CameraRawTables.bin 随应用
// 分发，并在包初始化时读一次、之后常驻（CameraRawTables.swift:30-31）。这里同样：
// `go:embed` 打进二进制，第一次用到时解码一次并缓存。

// 文件格式（CameraRawTables.swift:36-64）：
//
//	"CRT3" + size(uint16 LE) + count(uint16 LE) + step(uint16 LE)   共 10 字节头
//	其后是 xz 压缩的 count × size³ × 3 字节**运行差值**
//
// 压缩是 xz 容器（载荷以 FD 37 7A 58 5A 00 开头）：Apple 的 NSData
// .decompressed(using: .lzma) 实际解的就是 xz，尽管枚举叫 .lzma。
//
// 差值按「表 → 通道」分块，每块内是逐格的 int8 累加值（自然回绕）。实际取值 =
// 该轴上的基准级 + 累加值 × step，夹到 0…255；基准级把 0…255 按 size 等分取整。
// 索引次序是红最慢、其次绿、蓝最快（:30）。

// 来源：reference/Swift/Compositor/Resources/CameraRawTables.bin（pin e8c9e29），
// 逐字节拷贝。它是从 Photoshop 的 Camera Raw Filter 量出来的数据，无法在本仓重算，
// 所以只能随包带；重新量取需要 Photoshop 与 CameraRawTables.swift 里那张取值网格。

import (
	"bytes"
	_ "embed"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/ulikunitz/xz"
)

//go:embed assets/CameraRawTables.bin
var cameraRawTablesBin []byte

const (
	// cameraRawTableSize and cameraRawTableCount are the geometry the stages
	// are written against. The header has to agree, or this is a different
	// resource than the one the code was written for.
	cameraRawTableSize  = 17
	cameraRawTableCount = 763
)

// cameraRawTableSet is the decoded resource: the tables plus the geometry and
// the level axis every lookup needs.
type cameraRawTableSet struct {
	size    int
	count   int
	step    int
	entries int
	// levels is the 0…255 value each grid index stands for.
	levels []uint8
	// tables is count × entries, three bytes a colour, red slowest.
	tables []uint8
}

// tableAt is the table at index, or nil when the index is out of range.
func (s *cameraRawTableSet) tableAt(index int) []uint8 {
	if s == nil || index < 0 || index >= s.count {
		return nil
	}
	return s.tables[index*s.entries : (index+1)*s.entries]
}

// loadCameraRawTables decodes the resource. It is the guard upstream's load()
// runs: magic, geometry, decompressed length — anything off means nil there,
// and an error here so the failure is visible instead of silently disabling
// every table-driven stage.
func loadCameraRawTables(data []byte) (*cameraRawTableSet, error) {
	if len(data) <= 10 {
		return nil, fmt.Errorf("Camera Raw 表资源为空或过短")
	}
	if string(data[:4]) != "CRT3" {
		return nil, fmt.Errorf("Camera Raw 表资源标识不符: %q", data[:4])
	}
	size := int(binary.LittleEndian.Uint16(data[4:6]))
	count := int(binary.LittleEndian.Uint16(data[6:8]))
	step := int(binary.LittleEndian.Uint16(data[8:10]))
	if size != cameraRawTableSize || count != cameraRawTableCount || step <= 0 {
		return nil, fmt.Errorf("Camera Raw 表几何不符: size=%d count=%d step=%d", size, count, step)
	}
	reader, err := xz.NewReader(bytes.NewReader(data[10:]))
	if err != nil {
		return nil, fmt.Errorf("Camera Raw 表解压失败: %w", err)
	}
	cells := size * size * size
	entries := cells * 3
	planes := make([]byte, count*entries)
	if _, err := io.ReadFull(reader, planes); err != nil {
		return nil, fmt.Errorf("Camera Raw 表数据长度不符: %w", err)
	}
	levels := make([]uint8, size)
	for i := range levels {
		levels[i] = uint8(math.Round(float64(i) * 255 / float64(size-1)))
	}
	tables := make([]uint8, count*entries)
	for table := 0; table < count; table++ {
		for channel := 0; channel < 3; channel++ {
			base := (table*3 + channel) * cells
			var running uint8
			for cell := 0; cell < cells; cell++ {
				// Wrapping add: the packed deltas rely on uint8 overflow.
				running += planes[base+cell]
				axis := cell % size
				if channel == 0 {
					axis = cell / (size * size)
				} else if channel == 1 {
					axis = (cell / size) % size
				}
				value := int(levels[axis]) + int(int8(running))*step
				tables[table*entries+cell*3+channel] = uint8(min(255, max(0, value)))
			}
		}
	}
	return &cameraRawTableSet{size: size, count: count, step: step, entries: entries, levels: levels, tables: tables}, nil
}

var (
	cameraRawTablesOnce sync.Once
	cameraRawTables     *cameraRawTableSet
	cameraRawTablesErr  error
)

// CameraRawTables is the decoded table set, decoding the embedded resource on
// first use and keeping it for the process's life, as the original reads it
// once (CameraRawTables.swift:31).
func CameraRawTables() (*cameraRawTableSet, error) {
	cameraRawTablesOnce.Do(func() {
		cameraRawTables, cameraRawTablesErr = loadCameraRawTables(cameraRawTablesBin)
	})
	return cameraRawTables, cameraRawTablesErr
}

// ValidateCameraRawTables decodes the resource now instead of on first use, so
// a build with a missing or mismatched table resource says so at startup
// rather than the first time someone moves a Camera Raw slider.
func ValidateCameraRawTables() error {
	_, err := CameraRawTables()
	return err
}
