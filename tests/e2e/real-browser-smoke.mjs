#!/usr/bin/env node

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import { createRequire } from "node:module";
import { createHash } from "node:crypto";
import { classifyWailsOverlayDiagnostics } from "./wails-overlay-error.mjs";

const require = createRequire(import.meta.url);

const DEFAULT_URL = "http://127.0.0.1:34115";
const DEFAULT_EDGE = "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe";
const DEFAULT_TIMEOUT = 10_000;
const DEFAULT_RUN_TIMEOUT = 180_000;
const PREVIEW_ACK_TIMEOUT = 5_000;

class BackendResetError extends Error {
  constructor(message) {
    super(message);
    this.name = "BackendResetError";
  }
}

function parseArgs(argv) {
  const result = { baseUrl: DEFAULT_URL, edgePath: DEFAULT_EDGE, headed: false, runTimeoutMs: DEFAULT_RUN_TIMEOUT };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--headed") {
      result.headed = true;
      continue;
    }
    if (arg === "--base-url" || arg === "--edge-path" || arg === "--playwright-root" || arg === "--evidence-dir" || arg === "--run-timeout-ms") {
      const value = argv[i + 1];
      if (!value) throw new Error(`${arg} requires a value`);
      i += 1;
      if (arg === "--base-url") result.baseUrl = value.replace(/\/$/, "");
      if (arg === "--edge-path") result.edgePath = value;
      if (arg === "--playwright-root") result.playwrightRoot = value;
      if (arg === "--evidence-dir") result.evidenceDir = value;
      if (arg === "--run-timeout-ms") {
        result.runTimeoutMs = Number(value);
        if (!Number.isInteger(result.runTimeoutMs) || result.runTimeoutMs <= 0) {
          throw new Error("--run-timeout-ms must be a positive integer");
        }
      }
      continue;
    }
    if (arg === "--help" || arg === "-h") {
      console.log([
        "Usage: node tests/e2e/real-browser-smoke.mjs [options]",
        "  --base-url URL          Wails browser server (default http://127.0.0.1:34115)",
        "  --edge-path PATH        Edge executable path",
        "  --playwright-root PATH Isolated directory containing node_modules/playwright",
        "  --evidence-dir PATH     Directory for JSON and screenshots",
        `  --run-timeout-ms N      Maximum run time before the browser is closed (default ${DEFAULT_RUN_TIMEOUT})`,
        "  --headed                Show the Playwright Edge window",
      ].join("\n"));
      process.exit(0);
    }
    throw new Error(`Unknown argument: ${arg}`);
  }
  return result;
}

function loadPlaywright(root) {
  const candidates = [];
  if (root) candidates.push(path.join(root, "node_modules", "playwright"));
  candidates.push("playwright");
  let lastError;
  for (const candidate of candidates) {
    try {
      return require(candidate);
    } catch (error) {
      lastError = error;
    }
  }
  throw new Error(
    `Playwright is not available. Run scripts/smoke/run-real-browser.ps1 so it can install an isolated copy. ${lastError}`,
  );
}

function errorText(error) {
  return error instanceof Error ? error.message : String(error);
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function withTimeout(promise, timeout, label) {
  let timer;
  try {
    return await Promise.race([
      promise,
      new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error(`${label} exceeded ${timeout}ms`)), timeout);
      }),
    ]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}

async function waitUntil(predicate, message, timeout = DEFAULT_TIMEOUT, interval = 100) {
  const deadline = Date.now() + timeout;
  let lastError = null;
  while (Date.now() < deadline) {
    try {
      const value = await predicate();
      if (value) return value;
    } catch (error) {
      if (error instanceof BackendResetError) throw error;
      lastError = error;
    }
    await sleep(interval);
  }
  const suffix = lastError ? `; last error: ${errorText(lastError)}` : "";
  if (lastError instanceof BackendResetError) throw lastError;
  throw new Error(`${message}${suffix}`);
}

function parseJSON(raw, label) {
  try {
    return typeof raw === "string" ? JSON.parse(raw) : raw;
  } catch (error) {
    throw new Error(`${label} was not valid JSON: ${errorText(error)}`);
  }
}

function parseEnvelope(raw, label = "document envelope") {
  const value = parseJSON(raw, label);
  if (!value || typeof value !== "object") throw new Error(`${label} is not an object`);
  if (!Number.isInteger(value.rev)) throw new Error(`${label} has no integer rev`);
  return value;
}

function byteValues(value) {
  if (value === null || value === undefined) return [];
  if (Array.isArray(value)) return value;
  if (typeof value === "object" && Array.isArray(value.data)) return value.data;
  if (typeof value === "object" && Array.isArray(value.bytes)) return value.bytes;
  if (typeof ArrayBuffer !== "undefined" && ArrayBuffer.isView(value)) return Array.from(value);
  if (typeof ArrayBuffer !== "undefined" && value instanceof ArrayBuffer) return Array.from(new Uint8Array(value));
  try { return Array.from(value); } catch { return []; }
}

function pngInfo(bytes) {
  const values = byteValues(bytes);
  const signature = [137, 80, 78, 71, 13, 10, 26, 10];
  const validSignature = signature.every((value, index) => values[index] === value);
  if (!validSignature || values.length < 24) return { validSignature, width: 0, height: 0, bytes: values.length };
  const u32 = (offset) => ((values[offset] << 24) >>> 0) + (values[offset + 1] << 16) + (values[offset + 2] << 8) + values[offset + 3];
  return { validSignature, width: u32(16), height: u32(20), bytes: values.length };
}

function selectValue(locator) {
  return locator.inputValue();
}

function pngHash(bytes) {
  return createHash("sha256").update(Buffer.from(bytes)).digest("hex");
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  const playwright = loadPlaywright(options.playwrightRoot);
  if (!fs.existsSync(options.edgePath)) throw new Error(`Edge executable not found: ${options.edgePath}`);

  const evidenceDir = options.evidenceDir
    ? path.resolve(options.evidenceDir)
    : fs.mkdtempSync(path.join(os.tmpdir(), "compositor-real-browser-smoke-"));
  fs.mkdirSync(evidenceDir, { recursive: true });
  const projectPath = path.join(evidenceDir, "smoke-project.comp");
  const reportPath = path.join(evidenceDir, "report.json");
  const screenshotPath = path.join(evidenceDir, "failure.png");
  const finalScreenshotPath = path.join(evidenceDir, "final.png");
  const canvasScreenshotPath = path.join(evidenceDir, "canvas.png");

  const report = {
    status: "running",
    startedAt: new Date().toISOString(),
    baseUrl: options.baseUrl,
    edgePath: options.edgePath,
    evidenceDir,
    projectPath,
    checks: [],
    interfaces: {},
    render: {},
    gpu: {},
    preview: { requests: [], heldDrags: [] },
    transform: { requests: [] },
    diagnostics: { console: [], pageErrors: [], pageErrorDetails: [], failedRequests: [], failedResponses: [], dialogs: [] },
    failures: [],
    inconclusive: [],
    knownGaps: [],
  };

  let context;
  let page;
  let workspaceLost = false;
  let runDeadlineTimer;
  let heldStroke = null;
  const runtimeSources = new Map();
  const responseTasks = new Set();

  const observeResponse = (task) => {
    responseTasks.add(task);
    void task.finally(() => responseTasks.delete(task));
  };

  const recordCheck = (name, status, detail = undefined) => {
    const entry = { name, status };
    if (detail !== undefined) entry.detail = detail;
    report.checks.push(entry);
    if (status === "failed") report.failures.push(name);
    if (status === "inconclusive") report.inconclusive.push(name);
  };

  const markWorkspaceLost = (error) => {
    if (workspaceLost) return;
    workspaceLost = true;
    report.interfaces.workspaceReset = {
      at: new Date().toISOString(),
      reason: errorText(error),
    };
    report.knownGaps.push("The Wails backend lost the active workspace during the run; dependent UI phases are inconclusive until the dev watcher is stable.");
  };

  const asBackendReset = (error) => {
    if (error instanceof BackendResetError) return error;
    const message = errorText(error);
    if (/没有打开的文档|no activeId|active workspace has no|active document mismatch|WebSocket is not open|Still in CONNECTING|connection refused|Execution context was destroyed|page has been closed/i.test(message)) {
      return new BackendResetError(message);
    }
    return null;
  };

  const phase = async (name, action, { requiresWorkspace = false } = {}) => {
    if (requiresWorkspace) {
      if (workspaceLost) {
        recordCheck(name, "inconclusive", "blocked by the lost Wails workspace; no feature assertion was attempted");
        return null;
      }
      try {
        await activeState();
      } catch (error) {
        const reset = asBackendReset(error);
        if (reset) {
          markWorkspaceLost(reset);
          recordCheck(name, "inconclusive", reset.message);
          return null;
        }
        recordCheck(name, "failed", errorText(error));
        return null;
      }
    }
    try {
      const detail = await action();
      recordCheck(name, "passed", detail);
      return detail;
    } catch (error) {
      const reset = asBackendReset(error);
      if (reset) {
        markWorkspaceLost(reset);
        recordCheck(name, "inconclusive", reset.message);
        return null;
      }
      recordCheck(name, "failed", errorText(error));
      return null;
    }
  };

  const callPlane = async (plane, method, args = []) => {
    const label = `${plane}.${method}`;
    try {
      return await withTimeout(page.evaluate(
        async ({ plane: planeName, method: methodName, args: methodArgs }) => {
          const group = window.go?.bridge?.[planeName];
          const fn = group?.[methodName];
          if (typeof fn !== "function") throw new Error(`window.go.bridge.${planeName}.${methodName} is unavailable`);
          return await fn(...methodArgs);
        },
        { plane, method, args },
      ), DEFAULT_TIMEOUT, `${label} bridge call`);
    } catch (error) {
      if (/exceeded \d+ms$/.test(errorText(error))) {
        throw new BackendResetError(`${label} bridge call timed out; Wails IPC did not return`);
      }
      throw error;
    }
  };

  const workspaceCall = (method, args = []) => callPlane("Workspace", method, args);
  const serviceCall = (method, args = []) => callPlane("Service", method, args);

  const documentEnvelope = async () => parseEnvelope(
    await workspaceCall("ActiveDocumentJSON"),
    "Workspace.ActiveDocumentJSON",
  );

  const workspaceSnapshot = async () => {
    const snapshot = parseJSON(await workspaceCall("Snapshot"), "Workspace.Snapshot");
    if (!snapshot || !Array.isArray(snapshot.tabs)) throw new Error("Workspace.Snapshot has no tabs array");
    return snapshot;
  };

  const activeState = async () => {
    const snapshot = await workspaceSnapshot();
    const tabID = snapshot.activeId;
    if (!tabID) throw new BackendResetError("Workspace.Snapshot has no activeId");
    let envelope;
    try {
      envelope = await documentEnvelope();
    } catch (error) {
      const reset = asBackendReset(error);
      if (reset) throw reset;
      throw error;
    }
    if (!envelope.doc || envelope.doc.documentID !== tabID) {
      throw new BackendResetError(`active document mismatch: tab=${tabID}, doc=${envelope.doc?.documentID ?? "none"}`);
    }
    return { snapshot, envelope, tabID };
  };

  const waitRevision = async (minimumRevision) => waitUntil(async () => {
    const current = await activeState();
    return current.envelope.rev > minimumRevision ? current : null;
  }, `document revision did not advance beyond ${minimumRevision}`);

  const fetchBytes = async (resourcePath) => withTimeout(page.evaluate(async (resource) => {
    const response = await fetch(resource, { cache: "no-store" });
    const bytes = new Uint8Array(await response.arrayBuffer());
    return {
      status: response.status,
      contentType: response.headers.get("content-type") ?? "",
      bytes: Array.from(bytes),
    };
  }, resourcePath), DEFAULT_TIMEOUT, `fetch ${resourcePath}`);

  const fetchJSON = async (resourcePath) => withTimeout(page.evaluate(async (resource) => {
    const response = await fetch(resource, { cache: "no-store" });
    const text = await response.text();
    return {
      status: response.status,
      contentType: response.headers.get("content-type") ?? "",
      text,
    };
  }, resourcePath), DEFAULT_TIMEOUT, `fetch ${resourcePath}`);

  const requirePNG = (reply, label, width = 64, height = 48) => {
    const info = pngInfo(reply.bytes);
    if (reply.status !== 200 || !/^image\/png(?:;|$)/i.test(reply.contentType) ||
        !info.validSignature || info.width !== width || info.height !== height) {
      throw new Error(`${label}: HTTP ${reply.status}, ${reply.contentType}, PNG ${JSON.stringify(info)}`);
    }
    return info;
  };

  const assertPinnedDocument = async (before, label) => {
    const during = await activeState();
    if (during.tabID !== before.tabID || during.envelope.rev !== before.envelope.rev ||
        JSON.stringify(during.envelope.doc) !== JSON.stringify(before.envelope.doc)) {
      throw new Error(`${label} modified the committed document before submission: rev ${before.envelope.rev} -> ${during.envelope.rev}`);
    }
  };

  const waitHeldPreview = async (kind, before, requestIndex) => {
    const deadline = Date.now() + PREVIEW_ACK_TIMEOUT;
    const withinDeadline = (promise) => withTimeout(promise, Math.max(1, deadline - Date.now()), `${kind} held drag preview acknowledgement`);
    while (Date.now() < deadline) {
      await withinDeadline(assertPinnedDocument(before, `${kind} held drag`));
      const mounted = await withinDeadline(page.locator('.doc img.render[src^="blob:"]').evaluateAll(async (images) => {
        const image = images.find((candidate) => candidate.complete && candidate.naturalWidth === 64 && candidate.naturalHeight === 48);
        if (!image) return null;
        const src = image.src;
        try {
          const response = await fetch(src);
          const bytes = Array.from(new Uint8Array(await response.arrayBuffer()));
          // A newer frame may replace and revoke this blob while it is read.
          return image.src === src ? { src, bytes } : null;
        } catch {
          return null;
        }
      }));
      // sequence 1 acknowledges Begin; require a Point acknowledgement too.
      const reply = report.preview.requests.slice(requestIndex).find((entry) =>
        entry.status === 200 && Number(new URL(entry.url).searchParams.get("sequence")) >= 2 &&
        entry.png?.validSignature && entry.png.width === 64 && entry.png.height === 48 &&
        /^image\/png(?:;|$)/i.test(entry.contentType) && mounted && entry.pngHash === pngHash(mounted.bytes));
      if (reply && mounted) {
        await withinDeadline(assertPinnedDocument(before, `${kind} acknowledged preview`));
        const detail = { kind, revision: before.envelope.rev, url: reply.url, status: reply.status, png: reply.png, pngHash: reply.pngHash, mounted: true };
        report.preview.heldDrags.push(detail);
        return detail;
      }
      await sleep(50);
    }
    throw new Error(`${kind} held drag did not mount an acknowledged HTTP 200 PNG within ${PREVIEW_ACK_TIMEOUT}ms; requests=${JSON.stringify(report.preview.requests.slice(requestIndex))}`);
  };

  const transformInspector = () => page.locator('[aria-label="Transform inspector"]');
  const readTransformDraft = async () => {
    const inspector = transformInspector();
    const number = async (label) => {
      const value = Number(await inspector.getByRole("spinbutton", { name: label, exact: true }).inputValue());
      if (!Number.isFinite(value)) throw new Error(`transform ${label} is not finite`);
      return value;
    };
    return {
      origin: [await number("X"), await number("Y")],
      size: [await number("W"), await number("H")],
      rotation: await number("Rotation"),
      flipX: await inspector.getByRole("button", { name: "Horizontal flip", exact: true }).getAttribute("aria-pressed") === "true",
      flipY: await inspector.getByRole("button", { name: "Vertical flip", exact: true }).getAttribute("aria-pressed") === "true",
      sampling: await inspector.getByRole("combobox", { name: "Sampling", exact: true }).inputValue(),
    };
  };

  const beginTransform = async () => {
    await clickTool("移动");
    // Keep the tiny fixture's interior separate from the 8-screen-pixel
    // resize hit targets, using the same zoom control as a user.
    for (let step = 0; (await docBox()).width < 256 && step < 12; step += 1) {
      await page.locator('.zoom-controls button[title="放大"]').click();
    }
    await page.keyboard.press("Control+t");
    await transformInspector().waitFor({ state: "visible", timeout: DEFAULT_TIMEOUT });
    await page.locator("svg.transform-box polygon.transform-frame").waitFor({ state: "visible", timeout: DEFAULT_TIMEOUT });
    return readTransformDraft();
  };

  const waitTransformPreview = async (before, requestIndex, baseline) => {
    const deadline = Date.now() + PREVIEW_ACK_TIMEOUT;
    const withinDeadline = (promise) => withTimeout(promise, Math.max(1, deadline - Date.now()), "transform preview acknowledgement");
    const layerID = before.envelope.doc.activeLayerID;
    while (Date.now() < deadline) {
      await withinDeadline(assertPinnedDocument(before, "transform preview"));
      const mounted = await withinDeadline(page.locator(".doc img.render").evaluateAll((images) => {
        const image = images.find((candidate) => new URL(candidate.src).searchParams.has("transform"));
        return image && image.complete && image.naturalWidth === 64 && image.naturalHeight === 48 ? image.src : null;
      }));
      const reply = report.transform.requests.slice(requestIndex).find((entry) => entry.url === mounted && entry.status === 200 && entry.png);
      if (reply) {
        const query = new URL(reply.url).searchParams;
        if (query.get("layerID") !== layerID || query.get("rev") !== String(before.envelope.rev)) {
          throw new Error(`transform preview context mismatch: ${reply.url}`);
        }
        const draft = await withinDeadline(readTransformDraft());
        const sent = parseJSON(query.get("transform"), "transform preview query");
        if (JSON.stringify(sent) !== JSON.stringify(draft)) {
          // A previously loaded intermediate frame is not the current draft.
          await sleep(50);
          continue;
        }
        const rendered = await withinDeadline(fetchBytes(reply.url));
        const png = requirePNG(rendered, "transform preview");
        if (pngHash(rendered.bytes) !== reply.pngHash) throw new Error("mounted transform response differs from the HTTP readback");
        if (Buffer.from(rendered.bytes).equals(Buffer.from(baseline.bytes))) throw new Error("transform preview pixels did not change from the committed render");
        await withinDeadline(assertPinnedDocument(before, "rendered transform draft"));
        return { url: reply.url, status: reply.status, png, pngHash: pngHash(rendered.bytes), draft, layerID, revision: before.envelope.rev };
      }
      await sleep(50);
    }
    throw new Error(`transform drag did not mount its HTTP 200 PNG within ${PREVIEW_ACK_TIMEOUT}ms`);
  };

  const assertCommittedTransform = (current, layerID, draft) => {
    const actual = current.envelope.doc.layers.find((layer) => layer.id === layerID)?.transform;
    if (!actual) throw new Error(`committed transform for ${layerID} is missing`);
    for (const key of ["origin", "size", "rotation", "flipX", "flipY", "sampling"]) {
      if (JSON.stringify(actual[key]) !== JSON.stringify(draft[key])) {
        throw new Error(`committed transform ${key}: ${JSON.stringify(actual[key])} != ${JSON.stringify(draft[key])}`);
      }
    }
  };

  const heldTransformDrag = async (before, baseline, from, to) => {
    const requestIndex = report.transform.requests.length;
    await page.mouse.move(from.x, from.y);
    await page.mouse.down();
    let preview;
    try {
      await page.mouse.move(to.x, to.y, { steps: 6 });
      preview = await waitTransformPreview(before, requestIndex, baseline);
    } finally {
      await page.mouse.up();
    }
    // Explicit Ctrl+T sessions must also stay uncommitted after release.
    await assertPinnedDocument(before, "Ctrl+T pointer release");
    if (!(await transformInspector().isVisible())) throw new Error("Ctrl+T session closed on pointer release");
    return preview;
  };

  const moveTransformDraft = async (before, baseline) => {
    const initial = await beginTransform();
    const frame = await page.locator("svg.transform-box polygon.transform-frame").boundingBox();
    if (!frame || Math.min(frame.width, frame.height) <= 16) throw new Error("transform frame has no usable interior");
    const box = await docBox();
    const from = { x: frame.x + frame.width / 2, y: frame.y + frame.height / 2 };
    const to = { x: from.x + box.width * 0.12, y: from.y + box.height * 0.10 };
    const preview = await heldTransformDrag(before, baseline, from, to);
    if (JSON.stringify(preview.draft.origin) === JSON.stringify(initial.origin) ||
        JSON.stringify(preview.draft.size) !== JSON.stringify(initial.size) || preview.draft.rotation !== initial.rotation) {
      throw new Error(`interior drag did not translate the layer: ${JSON.stringify({ initial, draft: preview.draft })}`);
    }
    return preview;
  };

  const docBox = async () => {
    const box = await page.locator(".canvas-stage .doc").boundingBox();
    if (!box || box.width < 2 || box.height < 2) throw new Error("document gesture rectangle is not visible");
    return box;
  };

  const gesture = async (from, to) => {
    await page.mouse.move(from.x, from.y);
    await page.mouse.down();
    try {
      await page.mouse.move(to.x, to.y, { steps: 6 });
    } finally {
      await page.mouse.up();
    }
  };

  const clickDocument = async (xRatio = 0.5, yRatio = 0.5) => {
    const box = await docBox();
    await page.mouse.click(box.x + box.width * xRatio, box.y + box.height * yRatio);
  };

  const clickTool = async (titlePart) => {
    const button = page.locator(`nav[aria-label="工具"] button[title*="${titlePart}"]`).first();
    await button.waitFor({ state: "visible", timeout: DEFAULT_TIMEOUT });
    await button.click();
  };

  const clickMenuItem = async (menuLabel, itemMatcher) => {
    const nav = page.locator('nav[aria-label="菜单"]');
    await nav.getByRole("button", { name: menuLabel, exact: true }).click();
    const item = page.getByRole("menuitem").filter({ hasText: itemMatcher }).first();
    await item.waitFor({ state: "visible", timeout: DEFAULT_TIMEOUT });
    await item.click();
  };

  const callObjectSelection = async (tabID, x, y) => {
    // Wails runtime wrappers are variadic: Function.length is not Go arity.
    if ((await activeState()).tabID !== tabID) throw new Error("object selection tab changed");
    report.interfaces.selectObjectAtArgs = "x,y";
    return serviceCall("SelectObjectAt", [x, y]);
  };

  try {
    const { chromium } = playwright;
    if (!chromium) throw new Error("Playwright chromium launcher is unavailable");
    const profileDir = fs.mkdtempSync(path.join(os.tmpdir(), "compositor-edge-smoke-profile-"));
    context = await chromium.launchPersistentContext(profileDir, {
      executablePath: options.edgePath,
      headless: !options.headed,
      viewport: { width: 1280, height: 860 },
      args: ["--enable-unsafe-webgpu"],
    });
    page = context.pages()[0] ?? await context.newPage();
    page.setDefaultTimeout(DEFAULT_TIMEOUT);
    page.setDefaultNavigationTimeout(30_000);
    runDeadlineTimer = setTimeout(() => {
      const message = `smoke run exceeded ${options.runTimeoutMs}ms; closing the browser to preserve a bounded report`;
      if (!report.failures.includes(`fatal: ${message}`)) report.failures.push(`fatal: ${message}`);
      void context?.close().catch(() => {});
    }, options.runTimeoutMs);

    page.on("console", (message) => {
      if (message.type() === "warning" || message.type() === "error") {
        report.diagnostics.console.push({ type: message.type(), text: message.text(), url: message.location().url });
      }
    });
    page.on("pageerror", (error) => {
      // Runtime execution can precede response.text(); classify once its
      // actual source has arrived, retaining every message and stack.
      report.diagnostics.pageErrorDetails.push({ message: errorText(error), stack: error.stack ?? "" });
    });
    page.on("dialog", (dialog) => {
      report.diagnostics.dialogs.push({ type: dialog.type(), message: dialog.message() });
      void dialog.dismiss();
    });
    page.on("requestfailed", (request) => {
      const entry = { url: request.url(), method: request.method(), failure: request.failure()?.errorText ?? "unknown" };
      report.diagnostics.failedRequests.push(entry);
      const url = new URL(request.url());
      if (url.pathname.startsWith("/pixel/stroke/")) {
        report.preview.requests.push({ ...entry, status: null });
      }
      if (url.pathname.startsWith("/render/") && url.searchParams.has("transform")) {
        report.transform.requests.push({ ...entry, status: null });
      }
    });
    page.on("response", (response) => {
      const url = new URL(response.url());
      const entry = { url: response.url(), status: response.status(), contentType: response.headers()["content-type"] ?? "", heldStroke };
      const preview = url.pathname.startsWith("/pixel/stroke/");
      const transform = url.pathname.startsWith("/render/") && url.searchParams.has("transform");
      if (preview) report.preview.requests.push(entry);
      if (transform) report.transform.requests.push(entry);
      if ((preview || transform) && response.status() === 200) {
        observeResponse(response.body().then((bytes) => { entry.png = pngInfo(bytes); entry.pngHash = pngHash(bytes); })
          .catch((error) => { entry.bodyError = errorText(error); }));
      }
      if (url.origin === new URL(options.baseUrl).origin && url.pathname === "/wails/ipc.js" && entry.status === 200) {
        observeResponse(response.text().then((source) => { runtimeSources.set(response.url(), source); })
          .catch((error) => { report.diagnostics.runtimeSourceError = errorText(error); }));
      }
      // End retires the stroke token; only a late 404 after release is benign.
      const retiredStroke = preview && entry.status === 404 && heldStroke === null;
      if (entry.status >= 400 && !retiredStroke && url.pathname !== "/favicon.ico") {
        report.diagnostics.failedResponses.push(entry);
      }
    });

    await phase("navigate to Wails browser server", async () => {
      await page.goto(`${options.baseUrl}/`, { waitUntil: "domcontentloaded", timeout: 30_000 });
      await waitUntil(
        () => page.evaluate(() => typeof window.go?.bridge?.Workspace?.Snapshot === "function"),
        "window.go.bridge did not become available",
        30_000,
      );
      return { url: page.url() };
    });

    await phase("create a small document through the UI", async () => {
      const dialog = page.locator('[role="dialog"][aria-label="新建画布"]');
      await page.locator('nav[aria-label="菜单"]').waitFor({ state: "visible" });
      if (!(await dialog.isVisible())) await page.keyboard.press("Control+n");
      await dialog.waitFor({ state: "visible", timeout: DEFAULT_TIMEOUT });
      const fields = dialog.locator('input[type="number"]');
      if (await fields.count() !== 3) throw new Error(`new document dialog has ${await fields.count()} number inputs`);
      await fields.nth(0).fill("64");
      await fields.nth(1).fill("48");
      await fields.nth(2).fill("72");
      await dialog.getByRole("button", { name: "创建", exact: true }).click();
      await page.locator('[role="application"][aria-label="画布"]').waitFor({ state: "visible", timeout: DEFAULT_TIMEOUT });
      const state = await waitUntil(async () => {
        try { return await activeState(); } catch { return null; }
      }, "new document did not reach the active workspace", DEFAULT_TIMEOUT);
      if (state.envelope.doc.width !== 64 || state.envelope.doc.height !== 48) {
        throw new Error(`new document dimensions are ${state.envelope.doc.width}x${state.envelope.doc.height}`);
      }
      report.interfaces.tabID = state.tabID;
      return { tabID: state.tabID, width: state.envelope.doc.width, height: state.envelope.doc.height, rev: state.envelope.rev };
    });

    let state = null;
    try { state = await activeState(); } catch { /* The phase above owns the detailed failure. */ }
    if (state) {
      report.interfaces.tabID = state.tabID;

      await phase("validate render snapshot and PNG asset plane", async () => {
        let jsonReply = null;
        let snapshot = null;
        let jsonError = null;
        try {
          jsonReply = await fetchJSON(`/render/${encodeURIComponent(state.tabID)}.json?v=${state.envelope.rev}`);
          if (jsonReply.status !== 200) throw new Error(`/render JSON returned HTTP ${jsonReply.status}`);
          snapshot = parseJSON(jsonReply.text, "/render snapshot");
          if (snapshot.rev !== state.envelope.rev) throw new Error(`render snapshot rev ${snapshot.rev} != ${state.envelope.rev}`);
        } catch (error) {
          jsonError = errorText(error);
        }

        let pngReply = null;
        let info = null;
        let pngError = null;
        try {
          pngReply = await fetchBytes(`/render/${encodeURIComponent(state.tabID)}.png?v=${state.envelope.rev}`);
          info = requirePNG(pngReply, "HTTP render PNG");
        } catch (error) {
          pngError = errorText(error);
        }

        const imageAsset = Array.isArray(snapshot?.assets) ? snapshot.assets.find((asset) => asset.kind === "image") : null;
        let asset = null;
        let assetError = null;
        if (imageAsset?.url) {
          try {
            const assetReply = await fetchBytes(imageAsset.url);
            asset = { status: assetReply.status, bytes: assetReply.bytes.length, contentType: assetReply.contentType };
            if (assetReply.status !== 200 || assetReply.bytes.length === 0) throw new Error(`image asset returned HTTP ${assetReply.status}`);
          } catch (error) {
            assetError = errorText(error);
          }
        }

        // Pixel data belongs to the HTTP asset plane, not the JSON binding.
        // RenderPNG's Go multi-result return is an internal handler contract.
        report.render = {
          snapshot: jsonReply ? { status: jsonReply.status, assets: snapshot?.assets?.length ?? 0, error: jsonError } : { error: jsonError },
          endpointPNG: pngReply ? { status: pngReply.status, contentType: pngReply.contentType, ...info, error: pngError } : { error: pngError },
          imageAsset: { ...asset, error: assetError },
        };
        if (jsonError || pngError || assetError) {
          throw new Error(`render readback failed: ${JSON.stringify(report.render)}`);
        }
        return report.render;
      }, { requiresWorkspace: true });

      await phase("validate visible GPU or CPU render track", async () => {
        const track = await waitUntil(async () => {
          const text = await page.locator(".render-track").textContent().catch(() => null);
          return text === "GPU" || text === "CPU" ? text : null;
        }, "render track was not exposed in the status bar", 5_000);
        const gpu = await page.evaluate(async () => {
          const canvas = document.querySelector("canvas.render");
          const result = {
            navigatorGPU: typeof navigator.gpu !== "undefined",
            adapter: false,
            device: false,
            canvas: canvas ? { width: canvas.width, height: canvas.height, clientWidth: canvas.clientWidth, clientHeight: canvas.clientHeight } : null,
            webgpuContext: false,
          };
          if (canvas) {
            try { result.webgpuContext = !!canvas.getContext("webgpu"); } catch { result.webgpuContext = false; }
          }
          if (navigator.gpu) {
            try {
              const adapter = await navigator.gpu.requestAdapter({ powerPreference: "high-performance" });
              result.adapter = !!adapter;
              if (adapter) {
                const device = await adapter.requestDevice();
                result.device = !!device;
                device?.destroy?.();
              }
            } catch {
              // A CPU fallback is valid; the result records why only at app level.
            }
          }
          return result;
        });
        if (!gpu.canvas || gpu.canvas.width <= 0 || gpu.canvas.height <= 0) throw new Error("render canvas has no backing pixels");
        report.gpu = { visibleTrack: track, ...gpu, endpointReadback: report.render.endpointPNG ?? null };
        return report.gpu;
      }, { requiresWorkspace: true });

      let brushStartRevision = null;
      const brushInProgress = await phase("held brush drag displays HTTP 200 preview without committing", async () => {
        await clickTool("笔刷");
        const before = await activeState();
        brushStartRevision = before.envelope.rev;
        const requestIndex = report.preview.requests.length;
        const box = await docBox();
        await page.mouse.move(box.x + box.width * 0.25, box.y + box.height * 0.35);
        heldStroke = "brush";
        await page.mouse.down();
        try {
          await page.mouse.move(box.x + box.width * 0.70, box.y + box.height * 0.65, { steps: 5 });
          await waitHeldPreview("brush", before, requestIndex);
        } finally {
          heldStroke = null;
          await page.mouse.up();
        }
        return { revision: before.envelope.rev };
      }, { requiresWorkspace: true });

      const brushAfter = brushInProgress && brushStartRevision !== null
        ? await phase("brush pen-up commits pixels and revision", async () => {
          const advanced = await waitRevision(brushStartRevision);
          return { before: brushStartRevision, after: advanced.envelope.rev };
        }, { requiresWorkspace: true })
        : null;

      if (brushAfter) {
        const undo = await phase("undo through the Edit menu", async () => {
          await clickMenuItem("编辑", /撤销/);
          const after = await waitRevision(brushAfter.after);
          return { menuRevision: after.envelope.rev };
        }, { requiresWorkspace: true });
        if (undo) {
          await phase("redo through the public bridge", async () => {
            const direct = parseEnvelope(await serviceCall("Redo"), "Service.Redo");
            if (direct.rev <= undo.menuRevision) {
              throw new Error(`Service.Redo did not advance revision: ${undo.menuRevision} -> ${direct.rev}`);
            }
            const after = await waitRevision(undo.menuRevision);
            return { undoRevision: undo.menuRevision, redoRevision: after.envelope.rev };
          }, { requiresWorkspace: true });
        }
      }

      await phase("shape drag commits through the real canvas", async () => {
        await clickTool("形状");
        const selector = page.locator('select[aria-label="Shape kind"]');
        await selector.waitFor({ state: "visible", timeout: DEFAULT_TIMEOUT });
        await selector.selectOption("Rectangle");
        const before = (await activeState()).envelope.rev;
        const box = await docBox();
        await gesture(
          { x: box.x + box.width * 0.15, y: box.y + box.height * 0.15 },
          { x: box.x + box.width * 0.50, y: box.y + box.height * 0.50 },
        );
        const after = await waitRevision(before);
        return { before, after: after.envelope.rev, kind: await selectValue(selector) };
      }, { requiresWorkspace: true });

      await phase("held gradient drag previews HTTP 200 then commits on release", async () => {
        await clickTool("渐变");
        const selector = page.locator('select[aria-label="Gradient shape"]');
        await selector.waitFor({ state: "visible", timeout: DEFAULT_TIMEOUT });
        await selector.selectOption("Radial");
        await page.locator('select[aria-label="Gradient source"]').selectOption("foregroundBackground");
        const before = await activeState();
        const requestIndex = report.preview.requests.length;
        const box = await docBox();
        await page.mouse.move(box.x + box.width * 0.20, box.y + box.height * 0.20);
        heldStroke = "gradient";
        await page.mouse.down();
        try {
          await page.mouse.move(box.x + box.width * 0.45, box.y + box.height * 0.45, { steps: 6 });
          await waitHeldPreview("gradient", before, requestIndex);
        } finally {
          heldStroke = null;
          await page.mouse.up();
        }
        const after = await waitRevision(before.envelope.rev);
        const editedLayer = after.envelope.doc.layers.find((layer) => layer.id === after.envelope.doc.activeLayerID);
        if (editedLayer?.shape) throw new Error("gradient kept procedural shape metadata and hides its pixel edit");
        return { before: before.envelope.rev, after: after.envelope.rev, shape: await selectValue(selector) };
      }, { requiresWorkspace: true });

      await phase("Ctrl+T drag previews over HTTP and Escape restores the committed render", async () => {
        const before = await activeState();
        const baseline = await fetchBytes(`/render/${encodeURIComponent(before.tabID)}.png?v=${before.envelope.rev}`);
        requirePNG(baseline, "committed render before transform cancellation");
        try {
          const preview = await moveTransformDraft(before, baseline);
          await page.keyboard.press("Escape");
          await transformInspector().waitFor({ state: "hidden" });
          await page.locator("svg.transform-box").waitFor({ state: "hidden" });
          await waitUntil(() => page.locator('.doc img.render[src*="transform="]').count().then((count) => count === 0),
            "Escape retained the draft preview image", PREVIEW_ACK_TIMEOUT);
          const restored = await fetchBytes(`/render/${encodeURIComponent(before.tabID)}.png?v=${before.envelope.rev}`);
          requirePNG(restored, "render after Escape");
          if (!Buffer.from(restored.bytes).equals(Buffer.from(baseline.bytes))) throw new Error("Escape changed the committed render");
          await assertPinnedDocument(before, "Escape cancellation");
          report.transform.cancel = { ...preview, restored: true };
          return report.transform.cancel;
        } finally {
          if (await transformInspector().isVisible()) await page.keyboard.press("Escape");
        }
      }, { requiresWorkspace: true });

      await phase("Ctrl+T drag waits for Enter to commit its previewed transform", async () => {
        const before = await activeState();
        const baseline = await fetchBytes(`/render/${encodeURIComponent(before.tabID)}.png?v=${before.envelope.rev}`);
        requirePNG(baseline, "committed render before transform submission");
        try {
          const preview = await moveTransformDraft(before, baseline);
          await page.keyboard.press("Enter");
          const after = await waitRevision(before.envelope.rev);
          await transformInspector().waitFor({ state: "hidden" });
          assertCommittedTransform(after, preview.layerID, preview.draft);
          const committed = await fetchBytes(`/render/${encodeURIComponent(after.tabID)}.png?v=${after.envelope.rev}`);
          requirePNG(committed, "render after Enter");
          if (pngHash(committed.bytes) !== preview.pngHash) throw new Error("Enter render differs from the previewed transform");
          state = after;
          report.transform.commit = { ...preview, after: after.envelope.rev };
          return report.transform.commit;
        } finally {
          if (await transformInspector().isVisible()) await page.keyboard.press("Escape");
        }
      }, { requiresWorkspace: true });

      await phase("rotation handle previews over HTTP and Enter commits its angle", async () => {
        const before = await activeState();
        const baseline = await fetchBytes(`/render/${encodeURIComponent(before.tabID)}.png?v=${before.envelope.rev}`);
        requirePNG(baseline, "committed render before rotation");
        try {
          const initial = await beginTransform();
          const handle = page.locator("svg.transform-box .rotation-handle");
          await handle.waitFor({ state: "visible" });
          const grip = await handle.boundingBox();
          const frame = await page.locator("svg.transform-box polygon.transform-frame").boundingBox();
          if (!grip || !frame) throw new Error("rotation handle or transform frame is missing");
          const center = { x: frame.x + frame.width / 2, y: frame.y + frame.height / 2 };
          const from = { x: grip.x + grip.width / 2, y: grip.y + grip.height / 2 };
          const vector = { x: from.x - center.x, y: from.y - center.y };
          const radians = 35 * Math.PI / 180;
          const to = {
            x: center.x + vector.x * Math.cos(radians) - vector.y * Math.sin(radians),
            y: center.y + vector.x * Math.sin(radians) + vector.y * Math.cos(radians),
          };
          const preview = await heldTransformDrag(before, baseline, from, to);
          const angle = preview.draft.rotation - initial.rotation;
          if (angle < 25 || angle > 45 ||
              JSON.stringify(preview.draft.origin) !== JSON.stringify(initial.origin) ||
              JSON.stringify(preview.draft.size) !== JSON.stringify(initial.size)) {
            throw new Error(`rotation grip did not rotate about the layer center: ${JSON.stringify({ initial, draft: preview.draft })}`);
          }
          await page.keyboard.press("Enter");
          const after = await waitRevision(before.envelope.rev);
          await transformInspector().waitFor({ state: "hidden" });
          assertCommittedTransform(after, preview.layerID, preview.draft);
          const committed = await fetchBytes(`/render/${encodeURIComponent(after.tabID)}.png?v=${after.envelope.rev}`);
          requirePNG(committed, "render after rotation commit");
          if (Buffer.from(committed.bytes).equals(Buffer.from(baseline.bytes))) throw new Error("rotation commit did not change the render");
          if (pngHash(committed.bytes) !== preview.pngHash) throw new Error("rotation commit render differs from its preview");
          state = after;
          report.transform.rotation = { ...preview, angle, after: after.envelope.rev };
          return report.transform.rotation;
        } finally {
          if (await transformInspector().isVisible()) await page.keyboard.press("Escape");
        }
      }, { requiresWorkspace: true });

      await phase("wand selection works through UI and the public bridge", async () => {
        await clickTool("魔棒");
        const wandMode = page.locator('select[aria-label="Magic selection mode"]');
        await wandMode.waitFor({ state: "visible", timeout: DEFAULT_TIMEOUT });
        await wandMode.selectOption("wand");
        await clickDocument(0.5, 0.5);
        const selection = parseJSON(
          await serviceCall("SelectWandAt", [state.tabID, 32, 24, 0, 0, true, false]),
          "Service.SelectWandAt",
        );
        if (selection.tabId !== state.tabID || selection.selection?.w !== 64 || selection.selection?.h !== 48) {
          throw new Error(`wand selection shape is ${JSON.stringify(selection)}`);
        }
        const maskBytes = Buffer.from(selection.selection.mask, "base64").length;
        if (maskBytes !== 64 * 48) throw new Error(`wand mask has ${maskBytes} bytes`);
        const ants = await waitUntil(async () => page.locator("svg.ants").count(), "UI wand selection did not expose marching ants", 3_000);
        return { tabID: selection.tabId, maskBytes, marchingAnts: ants };
      }, { requiresWorkspace: true });

      await phase("selection edit menus open their real sheets and load pixels", async () => {
        const dialog = page.locator('dialog[aria-labelledby="selection-edit-title"]');
        const cases = [
          ["色彩范围", "色彩范围"],
          ["扩展选区", "扩展选区"],
          ["收缩选区", "收缩选区"],
          ["羽化选区", "羽化选区"],
          ["变换选区", "变换选区"],
        ];
        const opened = [];
        for (const [menuText, heading] of cases) {
          await clickMenuItem("选择", new RegExp(menuText));
          await dialog.waitFor({ state: "visible", timeout: DEFAULT_TIMEOUT });
          await dialog.getByRole("heading", { name: heading, exact: true }).waitFor({ state: "visible", timeout: DEFAULT_TIMEOUT });
          opened.push(heading);
          await dialog.getByRole("button", { name: "取消", exact: true }).click();
          await waitUntil(async () => !(await dialog.isVisible().catch(() => false)), `${heading} sheet did not close`, DEFAULT_TIMEOUT);
        }

        const nav = page.locator('nav[aria-label="菜单"]');
        await nav.getByRole("button", { name: "选择", exact: true }).click();
        const loadMask = page.getByRole("menuitem").filter({ hasText: "载入蒙版黑色区域" }).first();
        const loadPixels = page.getByRole("menuitem").filter({ hasText: "载入图层像素" }).first();
        await loadMask.waitFor({ state: "visible", timeout: DEFAULT_TIMEOUT });
        await loadPixels.waitFor({ state: "visible", timeout: DEFAULT_TIMEOUT });
        const maskDisabled = await loadMask.isDisabled();
        await loadPixels.click();
        await waitUntil(async () => {
          try { return (await activeState()).tabID === state.tabID; } catch { return null; }
        }, "loading layer pixels did not return to the active workspace", DEFAULT_TIMEOUT);
        report.interfaces.selectionMenus = { opened, loadPixels: true, loadMaskDisabled: maskDisabled };
        if (!maskDisabled) throw new Error("load-mask menu item was enabled without an active layer mask");
        return report.interfaces.selectionMenus;
      }, { requiresWorkspace: true });

      await phase("JPEG preview/export uses the real service and pixel plane", async () => {
        const begin = parseJSON(await serviceCall("BeginExportPreview"), "Service.BeginExportPreview");
        if (begin.width !== 64 || begin.height !== 48) throw new Error(`export preview dimensions are ${begin.width}x${begin.height}`);
        const preview = parseJSON(await serviceCall("ExportJPEGPreview", [85, "#ffffff"]), "Service.ExportJPEGPreview");
        if (typeof preview.url !== "string" || preview.size <= 0) throw new Error(`invalid JPEG preview reply: ${JSON.stringify(preview)}`);
        const staged = await fetchBytes(preview.url);
        if (staged.status !== 200 || staged.bytes.length <= 0) throw new Error(`staged JPEG returned HTTP ${staged.status}`);
        await serviceCall("EndExportPreview");
        return { width: begin.width, height: begin.height, url: preview.url, reportedBytes: preview.size, fetchedBytes: staged.bytes.length, contentType: staged.contentType };
      }, { requiresWorkspace: true });

      await phase("save, open, and reload the project through the public services", async () => {
        const savedRevision = await workspaceCall("SaveActive", [projectPath]);
        if (!Number.isInteger(savedRevision)) throw new Error(`SaveActive returned ${savedRevision}`);
        if (!fs.existsSync(path.join(projectPath, "manifest.json")) && !fs.existsSync(projectPath)) {
          throw new Error(`saved project was not found at ${projectPath}`);
        }
        const opened = parseJSON(await serviceCall("OpenPath", [projectPath]), "Service.OpenPath");
        if (opened.pending === true) throw new Error("saved .comp unexpectedly entered a pending conversion flow");
        const openedState = await activeState();
        if (openedState.envelope.doc.width !== 64 || openedState.envelope.doc.height !== 48) throw new Error("opened project dimensions changed");
        await page.reload({ waitUntil: "domcontentloaded", timeout: 30_000 });
        await waitUntil(() => page.evaluate(() => typeof window.go?.bridge?.Workspace?.Snapshot === "function"), "bridge did not return after browser reload", 30_000);
        await page.locator('[role="application"][aria-label="画布"]').waitFor({ state: "visible", timeout: DEFAULT_TIMEOUT });
        const reloaded = await activeState();
        if (reloaded.envelope.doc.width !== 64 || reloaded.envelope.doc.height !== 48) throw new Error("reloaded project is not usable");
        const png = await fetchBytes(`/render/${encodeURIComponent(reloaded.tabID)}.png?v=${reloaded.envelope.rev}`);
        const info = requirePNG(png, "reloaded HTTP render");
        report.interfaces.reloadTabID = reloaded.tabID;
        return { savedRevision, openActiveID: openedState.tabID, reloadActiveID: reloaded.tabID, render: info };
      }, { requiresWorkspace: true });

      await phase("object selection arms the UI mode and adapts the public API", async () => {
        const current = await activeState();
        await clickMenuItem("选择", /对象选择/);
        const mode = page.locator('select[aria-label="Magic selection mode"]');
        const modeValue = await waitUntil(async () => {
          const value = await mode.inputValue().catch(() => "");
          return value === "object" ? value : null;
        }, "object selection menu did not arm object mode", 3_000);
        await clickDocument(0.5, 0.5);
        const raw = await callObjectSelection(current.tabID, 32, 24);
        const selection = parseJSON(raw, "Service.SelectObjectAt");
        if (selection.tabId !== current.tabID || selection.selection?.w !== 64 || selection.selection?.h !== 48) {
          throw new Error(`object selection shape is ${JSON.stringify(selection)}`);
        }
        let negativeRejected = false;
        try {
          await callObjectSelection(current.tabID, -1, 0);
        } catch {
          negativeRejected = true;
        }
        if (!negativeRejected) throw new Error("negative SelectObjectAt coordinates were accepted");
        return { mode: modeValue, apiArgs: report.interfaces.selectObjectAtArgs, maskBytes: Buffer.from(selection.selection.mask, "base64").length, negativeRejected };
      }, { requiresWorkspace: true });
    }

    await sleep(300);
    // Missing or 404-only previews are failures of the held-drag phases,
    // never known gaps. Each gesture must independently acknowledge HTTP 200.
    report.preview.statuses = report.preview.requests.map((request) => request.status);
  } catch (error) {
    report.failures.push(`fatal: ${errorText(error)}`);
  } finally {
    if (page) {
      try {
        await withTimeout(page.screenshot({ path: finalScreenshotPath, fullPage: true }), 5_000, "final screenshot");
        report.finalScreenshot = finalScreenshotPath;
      } catch (error) {
        report.finalScreenshotError = errorText(error);
      }
      try {
        const canvas = page.locator('canvas.render');
        if (await canvas.count()) {
          await withTimeout(canvas.screenshot({ path: canvasScreenshotPath, timeout: 5_000 }), 6_000, "canvas screenshot");
          report.canvasScreenshot = canvasScreenshotPath;
        }
      } catch (error) {
        report.canvasScreenshotError = errorText(error);
      }
      if (report.failures.length > 0 || report.inconclusive.length > 0) {
        try {
          await withTimeout(page.screenshot({ path: screenshotPath, fullPage: true }), 5_000, "failure screenshot");
          report.screenshot = screenshotPath;
        } catch (error) {
          report.screenshotError = errorText(error);
        }
      }
    }
    if (runDeadlineTimer) clearTimeout(runDeadlineTimer);
    if (context) {
      try { await withTimeout(context.close(), 5_000, "browser close"); } catch (error) { report.contextCloseError = errorText(error); }
    }
    try {
      await withTimeout(Promise.all([...responseTasks]), 5_000, "HTTP response evidence");
    } catch (error) {
      report.failures.push(errorText(error));
    }
    const { knownGaps, ...classifiedDiagnostics } = classifyWailsOverlayDiagnostics(report.diagnostics, runtimeSources, options.baseUrl);
    Object.assign(report.diagnostics, classifiedDiagnostics);
    for (const gap of knownGaps) {
      if (!report.knownGaps.includes(gap)) report.knownGaps.push(gap);
    }
    if (report.diagnostics.pageErrors.length > 0) report.failures.push("browser page errors");
    if (report.diagnostics.console.some((entry) => entry.type === "error" && !entry.knownGap && !entry.url.endsWith("/favicon.ico"))) {
      report.failures.push("browser console errors");
    }
    if (report.diagnostics.failedRequests.some((entry) => {
      const url = new URL(entry.url);
      const supersededPreview = entry.failure === "net::ERR_ABORTED" &&
        (url.pathname.startsWith("/pixel/stroke/") || (url.pathname.startsWith("/render/") && url.searchParams.has("transform")));
      return !supersededPreview;
    })) report.failures.push("failed browser requests");
    if (report.diagnostics.failedResponses.length > 0) report.failures.push("HTTP error responses");
    report.finishedAt = new Date().toISOString();
    report.status = report.failures.length > 0
      ? "failed"
      : report.inconclusive.length > 0
        ? "inconclusive"
        : report.knownGaps.length > 0
          ? "passed_with_known_gaps"
          : "passed";
    fs.writeFileSync(reportPath, `${JSON.stringify(report, null, 2)}\n`, "utf8");
  }

  console.log(JSON.stringify({ status: report.status, report: reportPath, screenshot: report.screenshot ?? null, finalScreenshot: report.finalScreenshot ?? null, canvasScreenshot: report.canvasScreenshot ?? null, failures: report.failures, inconclusive: report.inconclusive, knownGaps: report.knownGaps, preview: report.preview, transform: report.transform, gpu: report.gpu }, null, 2));
  if (report.failures.length > 0 || report.inconclusive.length > 0) process.exitCode = 1;
}

main().catch((error) => {
  console.error(errorText(error));
  process.exitCode = 1;
});
