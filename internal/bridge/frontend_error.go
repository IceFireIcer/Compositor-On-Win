package bridge

// 前端错误可见性（运行时审计补丁）：正式版里 WebView2 的 console 完全不可见
// —— Wails v2.16 / go-webview2 都没有 console-message 通道，CDP 的
// --remote-debugging-port 也被 go-webview2 的环境变量覆盖逻辑挡掉。前端
// main.ts 注册的 window.onerror / unhandledrejection 兜底因此需要一条能把
// 错误打到 stdout 的通道，否则用户那边只表现为"界面没反应/画布空白"。
//
// 输出固定一行（启动脚本与运行时审计靠 grep "FRONTEND_ERROR:" 采集）：
//
//	FRONTEND_ERROR: <source> | <message> | <stack>
//
// 与 launch.go 的 LAUNCH_TO_READY_SECONDS 同走 stdout；这里多一层 writer 接缝
// （默认 os.Stdout），让测试不必去抢测试进程自己的 stdout。

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	// frontendErrorPrefix is the grep anchor of the report line.
	frontendErrorPrefix = "FRONTEND_ERROR: "
	// frontendInfoPrefix is the informational channel's anchor, and
	// frontendInfoSourcePrefix the source prefix that selects it: a normal
	// startup diagnostic (a GPU probe, say) should not be logged as an error,
	// but it travels the same stdout channel because the WebView2 console is
	// invisible in release builds.
	frontendInfoPrefix       = "FRONTEND_INFO: "
	frontendInfoSourcePrefix = "info:"
	// frontendErrorStackLimit / frontendErrorMessageLimit cap one report so a
	// runaway stack trace cannot flood the log.
	frontendErrorStackLimit   = 4000
	frontendErrorMessageLimit = 2000
	// frontendErrorSourceLimit is a safety cap on the source field.
	frontendErrorSourceLimit = 200
	// frontendErrorUnknownSource is the fallback for a blank source (an older
	// frontend build, or an error before the handlers were installed).
	frontendErrorUnknownSource = "unknown"
	// frontendErrorEllipsis marks a clipped field.
	frontendErrorEllipsis = "..."
)

// frontendErrorWriter is the stdout seam; production keeps os.Stdout.
var frontendErrorWriter io.Writer = os.Stdout

// ReportFrontendError forwards one uncaught frontend error to stdout as a
// single greppable line:
//
//	FRONTEND_ERROR: <source> | <message> | <stack>
//
// Newlines, carriage returns and tabs inside the fields become their
// two-character escapes, so one report is always one line; a blank source falls
// back to "unknown"; the stack is clipped to frontendErrorStackLimit and the
// message to frontendErrorMessageLimit.
//
// It deliberately never returns an error: reporting must not raise a second
// frontend exception, so every input is accepted. The reply is a small JSON
// acknowledgement the caller can ignore.
func (s *Service) ReportFrontendError(source, message, stack string) (string, error) {
	prefix := frontendErrorPrefix
	if trimmed := strings.TrimSpace(source); strings.HasPrefix(trimmed, frontendInfoSourcePrefix) {
		prefix = frontendInfoPrefix
		source = strings.TrimSpace(strings.TrimPrefix(trimmed, frontendInfoSourcePrefix))
	}
	if strings.TrimSpace(source) == "" {
		source = frontendErrorUnknownSource
	}
	fmt.Fprintf(frontendErrorWriter, "%s%s | %s | %s\n",
		prefix,
		escapeLogField(source, frontendErrorSourceLimit),
		escapeLogField(message, frontendErrorMessageLimit),
		escapeLogField(stack, frontendErrorStackLimit))
	return "{\"logged\":true}", nil
}

// escapeLogField flattens one field to a single line and clips it to limit
// bytes on a rune boundary, appending "..." when it was cut.
func escapeLogField(s string, limit int) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) <= limit {
		return out
	}
	cut := limit - len(frontendErrorEllipsis)
	for cut > 0 && !utf8.RuneStart(out[cut]) {
		cut--
	}
	return out[:cut] + frontendErrorEllipsis
}
