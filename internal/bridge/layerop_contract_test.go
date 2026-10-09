package bridge

// 票 47 的"假接线"防线：前端 runLayerOp("op") 发出去的每个 op 名字，
// planLayerOp 必须有对应的 case —— 否则动作注册了、快捷键也能触发，但每次
// 按下只会得到"未知操作"的错误（document.ts 的 runLayerOp 吞掉错误返回
// false），前端测试完全看不出来（它只断言动作函数存在）。反向同理：Go 里
// 有、前端从不发的 op 是死代码。
//
// 放在 Go 侧是因为 CI 与本地都跑 go test，且这里不需要 node 类型；两边都从
// 源码里读，不维护第三份清单。

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// frontendOpSources are the frontend files that name a LayerOp.
var frontendOpSources = []string{
	"frontend/src/lib/state/document.ts",
	"frontend/src/lib/state/filters.ts",
	"frontend/src/lib/shortcuts/actions.ts",
	"frontend/src/lib/components/LayersPanel.svelte",
}

var frontendOpRe = regexp.MustCompile(`runLayerOp\(\s*"([A-Za-z]+)"`)

// backendOps extracts planLayerOp's case labels: only that switch is scanned,
// since layer_commands.go's effect kinds (stroke/shadow/…) are cases too but
// are not op names.
func backendOps(t *testing.T) map[string]bool {
	t.Helper()
	source, err := os.ReadFile("document.go")
	if err != nil {
		t.Fatalf("读取 document.go: %v", err)
	}
	body := string(source)
	start := strings.Index(body, "func planLayerOp(")
	if start < 0 {
		t.Fatal("document.go 里找不到 planLayerOp")
	}
	if end := strings.Index(body[start:], "\n}\n"); end >= 0 {
		body = body[start : start+end]
	} else {
		body = body[start:]
	}
	ops := map[string]bool{}
	for _, m := range regexp.MustCompile(`case "([A-Za-z]+)":`).FindAllStringSubmatch(body, -1) {
		ops[m[1]] = true
	}
	return ops
}

// frontendOps extracts the ops the frontend actually sends: the string
// literals of runLayerOp("…") calls.
func frontendOps(t *testing.T) map[string]bool {
	t.Helper()
	ops := map[string]bool{}
	for _, rel := range frontendOpSources {
		source, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			if os.IsNotExist(err) {
				continue // the file may move; the op list still comes from the rest
			}
			t.Fatalf("读取 %s: %v", rel, err)
		}
		for _, m := range frontendOpRe.FindAllStringSubmatch(string(source), -1) {
			ops[m[1]] = true
		}
	}
	if len(ops) == 0 {
		t.Fatal("没有从任何前端源码里读到 runLayerOp 的 op 名字（路径或正则失效了？）")
	}
	return ops
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestLayerOpNamesMatchBetweenFrontendAndBackend(t *testing.T) {
	backend := backendOps(t)
	frontend := frontendOps(t)

	var missing []string
	for op := range frontend {
		if !backend[op] {
			missing = append(missing, op)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("planLayerOp 缺少这些 op 的 case：%v（前端发了但 Go 不认，运行时只会 warn）", missing)
	}

	var unused []string
	for op := range backend {
		if !frontend[op] {
			unused = append(unused, op)
		}
	}
	sort.Strings(unused)
	if len(unused) > 0 {
		t.Fatalf("这些 op 没有任何前端调用方：%v", unused)
	}
	t.Logf("LayerOp 契约：%v", sortedKeys(frontend))
}

// The ticket-47 command family was briefly wired on the frontend while the
// backend had no cases at all; this pins the ops that must exist.
func TestTicket47LayerCommandOpsExist(t *testing.T) {
	backend := backendOps(t)
	for _, op := range []string{
		"duplicateLayer", "groupLayers", "ungroupLayer", "setClippingMask",
		"deleteLayerMask", "deleteEffect", "mergeLayers", "nudgeLayer",
	} {
		if !backend[op] {
			t.Errorf("planLayerOp 缺少 %s", op)
		}
	}
}
