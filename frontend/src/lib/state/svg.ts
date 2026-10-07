/**
 * One-shot SVG rasterization in the browser — the frontend half of the
 * import pipeline (ticket 40). The original drew the SVG once with
 * NSImage at the fitted size; WebView2 does the same with its own SVG
 * renderer: fitted to the canvas when there is one, otherwise at the
 * size the file declares. The result travels back to Go as a PNG data
 * payload and becomes an ordinary image layer.
 */
export interface SVGRaster {
  name: string;
  png: string;
}

export async function rasterizeSVG(
  svgBase64: string,
  naturalW: number,
  naturalH: number,
  canvasW: number | null,
  canvasH: number | null,
): Promise<{ width: number; height: number; png: string }> {
  const scale = canvasW > 0 && canvasH > 0 ? Math.min(canvasW / naturalW, canvasH / naturalH) : 1;
  const width = Math.max(1, Math.round(naturalW * scale));
  const height = Math.max(1, Math.round(naturalH * scale));
  const img = new Image();
  img.src = `data:image/svg+xml;base64,${svgBase64}`;
  await img.decode();
  const canvas = document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("无法创建栅格画布");
  ctx.drawImage(img, 0, 0, width, height);
  const dataURL = canvas.toDataURL("image/png");
  return { width, height, png: dataURL.slice(dataURL.indexOf(",") + 1) };
}
