import { get } from "svelte/store";
import type { DocumentState } from "./document";
import { transformCorners, type SnapInput } from "./transformSession";
import { guideTargets, layoutGrid, snapOptions } from "./view";

/** Snapshot targets at pointer-move time, excluding the moving layer itself. */
export function canvasSnapInput(doc: DocumentState, movingLayerID: string, pointsPerPixel: number, bypass = false): SnapInput {
  const options = snapOptions(bypass);
  return {
    enabled: options.enabled && !options.bypass,
    toggles: options.toggles,
    tolerance: 10 / Math.max(pointsPerPixel, 0.0001),
    targets: {
      guides: guideTargets(), grid: get(layoutGrid), docBounds: { width: doc.width, height: doc.height },
      layerBounds: doc.layers.filter((layer) => layer.id !== movingLayerID && layer.isVisible && !layer.isGroup).map((layer) => {
        const corners = transformCorners({ ...layer.transform, rotation: layer.transform.rotation ?? 0,
          flipX: layer.transform.flipX ?? false, flipY: layer.transform.flipY ?? false,
          sampling: layer.transform.sampling ?? "High quality" });
        const xs = corners.map((p) => p.x), ys = corners.map((p) => p.y);
        const x = Math.min(...xs), y = Math.min(...ys);
        return { x, y, width: Math.max(...xs) - x, height: Math.max(...ys) - y };
      }),
    },
  };
}
