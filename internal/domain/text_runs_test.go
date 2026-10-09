package domain

// Ticket 45: the shared per-run predicate. Spans are UTF-16 ranges of
// Content, so the boundary cases are stated in UTF-16 units — an emoji is two
// of them and each hal of a surrogate pair is a legal span boundary.

import (
	"math"
	"strings"
	"testing"
)

func TestTextUTF16Len(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"世界", 2},
		{"a😀b", 4}, // the surrogate pair counts as two units
		{"😀😀", 4},
	}
	for _, c := range cases {
		if got := TextUTF16Len(c.in); got != c.want {
			t.Fatalf("TextUTF16Len(%q) = %d，想要 %d", c.in, got, c.want)
		}
	}
	// The exported helper must agree with the unexported original.
	if TextUTF16Len("a😀b") != utf16Len("a😀b") {
		t.Fatal("TextUTF16Len 与 utf16Len 不一致")
	}
}

func TestTextStyleValidateRuns(t *testing.T) {
	color := func(location, length int) TextColorRun {
		return TextColorRun{Location: location, Length: length, Red: 1}
	}
	font := func(location, length int, name string) TextFontRun {
		return TextFontRun{Location: location, Length: length, FontName: name}
	}
	longName := strings.Repeat("F", 201)

	cases := []struct {
		name    string
		style   TextStyle
		wantErr string // 错误子串；空表示应通过
	}{
		{"nil runs 通过", TextStyle{Content: "Hello"}, ""},
		{"两种 run 各一", TextStyle{Content: "Hello world",
			ColorRuns: []TextColorRun{color(0, 5)},
			FontRuns:  []TextFontRun{font(6, 5, "Arial")}}, ""},
		{"相邻 run 通过", TextStyle{Content: "Hello",
			ColorRuns: []TextColorRun{color(0, 2), color(2, 3)}}, ""},
		{"emoji 整个代理对", TextStyle{Content: "a😀b",
			ColorRuns: []TextColorRun{color(1, 2)}}, ""},
		{"emoji 半个代理对也是合法单元范围", TextStyle{Content: "a😀b",
			ColorRuns: []TextColorRun{color(2, 1)}}, ""},
		{"end == UTF-16 长度", TextStyle{Content: "a😀b",
			ColorRuns: []TextColorRun{color(3, 1)}}, ""},
		{"覆盖 emoji 到尾", TextStyle{Content: "a😀b",
			ColorRuns: []TextColorRun{color(1, 3)}}, ""},
		{"end 超出 UTF-16 长度一个单元", TextStyle{Content: "a😀b",
			ColorRuns: []TextColorRun{color(1, 4)}}, "超出文本范围"},
		{"按 rune 计会漏判的跨度", TextStyle{Content: "😀",
			ColorRuns: []TextColorRun{color(0, 3)}}, "超出文本范围"},
		{"重叠", TextStyle{Content: "Hello",
			ColorRuns: []TextColorRun{color(0, 3), color(2, 2)}}, "范围重叠"},
		{"未按序", TextStyle{Content: "Hello",
			ColorRuns: []TextColorRun{color(3, 1), color(0, 1)}}, "范围重叠"},
		{"负位置", TextStyle{Content: "Hello",
			ColorRuns: []TextColorRun{color(-1, 2)}}, "范围重叠"},
		{"零长度", TextStyle{Content: "Hello",
			ColorRuns: []TextColorRun{color(0, 0)}}, "长度必须为正"},
		{"负长度", TextStyle{Content: "Hello",
			ColorRuns: []TextColorRun{color(0, -2)}}, "长度必须为正"},
		{"空数组", TextStyle{Content: "Hello", ColorRuns: []TextColorRun{}}, "空数组"},
		{"容不下任何 run 的空内容", TextStyle{Content: "",
			ColorRuns: []TextColorRun{color(0, 1)}}, "超出文本范围"},
		{"颜色 NaN", TextStyle{Content: "Hi",
			ColorRuns: []TextColorRun{{Location: 0, Length: 1, Red: math.NaN()}}}, "颜色超出 0–1"},
		{"颜色 Inf", TextStyle{Content: "Hi",
			ColorRuns: []TextColorRun{{Location: 0, Length: 1, Green: math.Inf(1)}}}, "颜色超出 0–1"},
		{"颜色大于 1", TextStyle{Content: "Hi",
			ColorRuns: []TextColorRun{{Location: 0, Length: 1, Blue: 1.5}}}, "颜色超出 0–1"},
		{"颜色小于 0", TextStyle{Content: "Hi",
			ColorRuns: []TextColorRun{{Location: 0, Length: 1, Red: -0.01}}}, "颜色超出 0–1"},
		{"颜色端点 0 与 1", TextStyle{Content: "Hi",
			ColorRuns: []TextColorRun{{Location: 0, Length: 2, Red: 0, Green: 1, Blue: 0}}}, ""},
		{"fontRuns 合法", TextStyle{Content: "Hello",
			FontRuns: []TextFontRun{font(0, 5, "Helvetica")}}, ""},
		{"fontRuns 空名", TextStyle{Content: "Hello",
			FontRuns: []TextFontRun{font(0, 2, "")}}, "字体名无效"},
		{"fontRuns 超过 200 字符", TextStyle{Content: "Hello",
			FontRuns: []TextFontRun{font(0, 2, longName)}}, "字体名无效"},
		{"fontRuns 恰好 200 字符", TextStyle{Content: "Hello",
			FontRuns: []TextFontRun{font(0, 2, strings.Repeat("F", 200))}}, ""},
		{"fontRuns 换行", TextStyle{Content: "Hello",
			FontRuns: []TextFontRun{font(0, 2, "Ari\nal")}}, "字体名无效"},
		{"fontRuns 回车", TextStyle{Content: "Hello",
			FontRuns: []TextFontRun{font(0, 2, "Ari\rl")}}, "字体名无效"},
		{"fontRuns 空数组", TextStyle{Content: "Hello", FontRuns: []TextFontRun{}}, "空数组"},
		{"fontRuns 重叠", TextStyle{Content: "Hello",
			FontRuns: []TextFontRun{font(0, 3, "Arial"), font(1, 2, "Arial")}}, "范围重叠"},
		{"fontRuns 超出文本", TextStyle{Content: "Hi",
			FontRuns: []TextFontRun{font(1, 9, "Arial")}}, "超出文本范围"},
	}
	for _, c := range cases {
		err := c.style.ValidateRuns()
		if c.wantErr == "" {
			if err != nil {
				t.Fatalf("%s 应通过，得到 %v", c.name, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("%s 应被拒绝", c.name)
		}
		if !strings.Contains(err.Error(), c.wantErr) {
			t.Fatalf("%s 错误 = %q，缺少 %q", c.name, err, c.wantErr)
		}
	}
}
