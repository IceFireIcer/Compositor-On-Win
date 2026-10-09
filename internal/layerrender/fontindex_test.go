package layerrender

import (
	"reflect"
	"strings"
	"testing"
)

// 枚举必须给出 Windows 字体目录里的实际面孔（CI 上字体目录为空时跳过，
// 与 TestResolveSystemFont 的宽容策略一致）。
func TestListInstalledFontsEnumerates(t *testing.T) {
	faces := ListInstalledFonts()
	if len(faces) == 0 {
		t.Skip("本机字体索引为空")
	}
	for _, f := range faces {
		if f.Family == "" || f.PostScriptName == "" {
			t.Fatalf("字体条目缺字段: %+v", f)
		}
	}
}

// 排序（家族、字重，均忽略大小写）与按 PostScript 名去重；两次调用结果
// 必须一致（稳定排序 + 去重不依赖 map 迭代顺序）。
func TestListInstalledFontsSortedAndDeduped(t *testing.T) {
	faces := ListInstalledFonts()
	if len(faces) == 0 {
		t.Skip("本机字体索引为空")
	}
	for i := 1; i < len(faces); i++ {
		prev, cur := faces[i-1], faces[i]
		pf, cf := strings.ToLower(prev.Family), strings.ToLower(cur.Family)
		if cf < pf {
			t.Fatalf("字体未按家族排序: %q 在 %q 之后", cur.Family, prev.Family)
		}
		if cf == pf {
			ps, cs := strings.ToLower(prev.Subfamily), strings.ToLower(cur.Subfamily)
			if cs < ps {
				t.Fatalf("同家族未按字重排序: %q/%q 在 %q/%q 之后",
					cur.Family, cur.Subfamily, prev.Family, prev.Subfamily)
			}
		}
	}
	seen := map[string]bool{}
	for _, f := range faces {
		key := strings.ToLower(f.PostScriptName)
		if seen[key] {
			t.Fatalf("PostScript 名重复: %q", f.PostScriptName)
		}
		seen[key] = true
	}
	if again := ListInstalledFonts(); !reflect.DeepEqual(faces, again) {
		t.Fatal("两次枚举结果不一致")
	}
}

// 本机存在的已知字体应出现在清单里；缺失时跳过而不是失败。
func TestListInstalledFontsIncludesKnownFamily(t *testing.T) {
	faces := ListInstalledFonts()
	if len(faces) == 0 {
		t.Skip("本机字体索引为空")
	}
	found := false
	subfamilies := 0
	for _, f := range faces {
		if f.Subfamily != "" {
			subfamilies++
		}
		if strings.EqualFold(f.Family, "Segoe UI") {
			found = true
		}
	}
	if !found {
		t.Skip("本机没有 Segoe UI")
	}
	// name ID 2 至少对部分字体存在；Windows 自带字体应能解出字重名。
	if subfamilies == 0 {
		t.Error("枚举结果没有任何字重名（sfnt.NameIDSubfamily）")
	}
	// 枚举与 Resolve 共用同一索引：清单里的家族必须能命中。
	if _, ok := Resolve("Segoe UI"); !ok {
		t.Error("枚举中的 Segoe UI 应能被 Resolve 命中")
	}
}
