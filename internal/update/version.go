package update

import (
	"strconv"
	"strings"
)

// CompareVersions 比较两个版本号：a<b 返回 -1，a==b 返回 0，a>b 返回 1。
//
// 接受 "1.2.3"、"v1.2.3"、任意段数的数字段，以及 semver 的预发布后缀
// （"1.2.3-beta.1"）：预发布**小于**同号正式版，预发布之间按标识逐个比较
// （数字标识按数值、其余按字典序）。构建元数据（"+build"）不参与比较。
// 解析不了的段按 0 处理，保证任何输入都有确定结果（feed 是外部数据）。
func CompareVersions(a, b string) int {
	an, apre := splitVersion(a)
	bn, bpre := splitVersion(b)
	for i := 0; i < len(an) || i < len(bn); i++ {
		var av, bv int
		if i < len(an) {
			av = an[i]
		}
		if i < len(bn) {
			bv = bn[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	// 数字段相同：有预发布的小。
	switch {
	case apre == "" && bpre == "":
		return 0
	case apre == "":
		return 1
	case bpre == "":
		return -1
	}
	return comparePrerelease(apre, bpre)
}

// splitVersion 拆出数字段与预发布串。
func splitVersion(v string) ([]int, string) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(v), "v"))
	if i := strings.IndexByte(trimmed, '+'); i >= 0 {
		trimmed = trimmed[:i] // 构建元数据不参与比较
	}
	pre := ""
	if i := strings.IndexByte(trimmed, '-'); i >= 0 {
		pre = trimmed[i+1:]
		trimmed = trimmed[:i]
	}
	parts := strings.Split(trimmed, ".")
	nums := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			n = 0
		}
		nums = append(nums, n)
	}
	return nums, pre
}

// comparePrerelease 按 semver 规则比较两个预发布串。
func comparePrerelease(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		if i >= len(as) {
			return -1 // 段少的小
		}
		if i >= len(bs) {
			return 1
		}
		an, aerr := strconv.Atoi(as[i])
		bn, berr := strconv.Atoi(bs[i])
		switch {
		case aerr == nil && berr == nil:
			if an != bn {
				if an < bn {
					return -1
				}
				return 1
			}
		case aerr == nil:
			return -1 // 数字标识小于字母标识
		case berr == nil:
			return 1
		default:
			if as[i] != bs[i] {
				if as[i] < bs[i] {
					return -1
				}
				return 1
			}
		}
	}
	return 0
}

// NewerThan 报告 candidate 是否比 current 新。
func NewerThan(candidate, current string) bool {
	return CompareVersions(candidate, current) > 0
}
