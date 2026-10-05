/**
 * Render-graph builder: turns the document layer tree into the ordered GPU
 * draw plan, mirroring internal/render/composite.go's scope semantics —
 * layers bottom-to-top, folders pass-through with subtree opacity and mask,
 * layer masks (linked/unlinked via maskPlacement) and clip chains multiply
 * coverage, invisible layers hide themselves and their subtree, adjustment
 * layers recolor the composite below (the M5 kernels land later; kinds
 * without a GPU kernel pass through, exactly as the CPU truth does).
 *
 * The graph is device-independent: the WebGPU compositor executes it as
 * blend passes (shaders/blend.ts), the CPU fallback feeds the same structure
 * to the Go renderer — which is why the consistency protocol (GPU vs CPU
 * truth) can drive both from one graph.
 */

/** Blend mode raw values, the macOS spellings (domain.BlendMode order 0–23). */
export const BLEND_MODE_INDEX: Record<string, number> = {
  Normal: 0,
  Darken: 1,
  Multiply: 2,
  "Color Burn": 3,
  "Linear Burn": 4,
  Lighten: 5,
  Screen: 6,
  "Color Dodge": 7,
  "Linear Dodge (Add)": 8,
  Overlay: 9,
  "Soft Light": 10,
  "Hard Light": 11,
  "Vivid Light": 12,
  "Linear Light": 13,
  "Pin Light": 14,
  "Hard Mix": 15,
  Difference: 16,
  Exclusion: 17,
  Subtract: 18,
  Divide: 19,
  Hue: 20,
  Saturation: 21,
  Color: 22,
  Luminosity: 23,
};

/** One layer as the graph sees it — the bridge's document view. */
export interface GraphLayer {
  id: string;
  parentId?: string;
  isGroup: boolean;
  isVisible: boolean;
  opacity: number;
  blendMode: string;
  imageFile?: string;
  revision: number;
  /** Clipping: this layer is masked by the named layer's coverage. */
  maskSourceId?: string;
  maskFile?: string;
  maskEnabled: boolean;
  maskLinked: boolean;
  /** Adjustment kinds without a GPU kernel pass through (M5 tickets). */
  adjustment?: { kind: string };
}

export interface RenderDoc {
  width: number;
  height: number;
  layers: GraphLayer[];
}

export interface PlaceOp {
  kind: "place";
  layer: GraphLayer;
  /** Effective blend mode index after folder pass-through rules. */
  mode: number;
  opacity: number;
  /** Mask coverage texture info when the layer carries an enabled mask. */
  mask?: { file: string; linked: boolean };
  /** Clip source resolved during the walk (the layer's own maskSourceId). */
  clipTo?: string;
}

export interface GroupOp {
  kind: "group";
  ops: GraphOp[];
  opacity: number;
  mask?: { file: string; linked: boolean };
}

export type GraphOp = PlaceOp | GroupOp;

function isAdjustmentSupported(kind: string): boolean {
  // Ticket 10: LUT/cube kinds have both tracks; the spatial M5 kinds pass
  // through until their tickets land.
  return ["Levels", "Curves", "Exposure", "Hue/Saturation", "Gradient Map"].includes(kind);
}

/**
 * Builds the draw plan. Throws on structural errors the Go validator already
 * rejects (missing/cyclic parents), because a graph from invalid data would
 * render wrong rather than fail loudly.
 */
export function buildRenderGraph(doc: RenderDoc): GraphOp[] {
  const byId = new Map<string, GraphLayer>();
  for (const l of doc.layers) byId.set(l.id, l);
  const visiting = new Set<string>();
  const visited = new Set<string>();

  const childrenOf = (parentId: string): GraphLayer[] => {
    const out: GraphLayer[] = [];
    for (const l of doc.layers) {
      if (l.parentId === parentId) out.push(l);
    }
    return out; // doc.layers is bottom-to-top already
  };

  // Clip chain: contiguous clip siblings above a non-clipping base multiply
  // into the base's coverage (composite.go walks these through scope.cov).
  const walk = (layers: GraphLayer[], folderOpacity: number): GraphOp[] => {
    const ops: GraphOp[] = [];
    for (let i = 0; i < layers.length; i++) {
      const l = layers[i];
      if (!l.isVisible) continue;
      if (l.adjustment) {
        // The Swift layer path skips mask-sourced adjustment layers entirely.
        if (l.maskSourceId) continue;
        if (isAdjustmentSupported(l.adjustment.kind)) {
          // M5 wires the cube/LUT pass into the graph; until then the CPU
          // truth is the only track that renders adjustment content.
          ops.push({ kind: "place", layer: l, mode: BLEND_MODE_INDEX.Normal, opacity: l.opacity * folderOpacity });
        }
        continue;
      }
      if (l.isGroup) {
        if (visiting.has(l.id)) throw new Error(`circular group nesting at ${l.id}`);
        visiting.add(l.id);
        const ops2 = walk(childrenOf(l.id), 1);
        visiting.delete(l.id);
        visited.add(l.id);
        ops.push({
          kind: "group",
          ops: ops2,
          opacity: l.opacity * folderOpacity,
          mask: l.maskFile && l.maskEnabled ? { file: l.maskFile, linked: l.maskLinked } : undefined,
        });
        continue;
      }
      let clipTo: string | undefined;
      if (l.maskSourceId) {
        const source = byId.get(l.maskSourceId);
        if (!source) throw new Error(`missing clip source ${l.maskSourceId}`);
        clipTo = l.maskSourceId;
      }
      ops.push({
        kind: "place",
        layer: l,
        mode: BLEND_MODE_INDEX[l.blendMode] ?? 0,
        opacity: l.opacity * folderOpacity,
        mask: l.maskFile && l.maskEnabled ? { file: l.maskFile, linked: l.maskLinked } : undefined,
        clipTo,
      });
    }
    return ops;
  };

  const roots = doc.layers.filter((l) => !l.parentId);
  return walk(roots, 1);
}

/** Counts the place operations that actually draw (test/benchmark helper). */
export function countPlaces(ops: GraphOp[]): number {
  let n = 0;
  for (const op of ops) {
    if (op.kind === "place") n += 1;
    else n += countPlaces(op.ops);
  }
  return n;
}
