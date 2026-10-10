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

let reporting = false;

function report(source: string, value: unknown): void {
  const { message, stack } = describe(value);
  // 控制台也留一份：`wails dev` 下 DevTools 能看到。
  console.error(`[${source}]`, value);
  // 防止"上报失败 → 触发 error → 再上报"的死循环。
  if (reporting) return;
  reporting = true;
  void (async () => {
    try {
      const { ReportFrontendError } = await import("../wailsjs/go/bridge/Service");
      await ReportFrontendError(source, message, stack);
    } catch {
      // 没有 Wails 运行时（纯 vite dev / 单测）或绑定失败：静默。
    } finally {
      reporting = false;
    }
  })();
}

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
