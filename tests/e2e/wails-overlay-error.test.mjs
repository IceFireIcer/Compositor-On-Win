import assert from "node:assert/strict";
import test from "node:test";
import { classifyWailsOverlayDiagnostics, isWailsOverlayError, WAILS_OVERLAY_GAP } from "./wails-overlay-error.mjs";

const baseUrl = "http://127.0.0.1:34115";
const runtimeURL = `${baseUrl}/wails/ipc.js`;

// Unmodified excerpts from Wails v2.16.0 internal/frontend/runtime/ipc_websocket.js.
// Keep this replay self-contained, without requiring or modifying the Go cache.
const runtimeSource = [
  'function Fe(e,t){var r=h;r.nodes===null&&(r.nodes={start:e,end:t,a:null,t:null})}',
  'function wn(e,t){}',
  'function bn(){if(y)return Fe(b,null),b;var e=document.createDocumentFragment(),t=document.createComment(""),r=me();return e.append(t,r),Fe(t,r),e}',
  'function kt(e,t){}',
  'function kn(e,t){ye(()=>{var r=e.getRootNode(),n=r.host?r:r.head??r.ownerDocument.head;if(!n.querySelector("#"+t.hash)){let o=Le("style");o.id=t.hash,o.textContent=t.code,n.appendChild(o),l&&ti(t.hash,o)}})}var lh=[];',
  'function In(e){kn(e,Ss);let t=()=>qr(Sr,"$overlayVisible",r),[r,n]=Kr();var o=bn(),i=pn(o);{var s=a=>{var f=As();Cn(3,f,()=>ci,()=>({duration:300})),kt(a,f)};Sn(i,a=>{t()&&a(s)})}kt(e,o),n()}var $s={},Mn=null,Gt=[];',
  'window.addEventListener("DOMContentLoaded",()=>{$s.overlay=new In({target:document.body,anchor:document.querySelector("#wails-spinner")})});',
].join("\n");

function location(token) {
  const before = runtimeSource.slice(0, runtimeSource.indexOf(token));
  const lines = before.split("\n");
  return `${runtimeURL}:${lines.length}:${lines.at(-1).length + 1}`;
}

// Captured in compositor-real-browser-smoke-ehQjPH/report.json on 2026-10-12:
// Fe(:32:26544) -> bn(:32:27002) -> new In(:41:87) -> DOMContentLoaded(:41:367).
// Only line/column positions change to match the source excerpts above.
const nodesError = {
  message: "Cannot read properties of null (reading 'nodes')",
  stack: [
    "TypeError: Cannot read properties of null (reading 'nodes')",
    `    at Fe (${location("r.nodes")})`,
    `    at bn (${location("Fe(t,r)")})`,
    `    at new In (${location("bn(),i=pn(o)")})`,
    `    at ${location("new In({target:document.body")}`,
  ].join("\n"),
};

const runtimeSources = new Map([[runtimeURL, runtimeSource]]);

test("records the verified Wails nodes initialization error and its console mirror as a known gap", () => {
  const diagnostics = {
    pageErrorDetails: [nodesError],
    console: [{ type: "error", text: `[window.error] ${nodesError.stack}`, url: `${baseUrl}/src/main.ts` }],
  };
  const result = classifyWailsOverlayDiagnostics(diagnostics, runtimeSources, baseUrl);

  assert.deepEqual(result.pageErrors, []);
  assert.equal(result.pageErrorDetails[0].knownGap, WAILS_OVERLAY_GAP);
  assert.equal(result.console[0].knownGap, WAILS_OVERLAY_GAP);
  assert.equal(result.knownGaps.length, 1);
  assert.equal(result.pageErrorDetails[0].stack, nodesError.stack);
  assert.equal(result.console[0].text, diagnostics.console[0].text);
  assert.equal(diagnostics.pageErrorDetails[0].knownGap, undefined);
});

test("retains the known getRootNode initialization variant", () => {
  const error = {
    message: "e.getRootNode is not a function",
    stack: [
      "TypeError: e.getRootNode is not a function",
      `    at ${location("e.getRootNode()")}`,
      `    at new In (${location("kn(e,Ss)")})`,
      `    at ${location("new In({target:document.body")}`,
    ].join("\n"),
  };
  assert.equal(isWailsOverlayError(error, runtimeSources, baseUrl), true);
});

test("requires runtime source evidence before classifying an early pageerror", () => {
  const sources = new Map();
  const diagnostics = { pageErrorDetails: [nodesError], console: [] };
  const pending = classifyWailsOverlayDiagnostics(diagnostics, sources, baseUrl);
  assert.deepEqual(pending.pageErrors, [nodesError.message]);
  assert.deepEqual(pending.knownGaps, []);

  sources.set(runtimeURL, runtimeSource);
  const captured = classifyWailsOverlayDiagnostics(diagnostics, sources, baseUrl);
  assert.deepEqual(captured.pageErrors, []);
  assert.equal(captured.knownGaps.length, 1);
});

test("preserves an application nodes error alongside a verified overlay error", () => {
  const applicationError = {
    ...nodesError,
    stack: nodesError.stack.replace(location("r.nodes"), `${baseUrl}/src/lib/components/CanvasSurface.svelte:100:20`),
  };
  const applicationConsole = { type: "error", text: `[window.error] ${applicationError.stack}`, url: `${baseUrl}/src/main.ts` };
  const result = classifyWailsOverlayDiagnostics({
    pageErrorDetails: [nodesError, applicationError],
    console: [applicationConsole],
  }, runtimeSources, baseUrl);

  assert.deepEqual(result.pageErrors, [applicationError.message]);
  assert.equal(result.pageErrorDetails[1].knownGap, undefined);
  assert.deepEqual(result.console, [applicationConsole]);
});

test("does not ignore independent console errors with the same message", () => {
  const console = [
    { type: "error", text: nodesError.message, url: `${baseUrl}/src/main.ts` },
    { type: "error", text: `[application.error] ${nodesError.stack}`, url: `${baseUrl}/src/main.ts` },
    { type: "error", text: `[window.error] ${nodesError.stack}\n    at updateCanvas (${baseUrl}/src/app.ts:30:2)`, url: `${baseUrl}/src/main.ts` },
  ];
  const result = classifyWailsOverlayDiagnostics({ pageErrorDetails: [nodesError], console }, runtimeSources, baseUrl);
  assert.deepEqual(result.console, console);

  const noPageError = classifyWailsOverlayDiagnostics({
    pageErrorDetails: [],
    console: [{ type: "error", text: `[window.error] ${nodesError.stack}`, url: `${baseUrl}/src/main.ts` }],
  }, runtimeSources, baseUrl);
  assert.equal(noPageError.console[0].knownGap, undefined);
});

const unverifiedVariants = [
  ["other runtime origin", { ...nodesError, stack: nodesError.stack.replaceAll(baseUrl, "http://127.0.0.1:34116") },
    new Map([[runtimeURL.replace(baseUrl, "http://127.0.0.1:34116"), runtimeSource]])],
  ["application script named ipc.js", { ...nodesError, stack: nodesError.stack.replaceAll("/wails/ipc.js", "/src/ipc.js") },
    new Map([[`${baseUrl}/src/ipc.js`, runtimeSource]])],
  ["missing source", nodesError, new Map()],
  ["altered overlay initialization", nodesError,
    new Map([[runtimeURL, runtimeSource.replace("new In({target:document.body", "mount(In,{target:document.body")]])],
  ["different nodes lookup", nodesError,
    new Map([[runtimeURL, runtimeSource.replace("var r=h;r.nodes", "var r=e;r.nodes")]])],
  ["first frame outside nodes lookup", { ...nodesError, stack: nodesError.stack.replace(location("r.nodes"), location("kn(e,Ss)")) }, runtimeSources],
  ["caller outside fragment allocation", { ...nodesError, stack: nodesError.stack.replace(location("Fe(t,r)"), location("r.nodes")) }, runtimeSources],
  ["overlay frame outside fragment call", { ...nodesError, stack: nodesError.stack.replace(location("bn(),i=pn(o)"), location("kn(e,Ss)")) }, runtimeSources],
  ["last frame outside DOMContentLoaded", { ...nodesError, stack: nodesError.stack.replace(location("new In({target:document.body"), location("kn(e,Ss)")) }, runtimeSources],
  ["additional application frame", { ...nodesError, stack: `${nodesError.stack}\n    at updateCanvas (${baseUrl}/src/app.ts:30:2)` }, runtimeSources],
  ["missing fragment frame", { ...nodesError, stack: nodesError.stack.split("\n").filter((line) => !line.includes("at bn (")).join("\n") }, runtimeSources],
  ["missing constructor frame", { ...nodesError, stack: nodesError.stack.replace("at new In (", "at updateCanvas (") }, runtimeSources],
  ["invalid source position", { ...nodesError, stack: nodesError.stack.replace(location("r.nodes"), `${runtimeURL}:1:999999`) }, runtimeSources],
  ["different property", { ...nodesError, message: "Cannot read properties of null (reading 'children')" }, runtimeSources],
];

for (const [label, error, sources] of unverifiedVariants) {
  test(`leaves ${label} as a smoke failure`, () => {
    const result = classifyWailsOverlayDiagnostics({ pageErrorDetails: [error], console: [] }, sources, baseUrl);
    assert.deepEqual(result.pageErrors, [error.message]);
    assert.deepEqual(result.knownGaps, []);
    assert.equal(result.pageErrorDetails[0].knownGap, undefined);
  });
}
