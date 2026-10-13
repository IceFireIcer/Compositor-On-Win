import { mount } from "svelte";
import "./app.css";
import App from "./App.svelte";

/**
 * 全局错误兜底（票 52 的运行时可见性）。
 *
 * 为什么必须有这个：正式版里 WebView2 的 console 是**结构性不可见**的——
 * Wails v2.16 / go-webview2 都没有 console-message 通道，CDP 的
 * `--remote-debugging-port` 也被 go-webview2 的环境变量覆盖逻辑挡掉。
 * 于是"未捕获异常 / WebGPU 初始化失败 / 组件里一句 console.error"在用户
 * 那边只表现为**界面没反应或画布空白，不留任何日志**，没法排查。
 *
 * 这里把两类全局错误转发给 Go 侧的 `ReportFrontendError`，由它打到 stdout
 * （固定前缀 `FRONTEND_ERROR:`），启动脚本就能 grep 到。
 *
 * 约束：上报本身**绝不能**再抛一次（否则就是错误处理把应用打崩），所以
 * 动态 import + 全程 try/catch，失败静默。
 */
function describe(value: unknown): { message: string; stack: string } {
  if (value instanceof Error) {
    return { message: value.message || String(value), stack: value.stack ?? "" };
  }
  try {
    return { message: typeof value === "string" ? value : JSON.stringify(value), stack: "" };
  } catch {
    return { message: String(value), stack: "" };
  }
}

// nativeConsoleError 在下面的日志 tap 替换 console.error **之前**取好：
// 全局兜底仍要能把一份写进 DevTools，而又不能让 tap 把它自己这一行再转发回
// Go（那就是死循环）。
const nativeConsoleError = console.error.bind(console);

/**
 * 上报队列：串行 drain，而不是"正在上报就丢掉"。
 *
 * 旧版用一个 reporting 布尔挡重入，代价是**上报在途期间产生的 warn/error
 * 会被静默丢弃** —— 而"一次失败引发连锁 warn"恰恰是最需要看全的场景。
 * 现在改成有界 FIFO：进队即返回，drain 里逐条 await；队列满（说明 Go 侧
 * 长时间不可用）就丢最旧的，避免内存无界增长。
 */
const reportQueue: { source: string; message: string; stack: string }[] = [];
const reportQueueLimit = 500;
let draining = false;

function sendRaw(source: string, message: string, stack: string): void {
  reportQueue.push({ source, message, stack });
  if (reportQueue.length > reportQueueLimit) reportQueue.shift();
  void drainReports();
}

async function drainReports(): Promise<void> {
  if (draining) return;
  draining = true;
  try {
    while (reportQueue.length > 0) {
      const item = reportQueue.shift()!;
      try {
        const { ReportFrontendError } = await import("../wailsjs/go/bridge/Service");
        await ReportFrontendError(item.source, item.message, item.stack);
      } catch {
        // 没有 Wails 运行时（纯 vite dev / 单测）或绑定失败：静默。
        return; // 桥不可用就别空转，剩下的留在队列里等下一次
      }
    }
  } finally {
    draining = false;
  }
}

function report(source: string, value: unknown): void {
  const { message, stack } = describe(value);
  // 控制台也留一份：`wails dev` 下 DevTools 能看到。
  nativeConsoleError(`[${source}]`, value);
  sendRaw(source, message, stack);
}

/**
 * 日志模式：把 `console.warn` / `console.error` 也转发到 Go 的 stdout。
 *
 * 为什么需要：正式版的 WebView2 console 不可见（见文件头），而应用里
 * "用户可见的失败"绝大多数走的是 `console.warn("填充失败", err)` 这类分支
 * —— 界面只是没反应，stdout 里一行都没有，运行时验收只能靠肉眼猜。
 * 装上这个 tap 之后，每一次失败都会在启动日志里留下
 * `FRONTEND_INFO: console.warn | 填充失败 …` 一行。
 *
 * 上限 200 条：一次失败风暴不该把日志刷爆；tap 本身失败（无 Wails 运行时）
 * 静默，绝不影响控制台原行为。
 */
const consoleForwardLimit = 200;
let consoleForwarded = 0;
let consoleDropped = 0;

function forwardConsole(level: "warn" | "error", args: unknown[]): void {
  if (consoleForwarded >= consoleForwardLimit) {
    // 到顶之后不再静默：第一条被丢时补一行汇总，之后的条数挂在下一行尾部，
    // 否则"日志里没有失败"会被误读成"没有失败"。
    consoleDropped++;
    if (consoleDropped === 1) {
      sendRaw("info:console.warn", `console 转发已达上限 ${consoleForwardLimit} 条，后续只计数`, "");
    }
    return;
  }
  consoleForwarded++;
  const message = args.map((a) => describe(a).message).join(" ");
  const err = args.find((a): a is Error => a instanceof Error);
  const suffix = consoleDropped > 0 ? ` [dropped=${consoleDropped}]` : "";
  consoleDropped = 0;
  // warn 走 FRONTEND_INFO（是"诊断"，不是崩溃）；error 走 FRONTEND_ERROR。
  sendRaw(level === "warn" ? "info:console.warn" : "console.error", message + suffix, err?.stack ?? "");
}

function installConsoleTap(): void {
  (["warn", "error"] as const).forEach((level) => {
    const original = console[level].bind(console);
    console[level] = (...args: unknown[]) => {
      original(...args);
      forwardConsole(level, args);
    };
  });
}

installConsoleTap();

window.addEventListener("error", (event) => {
  report("window.error", event.error ?? event.message);
});

window.addEventListener("unhandledrejection", (event) => {
  report("unhandledrejection", event.reason);
});

const app = mount(App, {
  target: document.getElementById("app")!,
});

export default app;
