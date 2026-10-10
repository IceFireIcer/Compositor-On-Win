package bridge

// 前端错误上报测试：正式版里 WebView2 的 console 完全不可见，前端未捕获异常
// 只能靠这条 stdout 通道留痕。契约是"一行一条、固定前缀、必限长"——启动脚本
// 与运行时审计都靠 grep "FRONTEND_ERROR:" 采集。

import (
	"bytes"
	"strings"
	"testing"
)

// captureFrontendError swaps the stdout seam for a buffer so the test never
// races the real stdout of the test binary.
func captureFrontendError(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	old := frontendErrorWriter
	frontendErrorWriter = buf
	t.Cleanup(func() { frontendErrorWriter = old })
	return buf
}

func TestReportFrontendErrorPrintsAGreppableLine(t *testing.T) {
	svc := NewService(NewWorkspace())
	buf := captureFrontendError(t)

	out, err := svc.ReportFrontendError("window.onerror", "boom", "at foo (app.js:1:2)")
	if err != nil {
		t.Fatalf("上报失败不能再引发一次前端异常，error = %v", err)
	}
	if out == "" {
		t.Fatal("回复不应为空")
	}
	want := "FRONTEND_ERROR: window.onerror | boom | at foo (app.js:1:2)\n"
	if got := buf.String(); got != want {
		t.Fatalf("输出 = %q，想要 %q", got, want)
	}
	t.Logf("真实输出: %s", strings.TrimSuffix(buf.String(), "\n"))
}

// TestReportFrontendErrorRealisticWebGPU sample: the failure the runtime audit
// was written for — a WebGPU bring-up that throws before any document exists.
func TestReportFrontendErrorRealisticWebGPU(t *testing.T) {
	svc := NewService(NewWorkspace())
	buf := captureFrontendError(t)

	stack := "Error: WebGPU 适配器请求失败\n    at initGPU (assets/index-7f3a.js:1:20431)\n    at async main (assets/index-7f3a.js:1:882)"
	if _, err := svc.ReportFrontendError("unhandledrejection", "TypeError: Cannot read properties of null (reading 'requestDevice')", stack); err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSuffix(buf.String(), "\n")
	if strings.Count(line, "\n") != 0 {
		t.Fatalf("真实输出不是一行: %q", line)
	}
	if !strings.HasPrefix(line, "FRONTEND_ERROR: unhandledrejection | ") {
		t.Fatalf("真实输出前缀不对: %q", line)
	}
	t.Logf("真实输出: %s", line)
}

func TestReportFrontendErrorFallsBackToUnknownSource(t *testing.T) {
	svc := NewService(NewWorkspace())

	for _, source := range []string{"", "   ", "\t"} {
		buf := captureFrontendError(t)
		if _, err := svc.ReportFrontendError(source, "boom", "trace"); err != nil {
			t.Fatal(err)
		}
		line := buf.String()
		if !strings.HasPrefix(line, "FRONTEND_ERROR: unknown | ") {
			t.Fatalf("空 source %q 的输出 = %q，想要 unknown 兜底", source, line)
		}
	}
}

func TestReportFrontendErrorFlattensNewlines(t *testing.T) {
	svc := NewService(NewWorkspace())
	buf := captureFrontendError(t)

	if _, err := svc.ReportFrontendError("unhandledrejection", "line1\nline2", "a\r\nb\tc"); err != nil {
		t.Fatal(err)
	}
	line := buf.String()
	if strings.Count(line, "\n") != 1 {
		t.Fatalf("上报必须一行一条，得到 %q", line)
	}
	if !strings.Contains(line, `line1\nline2`) {
		t.Fatalf("message 的换行没有转义: %q", line)
	}
	if !strings.Contains(line, `a\r\nb\tc`) {
		t.Fatalf("stack 的换行/制表符没有转义: %q", line)
	}
}

func TestReportFrontendErrorTruncatesALongStack(t *testing.T) {
	svc := NewService(NewWorkspace())
	buf := captureFrontendError(t)

	stack := strings.Repeat("x", frontendErrorStackLimit*3)
	if _, err := svc.ReportFrontendError("window.onerror", "boom", stack); err != nil {
		t.Fatal(err)
	}
	line := buf.String()
	if strings.Count(line, "\n") != 1 {
		t.Fatalf("上报必须一行一条，得到 %d 行", strings.Count(line, "\n"))
	}
	if len(line) > 4096 {
		t.Fatalf("输出 %d 字节，超出单条上限", len(line))
	}
	if !strings.Contains(line, "...") {
		t.Fatal("被截断的 stack 应以 ... 结尾")
	}
	parts := strings.Split(strings.TrimSuffix(line, "\n"), " | ")
	if len(parts) != 3 {
		t.Fatalf("输出格式 = %q", line)
	}
	if len(parts[2]) > frontendErrorStackLimit {
		t.Fatalf("stack 段 %d 字节，超过 %d 上限", len(parts[2]), frontendErrorStackLimit)
	}
}

func TestReportFrontendErrorTruncatesALongMessage(t *testing.T) {
	svc := NewService(NewWorkspace())
	buf := captureFrontendError(t)

	message := strings.Repeat("m", frontendErrorMessageLimit*2)
	if _, err := svc.ReportFrontendError("window.onerror", message, "trace"); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimSuffix(buf.String(), "\n"), " | ")
	if len(parts) != 3 {
		t.Fatalf("输出格式 = %q", buf.String())
	}
	if len(parts[1]) > frontendErrorMessageLimit {
		t.Fatalf("message 段 %d 字节，超过 %d 上限", len(parts[1]), frontendErrorMessageLimit)
	}
}

func TestReportFrontendErrorNeedsNoDocument(t *testing.T) {
	// 启动期崩溃时还没有文档，这条通道必须照样能用。
	svc := NewService(NewWorkspace())
	buf := captureFrontendError(t)
	if _, err := svc.ReportFrontendError("startup", "WebGPU 初始化失败", ""); err != nil {
		t.Fatalf("没有文档时也必须能上报: %v", err)
	}
	if got := buf.String(); got != "FRONTEND_ERROR: startup | WebGPU 初始化失败 | \n" {
		t.Fatalf("输出 = %q", got)
	}
}

func TestReportFrontendErrorSurvivesPathologicalInput(t *testing.T) {
	svc := NewService(NewWorkspace())
	buf := captureFrontendError(t)

	huge := strings.Repeat("坏\n\r\t", 100000)
	if _, err := svc.ReportFrontendError("", huge, huge); err != nil {
		t.Fatal(err)
	}
	line := buf.String()
	if strings.Count(line, "\n") != 1 {
		t.Fatalf("上报必须一行一条，得到 %d 行", strings.Count(line, "\n"))
	}
	if len(line) > 8192 {
		t.Fatalf("病态输入下输出 %d 字节，没有限住", len(line))
	}
}
