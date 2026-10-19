import { afterAll, beforeAll, beforeEach, expect, it, vi } from "vitest";
import { createServer } from "vite";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import path from "node:path";

// Browser updates can exceed Vitest's 1s default under concurrent CI load.
const poll = (callback) => expect.poll(callback, { timeout: 10000 });
vi.setConfig({ testTimeout: 30000 });
const require = createRequire(import.meta.url);
// Install separately with npm install --prefix <PLAYWRIGHT_ROOT> playwright.
const { chromium } = require(path.join(process.env.PLAYWRIGHT_ROOT ?? path.join(tmpdir(), "compositor-playwright-smoke"), "node_modules", "playwright"));
let server, browser, page;

beforeAll(async () => {
  // Concurrent browser fixtures must not overwrite each other's Svelte cache.
  server = await createServer({ cacheDir: "node_modules/.vite-toolbar-browser", server: { port: 0, strictPort: false }, optimizeDeps: { noDiscovery: true, include: [] }, plugins: [{ name: "tool-options-browser-fixture", configureServer(vite) {
    vite.middlewares.use("/tool-options-test", (_req, res) => {
      res.setHeader("Content-Type", "text/html");
      res.end('<!doctype html><html><body><div id="bar" style="width:320px"></div></body></html>');
    });
  } }] });
  await server.listen();
  browser = await chromium.launch({ executablePath: process.env.EDGE_PATH ?? "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe", headless: true });
}, 30000);

afterAll(async () => {
  await browser?.close();
  await server?.close();
}, 30000);

beforeEach(async () => {
  await page?.close();
  page = await browser.newPage();
  await page.goto(`${server.resolvedUrls.local[0]}tool-options-test`);
  // A string keeps Vitest's SSR transform out of the browser imports.
  await page.evaluate(`(async () => {
    const { mount, tick } = await import("/node_modules/.vite-toolbar-browser/deps/svelte.js");
    const { default: Bar } = await import("/src/lib/components/ToolOptionsBar.svelte");
    const { activeTool } = await import("/src/lib/state/tools.ts");
    window.setTool = (tool) => activeTool.set(tool);
    mount(Bar, { target: document.getElementById("bar") });
    await tick();
  })()`);
  await poll(() => page.locator(".options").count()).toBe(1);
});
// 量一次工具栏在给定宽度下的几何。
async function measure(width) {
  await page.evaluate((w) => { document.getElementById("bar").style.width = `${w}px`; }, width);
  await page.evaluate(() => new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  return page.evaluate(() => {
    const bar = document.querySelector(".options");
    return {
      scrollWidth: bar.scrollWidth,
      clientWidth: bar.clientWidth,
      scrollHeight: bar.scrollHeight,
      clientHeight: bar.clientHeight,
    };
  });
}

it("scrolls horizontally at a narrow width instead of squashing its controls", async () => {
  // 票 92：工具选项栏可横向滚动且没有多余滚动条。
  // 用画笔：它是选项最多的一支，默认工具那点控件在 240px 下本来就放得下。
  await page.evaluate(() => window.setTool("brush"));
  await poll(() => page.locator(".options").textContent()).toMatch(/Diameter/);
  const narrow = await measure(240);
  // 横向：内容比客户区宽，所以才需要横滚 —— 控件是被挤到外面而不是被压扁。
  expect(narrow.scrollWidth).toBeGreaterThan(narrow.clientWidth);
  // 纵向：横滚条不该把内容挤出第二条（竖向）滚动条。
  expect(narrow.scrollHeight).toBeLessThanOrEqual(narrow.clientHeight + 1);

  // 每个控件都还有实际宽度（没有被压成 0）。
  const collapsed = await page.evaluate(() =>
    [...document.querySelectorAll(".options label, .options input, .options select")]
      .filter((el) => el.getBoundingClientRect().width < 1)
      .map((el) => el.className || el.tagName));
  expect(collapsed).toEqual([]);
});

it("does not scroll at all when there is room", async () => {
  const wide = await measure(1600);
  // 宽窗口下不该出现任何滚动条：scrollWidth 与 scrollHeight 都不超过客户区。
  expect(wide.scrollWidth).toBeLessThanOrEqual(wide.clientWidth + 1);
  expect(wide.scrollHeight).toBeLessThanOrEqual(wide.clientHeight + 1);
});

// 票 53/V06：工具栏的数值选项改用 NumericField —— 拖标签改值、↑/↓ 步进（Shift ×10）、
// 输入解析并钳制，三种语义都来自 numeric.ts。这里钉的是「真的接上了」，纯函数本身由
// numeric.test.ts 覆盖。
async function brushDiameter() {
  // 字符串形式求值：函数体里的动态 import 会被 Vitest 的 SSR 转换破坏。
  return page.evaluate(`(async () => {
    const { brushSettings } = await import("/src/lib/state/tools.ts");
    let diameter = null;
    brushSettings.subscribe((settings) => { diameter = settings.diameter; })();
    return diameter;
  })()`);
}

it("scrubs, steps and clamps the numeric tool options", async () => {
  await page.evaluate(() => window.setTool("brush"));
  await poll(() => page.locator(".options").textContent()).toMatch(/Diameter/);
  // 放宽工具栏，避免 320px 下横滚把后面的字段裁出可视区、鼠标坐标落空。
  await page.evaluate(() => { document.getElementById("bar").style.width = "1600px"; });

  const field = page.locator(".numeric-field").filter({ hasText: "Diameter" });
  const input = field.locator("input");
  const label = field.locator("label");
  await poll(() => input.inputValue()).toBe("40");

  // 拖标签：右移 7px → 40 + 7 = 47（灵敏度 1、步进 1，和 TransformInspector 同一套语义）。
  const box = await label.boundingBox();
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 7, box.y + box.height / 2, { steps: 4 });
  await page.mouse.up();
  await poll(() => input.inputValue()).toBe("47");
  // 输入框显示的是 store 回写的结果，所以这条同时证明 onchange 真的写进了 store。
  await poll(() => brushDiameter()).toBe(47);

  // ↑ 加一步，Shift+↑ 走十步（ArrowStepper 把步长乘十，不是调细），↓ 减一步。
  await input.focus();
  await page.keyboard.press("ArrowUp");
  await poll(() => input.inputValue()).toBe("48");
  await page.keyboard.press("Shift+ArrowUp");
  await poll(() => input.inputValue()).toBe("58");
  await page.keyboard.press("ArrowDown");
  await poll(() => input.inputValue()).toBe("57");
  await poll(() => brushDiameter()).toBe(57);

  // 输入超范围：钳到 2000，不回绕。
  //
  // 注意：store 自己也会把直径夹在 1…2000，所以这条是**行为**断言（用户看到的是钳制结果，
  // 不是 99999）；direct() 的钳制逻辑本身由 numeric.test.ts 的单测钉住。
  await input.fill("99999");
  await input.blur();
  await poll(() => input.inputValue()).toBe("2000");
  await poll(() => brushDiameter()).toBe(2000);

  // 解析不出来（清空）就保留旧值，而不是写进 NaN 或 0。
  await input.fill("");
  await input.blur();
  await poll(() => input.inputValue()).toBe("2000");
  await poll(() => brushDiameter()).toBe(2000);
});

// 工具栏那条「钳到 2000」是行为断言：store 自己也会把直径夹在 1…2000，所以它证明不了
// NumericField 在调用 onchange 之前就钳好了。这里直接挂一个 NumericField，用回调记录它交给
// 父组件的值 —— 这才是组件级钳制的判别点。
it("clamps typed input before handing it to its parent", async () => {
  await page.evaluate(`(async () => {
    const { mount, tick } = await import("/node_modules/.vite-toolbar-browser/deps/svelte.js");
    const { default: NumericField } = await import("/src/lib/components/NumericField.svelte");
    const host = document.createElement("div");
    host.id = "numeric-field";
    document.body.append(host);
    window.__numericChanges = [];
    mount(NumericField, {
      target: host,
      props: {
        label: "Tolerance",
        value: 32,
        min: 0,
        max: 255,
        step: 1,
        onchange: (value) => window.__numericChanges.push(value),
      },
    });
    await tick();
  })()`);
  const input = page.locator("#numeric-field input");
  await poll(() => input.inputValue()).toBe("32");

  // 超上界：onchange 收到的是钳制后的 255，不是 999。
  await input.fill("999");
  await input.blur();
  await poll(() => page.evaluate(() => window.__numericChanges.at(-1))).toBe(255);
  await poll(() => input.inputValue()).toBe("255");

  // 解析不出来：onchange 不该被调用，显示回到旧值。
  await input.fill("");
  await input.blur();
  await poll(() => input.inputValue()).toBe("32");
  expect(await page.evaluate(() => window.__numericChanges.length)).toBe(1);
});
