import process from "node:process";

const baseUrl = (process.env.COMPOSITOR_SMOKE_URL || "http://127.0.0.1:34115").replace(/\/$/, "");
const edgePath = process.env.COMPOSITOR_EDGE_PATH || "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe";
const playwrightRoot = process.env.COMPOSITOR_PLAYWRIGHT_ROOT;

function loadPlaywright() {
  const candidates = [playwrightRoot && `${playwrightRoot}/node_modules/playwright`, "playwright"].filter(Boolean);
  let lastError;
  for (const candidate of candidates) {
    try {
      return require(candidate);
    } catch (error) {
      lastError = error;
    }
  }
  throw new Error(`Playwright is unavailable: ${lastError}`);
}

const { createRequire } = await import("node:module");
const require = createRequire(import.meta.url);
const playwright = loadPlaywright();

if (!require("node:fs").existsSync(edgePath)) {
  throw new Error(`Edge executable not found: ${edgePath}`);
}

const browser = await playwright.chromium.launch({ executablePath: edgePath, headless: true });
try {
  const page = await browser.newPage();
  await page.goto(`${baseUrl}/`, { waitUntil: "domcontentloaded", timeout: 30_000 });
  const tabID = await page.evaluate(async () => {
    let snapshot = await window.go.bridge.Workspace.Snapshot();
    if (!snapshot?.activeId) {
      snapshot = await window.go.bridge.Workspace.NewDocument(64, 48, 72);
    }
    if (!snapshot?.activeId) throw new Error("workspace has no active tab");
    return snapshot.activeId;
  });

  const result = await page.evaluate(async (id) => {
    const response = {};
    const jsonReply = await fetch(`/render/${encodeURIComponent(id)}.json`);
    response.jsonStatus = jsonReply.status;
    response.jsonContentType = jsonReply.headers.get("content-type");
    const snapshot = await jsonReply.json();
    if (!Number.isInteger(snapshot.rev) || !snapshot.doc || !Array.isArray(snapshot.assets)) {
      throw new Error("render JSON did not contain the authoritative snapshot shape");
    }
    response.rev = snapshot.rev;
    response.documentID = snapshot.doc.documentID;

    for (const asset of snapshot.assets) {
      const reply = await fetch(asset.url);
      const bytes = new Uint8Array(await reply.arrayBuffer());
      const expectedBytes = asset.kind === "mask"
        ? asset.width * asset.height
        : asset.width * asset.height * 4;
      if (reply.status !== 200 || reply.headers.get("content-type")?.includes("text/html") || bytes.length !== expectedBytes) {
        throw new Error(`pixel asset route failed: ${asset.url}, HTTP ${reply.status}, ${reply.headers.get("content-type")}, ${bytes.length} bytes`);
      }
    }
    response.pixelAssets = snapshot.assets.length;

    const pngReply = await fetch(`/render/${encodeURIComponent(id)}.png?v=${snapshot.rev}`);
    response.pngStatus = pngReply.status;
    response.pngContentType = pngReply.headers.get("content-type");
    const bytes = new Uint8Array(await pngReply.arrayBuffer());
    const signature = [137, 80, 78, 71, 13, 10, 26, 10];
    if (!signature.every((value, index) => bytes[index] === value)) {
      throw new Error("render PNG did not have a valid PNG signature");
    }
    const u32 = (offset) => ((bytes[offset] << 24) >>> 0) + (bytes[offset + 1] << 16) + (bytes[offset + 2] << 8) + bytes[offset + 3];
    response.pngWidth = u32(16);
    response.pngHeight = u32(20);
    if (response.pngWidth !== snapshot.doc.width || response.pngHeight !== snapshot.doc.height) {
      throw new Error(`render PNG dimensions ${response.pngWidth}x${response.pngHeight} do not match document ${snapshot.doc.width}x${snapshot.doc.height}`);
    }
    if (response.jsonStatus !== 200 || response.jsonContentType !== "application/json" || response.pngStatus !== 200 || response.pngContentType !== "image/png") {
      throw new Error(`unexpected render route response: ${JSON.stringify(response)}`);
    }
    return response;
  }, tabID);

  console.log(JSON.stringify({ status: "passed", baseUrl, tabID, ...result }));
} finally {
  await browser.close();
}
