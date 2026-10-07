/**
 * One-shot SVG rasterization in the browser — the frontend half of the
 * import pipeline (ticket 40). The original drew the SVG once with
 * NSImage at the fitted size; WebView2 does the same with its own SVG
 * renderer: fitted to the canvas when there is one, otherwise at the
 * size the file declares. The result travels back to Go as a PNG data
 * payload and becomes an ordinary image layer.
 */
export async function rasterizeSVG(
  svgURL: string,
  naturalW: number,
  naturalH: number,
  canvasW: number | null,
  canvasH: number | null,
): Promise<{ width: number; height: number; blob: Blob }> {
  const fitted = canvasW != null && canvasH != null && canvasW > 0 && canvasH > 0;
  const scale = fitted ? Math.min((canvasW as number) / naturalW, (canvasH as number) / naturalH) : 1;
  const width = Math.max(1, Math.round(naturalW * scale));
  const height = Math.max(1, Math.round(naturalH * scale));
  const img = new Image();
  // The SVG source sits on the HTTP pixel plane (architecture §3.3).
  img.src = svgURL;
  await img.decode();
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("无法创建栅格画布");
  ctx.drawImage(img, 0, 0, width, height);
  const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/png"));
  if (!blob) throw new Error("无法编码栅格");
  return { width, height, blob };
}

/** PUT one rasterized SVG back onto the pixel plane. */
export async function uploadRaster(url: string, blob: Blob): Promise<void> {
  const response = await fetch(url, { method: "PUT", body: blob });
  if (!response.ok) {
    throw new Error(`SVG 上传失败：${response.status}`);
  }
}
