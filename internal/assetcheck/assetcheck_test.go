package assetcheck

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// repoRoot is two levels up from internal/assetcheck.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// 票 91：仓库里带的资产自证一致 —— 嵌入的表与上游逐字节相同、拷贝的 C 内核都
// 自证来源、许可证在位、没有把 macOS 专属工程当成 Windows 运行时依赖。
func TestRepositoryAssetsAreConsistent(t *testing.T) {
	issues := Verify(repoRoot(t))
	lines := make([]string, 0, len(issues))
	for _, issue := range issues {
		lines = append(lines, issue.String())
	}
	sort.Strings(lines)
	if len(lines) > 0 {
		t.Fatalf("资产检查发现 %d 处问题:\n  %s", len(lines), strings.Join(lines, "\n  "))
	}
}

// 检查本身要能抓到东西：指向一个空目录时，缺资产、缺许可证都该报出来。
func TestVerifyReportsAMissingTree(t *testing.T) {
	issues := Verify(t.TempDir())
	if len(issues) == 0 {
		t.Fatal("空目录上什么都没报 —— 检查没有判别力")
	}
	wanted := []string{cameraRawTablesPath, "LICENSE"}
	for _, path := range wanted {
		found := false
		for _, issue := range issues {
			if issue.Path == path {
				found = true
			}
		}
		if !found {
			t.Errorf("空目录上应当报出 %s 缺失", path)
		}
	}
}

// 来源区域解析：注释标记去掉、跨行路径接起来，而且只读文件顶部那一段。
func TestProvenanceRegionJoinsWrappedLinesAndStopsAtTheWindow(t *testing.T) {
	wrapped := "// Verbatim copy of the Scanlines portion of reference/Swift/Compositor/\n" +
		"// Rendering/DitherPixels.c (ticket 60). Source commit\n" +
		"// e8c9e29701528413cc0e608d61187c1c61790add\n" +
		"#ifndef X_h\n#define X_h\n" +
		"// below the guard\n" +
		"void f(void);\n"
	region := provenanceRegion(wrapped)
	if !strings.Contains(region, "reference/Swift/Compositor/ Rendering/DitherPixels.c") {
		t.Errorf("跨行的路径应当接起来，得到 %q", region)
	}
	if !commitPattern.MatchString(region) {
		t.Errorf("提交号应当在区域里，得到 %q", region)
	}

	// 窗口之外的东西不算：正文深处的路径不该被当成来源。
	far := strings.Repeat("int filler;\n", provenanceLines+2) + "// reference/Swift/Compositor/Rendering/Late.c\n"
	if strings.Contains(provenanceRegion(far), "Late.c") {
		t.Errorf("窗口之外不该算来源，得到 %q", provenanceRegion(far))
	}
}

// 嵌入的表被改一个字节就该被抓住 —— 用真实的文件副本，不是造一个假目录。
func TestVerifyCatchesADriftedEmbeddedTable(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(repoRoot(t), cameraRawTablesPath)
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(root, cameraRawTablesPath)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	drifted := append([]byte(nil), data...)
	drifted[len(drifted)-1] ^= 0xff
	if err := os.WriteFile(dst, drifted, 0o644); err != nil {
		t.Fatal(err)
	}
	issues := Verify(root)
	found := false
	for _, issue := range issues {
		if issue.Path == cameraRawTablesPath && strings.Contains(issue.Detail, "sha256") {
			found = true
		}
	}
	if !found {
		t.Fatalf("改动过的嵌入表应当被摘要检查抓住，得到 %v", issues)
	}
}
