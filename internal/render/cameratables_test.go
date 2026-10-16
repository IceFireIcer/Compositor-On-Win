package render

// 票 78：Camera Raw 测量表的加载与解码。

import (
	"encoding/binary"
	"testing"
)

func TestCameraRawTablesResourceHasTheDeclaredGeometry(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatalf("随包资源应当能解码: %v", err)
	}
	if set.size != cameraRawTableSize || set.count != cameraRawTableCount || set.step <= 0 {
		t.Fatalf("几何 = (%d, %d, %d)", set.size, set.count, set.step)
	}
	if set.entries != cameraRawTableSize*cameraRawTableSize*cameraRawTableSize*3 {
		t.Fatalf("每张表 %d 字节", set.entries)
	}
	if len(set.tables) != set.count*set.entries {
		t.Fatalf("表数据 %d 字节，想要 %d", len(set.tables), set.count*set.entries)
	}
	// 基准级把 0…255 按 size 等分取整。
	if set.levels[0] != 0 || set.levels[set.size-1] != 255 {
		t.Fatalf("基准级端点 = %d…%d，想要 0…255", set.levels[0], set.levels[set.size-1])
	}
	if set.levels[1] != 16 { // round(255/16) = 16
		t.Fatalf("第二个基准级 = %d，想要 16", set.levels[1])
	}
}

// 解码正确性的硬证据：**不变的表必须真的是恒等**。运行差值累加或轴映射写错的话，
// 这些表就会偏离它们自己的基准级。
func TestCameraRawIdentityTablesAreIdentity(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	// 滑杆取 0 的那一张：曝光 −5…5 步长 0.5（0 在第 11 张）、白/黑/饱和/自然饱和
	// 各 −100…100 步长 25（0 在各自的第 5 张）。
	for _, tc := range []struct {
		name  string
		start int
		step  int
	}{
		{"曝光 0", 81, 10},
		{"白色 0", 102, 4},
		{"黑色 0", 111, 4},
		{"饱和度 0", 120, 4},
		{"自然饱和度 0", 129, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			table := set.tableAt(tc.start + tc.step)
			if table == nil {
				t.Fatal("表下标越界")
			}
			size := set.size
			worst := 0
			worstCell := 0
			for cell := 0; cell < size*size*size; cell++ {
				for channel := 0; channel < 3; channel++ {
					axis := cell % size
					if channel == 0 {
						axis = cell / (size * size)
					} else if channel == 1 {
						axis = (cell / size) % size
					}
					deviation := int(table[cell*3+channel]) - int(set.levels[axis])
					if deviation < 0 {
						deviation = -deviation
					}
					if deviation > worst {
						worst, worstCell = deviation, cell
					}
				}
			}
			t.Logf("偏离基准级最大 %d 级（cell %d）", worst, worstCell)
			// 随包的这份资源上实测**恰好 0 级**；留 1 级给将来重新量取时的抖动，
			// 但解码或轴映射写错会远不止 1 级。
			if worst > 1 {
				t.Fatalf("不变的表偏离基准级 %d 级 —— 解码或轴映射不对", worst)
			}
		})
	}
}

func TestCameraRawTableHeaderValidation(t *testing.T) {
	valid := append([]byte(nil), cameraRawTablesBin...)
	cases := []struct {
		name string
		data []byte
	}{
		{"过短", valid[:8]},
		{"标识不符", append([]byte("NOPE"), valid[4:]...)},
	}
	// 几何不符：直接把头里的 count 改掉。
	badCount := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint16(badCount[6:8], uint16(cameraRawTableCount-1))
	cases = append(cases, struct {
		name string
		data []byte
	}{"几何不符", badCount})
	// 步长为 0 同样要被拒。
	badStep := append([]byte(nil), valid...)
	binary.LittleEndian.PutUint16(badStep[8:10], 0)
	cases = append(cases, struct {
		name string
		data []byte
	}{"步长为零", badStep})
	// 载荷不是 LZMA。
	badBody := append([]byte(nil), valid...)
	for i := 10; i < len(badBody); i++ {
		badBody[i] = 0xFF
	}
	cases = append(cases, struct {
		name string
		data []byte
	}{"载荷损坏", badBody})

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := loadCameraRawTables(c.data); err == nil {
				t.Fatal("应当报错")
			}
		})
	}
}

func TestCameraRawTableAtBounds(t *testing.T) {
	set, err := CameraRawTables()
	if err != nil {
		t.Fatal(err)
	}
	if set.tableAt(-1) != nil || set.tableAt(set.count) != nil {
		t.Fatal("越界应返回 nil")
	}
	if got := set.tableAt(0); len(got) != set.entries {
		t.Fatalf("第 0 张表 %d 字节", len(got))
	}
	if set.tableAt(0)[0] != set.tables[0] {
		t.Fatal("tableAt(0) 应当指向表数据的开头")
	}
}
