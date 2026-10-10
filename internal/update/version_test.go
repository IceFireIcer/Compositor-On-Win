package update

import "testing"

// 版本比较（票 50）：feed 是外部数据，任何输入都要有确定结果。
func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.2.0", "0.2.0", 0},
		{"0.2.0", "0.1.9", 1},
		{"0.1.9", "0.2.0", -1},
		{"v0.2.0", "0.2.0", 0},
		{"1.0", "1.0.0", 0},
		{"1.0.1", "1.0", 1},
		{"2.0.0", "1.9.9", 1},
		{"1.2.3-beta.1", "1.2.3", -1},
		{"1.2.3", "1.2.3-beta.1", 1},
		{"1.2.3-beta.2", "1.2.3-beta.1", 1},
		{"1.2.3-beta.10", "1.2.3-beta.9", 1},
		{"1.2.3-alpha", "1.2.3-beta", -1},
		{"1.2.3-rc.1", "1.2.3-rc.1", 0},
		{"1.2.3+build.7", "1.2.3+build.9", 0},
		{"", "0.0.0", 0},
		{"garbage", "0.0.0", 0},
		{"0.2.0", "garbage", 1},
	}
	for _, tc := range cases {
		if got := CompareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareVersions(%q,%q) = %d，想要 %d", tc.a, tc.b, got, tc.want)
		}
		if got := CompareVersions(tc.b, tc.a); got != -tc.want {
			t.Errorf("反向 CompareVersions(%q,%q) = %d，想要 %d", tc.b, tc.a, got, -tc.want)
		}
	}
}

func TestNewerThan(t *testing.T) {
	if !NewerThan("0.2.0", "0.1.0") {
		t.Fatal("0.2.0 应比 0.1.0 新")
	}
	if NewerThan("0.1.0", "0.1.0") {
		t.Fatal("同版本不算新")
	}
	if NewerThan("0.1.0", "0.2.0") {
		t.Fatal("更旧的版本不算新")
	}
}
