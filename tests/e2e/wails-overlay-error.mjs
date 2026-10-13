export const WAILS_OVERLAY_GAP = "wails-2.16-svelte5-browser-overlay";

const overlayStartup = 'window.addEventListener("DOMContentLoaded",()=>{$s.overlay=new In({target:document.body,anchor:document.querySelector("#wails-spinner")})});';
const gapDescription = 'Wails 2.16 browser IPC overlay invokes the Svelte 5 function component as new In({target:document.body,anchor:document.querySelector("#wails-spinner")}); its getRootNode or null nodes initialization failure was verified against /wails/ipc.js source and the full runtime-only stack. Runtime was not patched.';

function sourceOffset(lines, lineNumber, column) {
  if (lineNumber < 1 || lineNumber > lines.length || column < 1 || column > lines[lineNumber - 1].length) return -1;
  return lines.slice(0, lineNumber - 1).reduce((total, value) => total + value.length + 1, 0) + column - 1;
}

function pointsTo(source, offset, token, start, end) {
  const tokenStart = source.indexOf(token, start);
  return start >= 0 && end > start && tokenStart >= start && tokenStart + token.length <= end &&
    offset >= tokenStart && offset < tokenStart + token.length;
}

// Classify only the upstream overlay constructor, never arbitrary Svelte or
// application errors. Source and every stack frame must belong to Wails IPC.
export function isWailsOverlayError(entry, runtimeSources, baseUrl) {
  const rootFailure = entry.message === "e.getRootNode is not a function";
  const nodesFailure = entry.message === "Cannot read properties of null (reading 'nodes')";
  if ((!rootFailure && !nodesFailure) || typeof entry.stack !== "string") return false;
  const [header, ...frames] = entry.stack.trimEnd().split("\n");
  if (header !== `TypeError: ${entry.message}` || frames.length < 3) return false;
  const locations = frames.map((frame) => frame.match(/^\s*at (?:([^()]+) \()?(https?:\/\/[^\s)]+):(\d+):(\d+)\)?$/));
  if (locations.some((location) => !location) || new Set(locations.map((location) => location[2])).size !== 1) return false;
  const runtimeURL = locations[0][2];
  try {
    const url = new URL(runtimeURL);
    if (url.origin !== new URL(baseUrl).origin || url.pathname !== "/wails/ipc.js") return false;
  } catch {
    return false;
  }
  const source = runtimeSources.get(runtimeURL);
  if (typeof source !== "string" ||
      !source.includes('function In(e){kn(e,Ss);') ||
      !source.includes('var r=e.getRootNode()') ||
      !source.includes(overlayStartup)) return false;
  const lines = source.split("\n");
  const offsets = locations.map((location) => sourceOffset(lines, Number(location[3]), Number(location[4])));
  if (offsets.some((offset) => offset < 0)) return false;
  if (rootFailure) {
    const rootStart = source.indexOf("function kn(e,t){");
    const rootEnd = source.indexOf("var lh=", rootStart);
    return locations.some((location) => location[1] === "new In" || location[1] === "In") &&
      pointsTo(source, offsets[0], "e.getRootNode()", rootStart, rootEnd);
  }

  // The captured nodes failure is specifically Fe -> bn -> new In -> the
  // DOMContentLoaded callback. Validate every call site against the source,
  // including the null effect lookup; a message or runtime URL alone is not evidence.
  if (locations.length !== 4 || locations[0][1] !== "Fe" || locations[1][1] !== "bn" ||
      locations[2][1] !== "new In" || locations[3][1] !== undefined) return false;
  const nodesStart = source.indexOf("function Fe(e,t){var r=h;r.nodes===null&&");
  const nodesEnd = source.indexOf("function wn(", nodesStart);
  const fragmentStart = source.indexOf("function bn(){");
  const fragmentEnd = source.indexOf("function kt(", fragmentStart);
  const overlayStart = source.indexOf("function In(e){kn(e,Ss);");
  const overlayEnd = source.indexOf("var $s=", overlayStart);
  const startupStart = source.indexOf(overlayStartup);
  return pointsTo(source, offsets[0], "r.nodes", nodesStart, nodesEnd) &&
    pointsTo(source, offsets[1], "Fe(t,r)", fragmentStart, fragmentEnd) &&
    pointsTo(source, offsets[2], "bn()", overlayStart, overlayEnd) &&
    pointsTo(source, offsets[3], "new In({target:document.body", startupStart, startupStart + overlayStartup.length);
}

// Preserve raw evidence while deriving the smoke report's failure buckets.
export function classifyWailsOverlayDiagnostics(diagnostics, runtimeSources, baseUrl) {
  const verified = [];
  const pageErrorDetails = diagnostics.pageErrorDetails.map((entry) => {
    if (!isWailsOverlayError(entry, runtimeSources, baseUrl)) return { ...entry };
    const classified = { ...entry, knownGap: WAILS_OVERLAY_GAP };
    verified.push(classified);
    return classified;
  });
  return {
    pageErrorDetails,
    pageErrors: pageErrorDetails.filter((entry) => !entry.knownGap).map((entry) => entry.message),
    // main.ts forwards window.error to console.error with its original stack.
    // Exempt only an exact mirror of a source-verified pageerror, keeping
    // independent application console errors in the smoke failure bucket.
    console: diagnostics.console.map((entry) => entry.type === "error" &&
      verified.some((error) => entry.text === `[window.error] ${error.stack}`)
      ? { ...entry, knownGap: WAILS_OVERLAY_GAP }
      : { ...entry }),
    knownGaps: verified.length > 0 ? [gapDescription] : [],
  };
}
