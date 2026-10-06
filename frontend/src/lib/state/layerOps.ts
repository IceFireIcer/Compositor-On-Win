/**
 * Layer command family + layer-panel state (ticket 20), porting the Swift
 * EditorSession layer commands onto the document shape of
 * internal/domain/document.go: a flat bottom→top layer array with hierarchy
 * by parentID — folders are isGroup records, and the renderers (Go
 * composite.go, the GPU graph) resolve parents by lookup, so commands keep
 * each moved subtree contiguous where the original does.
 *
 * Every command is a pure function (ctx, …) → { layers, undoName,
 * activeLayerID, selectedLayerIDs }, or null when its precondition fails —
 * null means "no change", exactly the Swift unavailable-action gates. Undo
 * wiring lives in the integration ticket: returning undoName is enough here
 * (DocumentHistory keeps the OUTERMOST beginEdit name, which is what these
 * strings reproduce). Setting the active layer collapses the selection to it
 * (Swift's activeLayerID didSet), which is why group/ungroup/merge/delete/
 * duplicate also report selection while rename/visibility/reorder echo it.
 *
 * Pixel semantics are noted, not implemented: ⌘E composites "as the canvas
 * shows them" through Go render.Render at integration (kept subset in, PNG
 * out); this layer owns the ordering — result identity/name/slot, removals,
 * clip re-pointing — mirroring LayerMerge.swift. Likewise the delete-time
 * "bake or unlink" alert is UI work; deleting here unlinks (Swift's
 * "Remove Links and Delete" path).
 *
 * Swift sources (read-only truth, reference/Swift/Compositor/):
 * EditorSession.swift (blank layer, rename, visibility, reorder, move,
 * delete neighbor), LayerGroups.swift (group/ungroup/place/move out),
 * LayerMerge.swift (⌘E plan), LiveLayerMask.swift (clipping + release),
 * SelectionClipboard.swift (duplicate), LayerMask.swift (mask enable/link/
 * delete), LayerEffects.swift (effect removal), LayerAppearance.swift
 * (opacity/blend gating), NativeLayerList.swift (context menu titles).
 */

/** Layer limits carried over from Swift/DocumentLimits (domain.MaxLayers). */
export const MAX_LAYERS = 10_000;

/**
 * One layer as internal/domain.Document.Layers sees it (JSON field names
 * verbatim). Fields this module never touches (transform, imageFile,
 * adjustment payload, …) survive every command untouched.
 */
export interface DocLayer {
  id: string;
  name: string;
  isVisible: boolean;
  /** Version 2: hierarchy. Absent = root level. */
  parentID?: string;
  isGroup?: boolean;
  /** Version 3: appearance. Opacity 0–1, absent = 1. */
  opacity?: number;
  blendMode?: string;
  /** Version 4/6: raster layer mask. */
  maskFile?: string;
  maskEnabled?: boolean;
  maskLinked?: boolean;
  maskPlacement?: Record<string, unknown>;
  /** Version 5: clipping mask — this layer shows through the named layer. */
  maskSourceID?: string;
  /** Version 7: adjustment layers (payload opaque here). */
  adjustment?: Record<string, unknown>;
  /** Ungated: per-layer effect bundle (domain.Effects JSON). */
  effects?: LayerEffects;
}

export interface LayerEffects {
  stroke?: Record<string, unknown>;
  shadow?: Record<string, unknown>;
  colorOverlay?: Record<string, unknown>;
  innerShadow?: Record<string, unknown>;
  outerGlow?: Record<string, unknown>;
  innerGlow?: Record<string, unknown>;
}

/** Effect kinds, keyed as domain.Effects stores them (Swift rawValues). */
export type EffectKind = keyof LayerEffects;

export const EFFECT_KIND_TITLES: Record<EffectKind, string> = {
  stroke: "Stroke",
  shadow: "Drop Shadow",
  colorOverlay: "Color Overlay",
  innerShadow: "Inner Shadow",
  outerGlow: "Outer Glow",
  innerGlow: "Inner Glow",
};

export interface LayerContext {
  /** Bottom → top, domain.Document.Layers. */
  layers: DocLayer[];
  activeLayerID: string | null;
  /** Multi-selection; includes the active layer when there is one. */
  selectedLayerIDs: string[];
  /** Id factory for created layers (tests inject deterministic ids). */
  newId?: () => string;
}

export interface LayerOpResult {
  layers: DocLayer[];
  undoName: string;
  activeLayerID: string | null;
  selectedLayerIDs: string[];
}

/** What the delete key / context menu targets (Swift deleteLayerOrMask). */
export type DeleteTarget =
  | { kind: "layer" }
  | { kind: "mask" }
  | { kind: "effect"; layerId: string; effect: EffectKind };

/** Which thumbnail the right-click came from (menu title selection). */
export type MenuTarget =
  | { kind: "layer" }
  | { kind: "mask" }
  | { kind: "effect"; effect: EffectKind };

function makeId(ctx: LayerContext): string {
  return ctx.newId ? ctx.newId() : crypto.randomUUID();
}

function isGroup(l: DocLayer): boolean {
  return l.isGroup === true;
}

function clone(layers: DocLayer[]): DocLayer[] {
  return layers.map((l) => ({ ...l }));
}

function done(
  ctx: LayerContext,
  layers: DocLayer[],
  undoName: string,
  selection?: { activeLayerID?: string | null; selectedLayerIDs?: string[] },
): LayerOpResult {
  const active = selection?.activeLayerID !== undefined ? selection.activeLayerID : ctx.activeLayerID;
  return {
    layers,
    undoName,
    activeLayerID: active,
    // Swift's activeLayerID didSet: assigning the active layer collapses the
    // selection to it; keeping it echoes the incoming selection.
    selectedLayerIDs:
      selection?.selectedLayerIDs !== undefined
        ? selection.selectedLayerIDs
        : selection?.activeLayerID !== undefined
          ? active === null
            ? []
            : [active]
          : ctx.selectedLayerIDs,
  };
}

/** Assigning the active layer, with the didSet selection collapse. */
function withActive(ctx: LayerContext, layers: DocLayer[], undoName: string, id: string | null): LayerOpResult {
  return done(ctx, layers, undoName, {
    activeLayerID: id,
    selectedLayerIDs: id === null ? [] : [id],
  });
}

function activeLayer(ctx: LayerContext): DocLayer | null {
  if (ctx.activeLayerID === null) return null;
  return ctx.layers.find((l) => l.id === ctx.activeLayerID) ?? null;
}

/** Every layer below `id` in the tree (not `id` itself) — Swift descendantIDs. */
export function descendantIDs(layers: DocLayer[], id: string): Set<string> {
  const children = new Map<string | null, string[]>();
  for (const l of layers) {
    const key = l.parentID ?? null;
    const list = children.get(key) ?? [];
    list.push(l.id);
    children.set(key, list);
  }
  const out = new Set<string>();
  const pending = [id];
  for (let head = 0; head < pending.length; head++) {
    for (const child of children.get(pending[head]!) ?? []) {
      if (!out.has(child)) {
        out.add(child);
        pending.push(child);
      }
    }
  }
  return out;
}

function childrenOf(layers: DocLayer[]): Map<string | null, DocLayer[]> {
  const children = new Map<string | null, DocLayer[]>();
  for (const l of layers) {
    const key = l.parentID ?? null;
    const list = children.get(key) ?? [];
    list.push(l);
    children.set(key, list);
  }
  return children;
}

/** Depth-first pre-order bottom→top (Swift LayerOrder.resolve order). */
function hierarchyOrder(layers: DocLayer[]): string[] {
  const children = childrenOf(layers);
  const order: string[] = [];
  const visit = (parent: string | null): void => {
    for (const l of children.get(parent) ?? []) {
      order.push(l.id);
      if (isGroup(l)) visit(l.id);
    }
  };
  visit(null);
  return order;
}

/** The panel's rows, top first (Swift LayerHierarchy.entries topFirst). */
function panelOrder(layers: DocLayer[]): string[] {
  const children = childrenOf(layers);
  const out: string[] = [];
  const visit = (parent: string | null): void => {
    const list = children.get(parent) ?? [];
    for (let i = list.length - 1; i >= 0; i--) {
      const l = list[i]!;
      out.push(l.id);
      if (isGroup(l)) visit(l.id);
    }
  };
  visit(null);
  return out;
}

function findLastIndex(list: DocLayer[], pred: (l: DocLayer) => boolean): number {
  for (let i = list.length - 1; i >= 0; i--) {
    if (pred(list[i]!)) return i;
  }
  return -1;
}

function nextName(layers: DocLayer[], prefix: string): string {
  const names = new Set(layers.map((l) => l.name));
  let n = 1;
  while (names.has(`${prefix} ${n}`)) n++;
  return `${prefix} ${n}`;
}

/**
 * A moved layer stops clipping when it no longer belongs to the contiguous
 * stack above its base (Swift LiveLayerMask.releaseDetachedClipping).
 */
function releaseDetachedClipping(layers: DocLayer[]): void {
  const siblings = childrenOf(layers);
  const release = new Set<string>();
  for (const stack of siblings.values()) {
    let base: string | null = null;
    for (const l of stack) {
      const source = l.maskSourceID;
      if (source !== undefined) {
        if (source !== base) {
          release.add(l.id);
          base = l.id;
        }
      } else {
        base = isGroup(l) ? null : l.id;
      }
    }
  }
  for (const l of layers) {
    if (release.has(l.id)) delete l.maskSourceID;
  }
}

/**
 * A layer dropped into the middle of a clipping group joins it (Swift
 * LiveLayerMask.adoptClipping) — run while rearranging, before the release.
 */
function adoptClipping(layers: DocLayer[], id: string): void {
  const layer = layers.find((l) => l.id === id);
  if (!layer || isGroup(layer)) return;
  const siblings = layers.filter((l) => (l.parentID ?? null) === (layer.parentID ?? null));
  const index = siblings.findIndex((l) => l.id === id);
  if (index <= 0 || index + 1 >= siblings.length) return;
  const source = siblings[index + 1]!.maskSourceID;
  if (source === undefined || source === id) return;
  const below = siblings[index - 1]!;
  if (below.id !== source && below.maskSourceID !== source) return;
  layer.maskSourceID = source;
}

// ---------------------------------------------------------------------------
// Appearance
// ---------------------------------------------------------------------------

/** Hide/Show with the edit named for the direction it goes. */
export function toggleLayerVisibility(ctx: LayerContext, id: string): LayerOpResult | null {
  const layers = clone(ctx.layers);
  const layer = layers.find((l) => l.id === id);
  if (!layer) return null;
  layer.isVisible = !layer.isVisible;
  return done(ctx, layers, layer.isVisible ? "Show Layer" : "Hide Layer");
}

/** Inline rename: trimmed; blank names change nothing (LayerTests). */
export function renameLayer(ctx: LayerContext, id: string, name: string): LayerOpResult | null {
  const trimmed = name.trim();
  if (trimmed === "") return null;
  const layers = clone(ctx.layers);
  const layer = layers.find((l) => l.id === id);
  if (!layer) return null;
  layer.name = trimmed;
  return done(ctx, layers, "Rename Layer");
}

function clampOpacity(value: number): number | null {
  if (!Number.isFinite(value)) return null;
  return Math.min(1, Math.max(0, value));
}

/** The active layer's opacity (folders take one too — it dims their contents). */
export function setLayerOpacity(ctx: LayerContext, opacity: number): LayerOpResult | null {
  const value = clampOpacity(opacity);
  if (value === null || ctx.selectedLayerIDs.length !== 1 || activeLayer(ctx) === null) return null;
  const layers = clone(ctx.layers);
  const layer = layers.find((l) => l.id === ctx.activeLayerID);
  if (!layer) return null;
  layer.opacity = value;
  return done(ctx, layers, "Layer Opacity");
}

/** Every selected layer at once, as one undo step (Swift setSelectedLayersOpacity). */
export function setSelectedLayersOpacity(ctx: LayerContext, opacity: number): LayerOpResult | null {
  const value = clampOpacity(opacity);
  if (value === null) return null;
  const selected = new Set(ctx.selectedLayerIDs);
  const layers = clone(ctx.layers);
  let touched = false;
  for (const l of layers) {
    if (selected.has(l.id) && l.opacity !== value) {
      l.opacity = value;
      touched = true;
    }
  }
  if (!touched) return null;
  return done(ctx, layers, "Layer Opacity");
}

/**
 * The active layer's blend mode. Folders are pass-through: they take an
 * opacity but never a blend mode (Swift canEditAppearance).
 */
export function setLayerBlendMode(ctx: LayerContext, mode: string): LayerOpResult | null {
  const active = activeLayer(ctx);
  if (ctx.selectedLayerIDs.length !== 1 || active === null || isGroup(active)) return null;
  const layers = clone(ctx.layers);
  const layer = layers.find((l) => l.id === active.id);
  if (!layer) return null;
  layer.blendMode = mode;
  return done(ctx, layers, "Layer Blend Mode");
}

// ---------------------------------------------------------------------------
// Mask state (LayerMask.swift)
// ---------------------------------------------------------------------------

function canEditMask(ctx: LayerContext): boolean {
  return ctx.selectedLayerIDs.length === 1 && activeLayer(ctx) !== null;
}

/** Disable/Enable Mask on the active layer's raster mask. */
export function toggleLayerMaskEnabled(ctx: LayerContext): LayerOpResult | null {
  if (!canEditMask(ctx)) return null;
  const layers = clone(ctx.layers);
  const layer = layers.find((l) => l.id === ctx.activeLayerID);
  if (!layer || layer.maskFile === undefined) return null;
  const wasEnabled = layer.maskEnabled !== false;
  layer.maskEnabled = !wasEnabled;
  return done(ctx, layers, wasEnabled ? "Disable Layer Mask" : "Enable Layer Mask");
}

/** Unlink/Link Mask: whether the mask follows the layer's transform. */
export function toggleMaskLink(ctx: LayerContext): LayerOpResult | null {
  if (!canEditMask(ctx)) return null;
  const layers = clone(ctx.layers);
  const layer = layers.find((l) => l.id === ctx.activeLayerID);
  if (!layer || layer.maskFile === undefined || isGroup(layer) || layer.adjustment !== undefined) {
    return null;
  }
  const wasLinked = layer.maskLinked !== false;
  layer.maskLinked = !wasLinked;
  return done(ctx, layers, wasLinked ? "Unlink Layer Mask" : "Link Layer Mask");
}

/** Delete Mask: the mask, its placement and its link state all go. */
export function deleteLayerMask(ctx: LayerContext): LayerOpResult | null {
  if (!canEditMask(ctx)) return null;
  const layers = clone(ctx.layers);
  const layer = layers.find((l) => l.id === ctx.activeLayerID);
  if (!layer || layer.maskFile === undefined) return null;
  delete layer.maskFile;
  delete layer.maskEnabled;
  delete layer.maskLinked;
  delete layer.maskPlacement;
  return done(ctx, layers, "Delete Layer Mask");
}

// ---------------------------------------------------------------------------
// Effects (LayerEffects.swift)
// ---------------------------------------------------------------------------

/** Removes one effect kind; the last one takes the whole record with it. */
export function deleteEffect(ctx: LayerContext, layerId: string, kind: EffectKind): LayerOpResult | null {
  const layers = clone(ctx.layers);
  const layer = layers.find((l) => l.id === layerId);
  const effects = layer?.effects;
  if (!layer || !effects || effects[kind] === undefined) return null;
  const next = { ...effects };
  delete next[kind];
  if (Object.keys(next).length === 0) delete layer.effects;
  else layer.effects = next;
  return done(ctx, layers, `Remove ${EFFECT_KIND_TITLES[kind]}`);
}

// ---------------------------------------------------------------------------
// New blank layer (⇧⌘N) and duplicate (⌘J) — EditorSession / SelectionClipboard
// ---------------------------------------------------------------------------

/**
 * ⇧⌘N: a transparent layer above the active one (inside its folder), named
 * with the next free "Layer N". With a folder selected it lands on top of
 * that folder's contents.
 */
export function addBlankLayer(ctx: LayerContext): LayerOpResult | null {
  const layers = clone(ctx.layers);
  const active = activeLayer(ctx);
  const parentID = active ? (isGroup(active) ? active.id : active.parentID ?? null) : null;
  const layer: DocLayer = {
    id: makeId(ctx),
    name: nextName(layers, "Layer"),
    isVisible: true,
    parentID: parentID ?? undefined,
  };
  let insertion = active ? layers.findIndex((l) => l.id === active.id) + 1 : layers.length;
  if (active && isGroup(active)) {
    const inside = descendantIDs(layers, active.id);
    const topmost = findLastIndex(layers, (l) => inside.has(l.id));
    if (topmost >= 0) insertion = Math.max(insertion, topmost + 1);
  }
  layers.splice(insertion, 0, layer);
  return withActive(ctx, layers, "New Blank Layer", layer.id);
}

/**
 * One copy of the layer and anything it holds, inserted just above it; the
 * root copy gains " copy", descendants keep their names, and clip sources
 * inside the copied subtree are remapped into it (Swift insertCopy).
 */
function insertCopy(layers: DocLayer[], id: string, ctx: LayerContext): string | null {
  const index = layers.findIndex((l) => l.id === id);
  if (index < 0) return null;
  const included = descendantIDs(layers, id);
  included.add(id);
  const originals = layers.filter((l) => included.has(l.id));
  if (layers.length + originals.length > MAX_LAYERS) return null;
  const mapping = new Map(originals.map((o) => [o.id, makeId(ctx)]));
  const copies = originals.map((o) => ({
    ...o,
    id: mapping.get(o.id)!,
    name: o.id === id ? `${o.name} copy` : o.name,
    parentID:
      o.parentID !== undefined ? (mapping.get(o.parentID) ?? o.parentID) : undefined,
    maskSourceID:
      o.maskSourceID !== undefined ? (mapping.get(o.maskSourceID) ?? o.maskSourceID) : undefined,
  }));
  layers.splice(index + 1, 0, ...copies);
  return mapping.get(id)!;
}

/** Swift placeLayer on a single record, for the duplicate restack. */
function placeWithin(
  layers: DocLayer[],
  id: string,
  parentID: string | null,
  aboveID: string | null,
): boolean {
  const index = layers.findIndex((l) => l.id === id);
  if (index < 0 || aboveID === id) return false;
  let target = -1;
  if (aboveID !== null) {
    target = layers.findIndex((l) => l.id === aboveID && (l.parentID ?? null) === parentID);
    if (target < 0) return false;
  }
  const removed = layers.splice(index, 1)[0];
  if (!removed) return false;
  removed.parentID = parentID ?? undefined;
  const at = target >= 0 ? (target > index ? target : target + 1) : layers.length;
  layers.splice(at, 0, removed);
  adoptClipping(layers, id);
  releaseDetachedClipping(layers);
  return true;
}

/**
 * ⌘J / Duplicate Layer: every selected root (a selected folder brings its
 * contents), as one undo step. One copy sits just above its original; several
 * stack together, in their order, above the topmost original. The copies end
 * up selected.
 */
export function duplicateLayers(ctx: LayerContext, ids?: string[]): LayerOpResult | null {
  const layers = clone(ctx.layers);
  const byID = new Map(layers.map((l) => [l.id, l]));
  let roots: string[];
  if (ids === undefined) {
    const selected = new Set([...ctx.selectedLayerIDs, ...(ctx.activeLayerID !== null ? [ctx.activeLayerID] : [])]);
    const nested = new Set<string>();
    for (const id of selected) {
      for (const d of descendantIDs(layers, id)) nested.add(d);
    }
    roots = layers.map((l) => l.id).filter((id) => selected.has(id) && !nested.has(id));
  } else {
    roots = ids.filter((id) => byID.has(id));
  }
  if (roots.length === 0) return null;
  const copiesOf = new Map<string, string>();
  for (const id of roots) {
    const copy = insertCopy(layers, id, ctx);
    if (copy !== null) copiesOf.set(id, copy);
  }
  if (copiesOf.size === 0) return null;
  if (copiesOf.size > 1) {
    // Panel order, top first, so layers in different folders compare as seen.
    const originals = panelOrder(layers).filter((id) => copiesOf.has(id));
    if (originals.length > 0) {
      const top = originals[0]!;
      const parentID = layers.find((l) => l.id === top)?.parentID ?? null;
      let below: string | null = top;
      for (const original of [...originals].reverse()) {
        const copy = copiesOf.get(original)!;
        if (placeWithin(layers, copy, parentID, below)) below = copy;
      }
    }
  }
  const copies = new Set(copiesOf.values());
  const selectedCopies = layers.filter((l) => copies.has(l.id)).map((l) => l.id);
  const activeCopy =
    (ctx.activeLayerID !== null ? copiesOf.get(ctx.activeLayerID) : undefined) ??
    copiesOf.get(roots[0]!) ??
    selectedCopies[0] ??
    null;
  return done(ctx, layers, "Duplicate Layer", {
    activeLayerID: activeCopy,
    selectedLayerIDs: selectedCopies,
  });
}

// ---------------------------------------------------------------------------
// Grouping — LayerGroups.swift
// ---------------------------------------------------------------------------

/**
 * ⌘G: the selected roots (a selected folder carries its subtree) move into a
 * new "Folder N" that takes the topmost selected branch's spot in their
 * common parent. Reverses with ⇧⌘G.
 */
export function groupSelectedLayers(ctx: LayerContext): LayerOpResult | null {
  if (ctx.layers.length >= MAX_LAYERS) return null;
  const layers = clone(ctx.layers);
  const byID = new Map(layers.map((l) => [l.id, l]));
  const selected = new Set(ctx.selectedLayerIDs.filter((id) => byID.has(id)));
  const ancestorsOf = (id: string): (string | null)[] => {
    const chain: (string | null)[] = [];
    let parent: string | null = byID.get(id)?.parentID ?? null;
    while (parent !== null) {
      chain.push(parent);
      parent = byID.get(parent)?.parentID ?? null;
    }
    chain.push(null);
    return chain;
  };
  // A selected folder carries its subtree; selected descendants must not be
  // pulled out of it.
  const roots = new Set(
    [...selected].filter((id) => !ancestorsOf(id).some((a) => a !== null && selected.has(a))),
  );
  const ordered = hierarchyOrder(layers).filter((id) => roots.has(id));
  // Nearest common ancestor of everything selected (nil = root level).
  let parentID: string | null = null;
  if (ordered.length > 0) {
    for (const candidate of ancestorsOf(ordered[0]!)) {
      if (ordered.every((id) => ancestorsOf(id).includes(candidate))) {
        parentID = candidate;
        break;
      }
    }
  }
  const group: DocLayer = {
    id: makeId(ctx),
    name: nextName(layers, "Folder"),
    isVisible: true,
    isGroup: true,
    parentID: parentID ?? undefined,
  };
  // The wrapper goes at the topmost selected branch in the common parent:
  // each selected layer climbs to the branch under that parent, and the group
  // slots in after the highest of them in the kept array.
  const branches = ordered.map((id) => {
    let branch = id;
    for (;;) {
      const p: string | null = byID.get(branch)?.parentID ?? null;
      if (p === null || p === parentID) break;
      branch = p;
    }
    return branch;
  });
  const highest = findLastIndex(layers, (l) => branches.includes(l.id));
  const insertion =
    highest < 0 ? layers.length : layers.slice(0, highest + 1).filter((l) => !roots.has(l.id)).length;
  const kept = layers.filter((l) => !roots.has(l.id));
  kept.splice(Math.min(insertion, kept.length), 0, group);
  for (const id of ordered) {
    const child = byID.get(id);
    if (!child) continue;
    child.parentID = group.id;
    kept.push(child);
  }
  return withActive(ctx, kept, "Group Layers", group.id);
}

/** ⇧⌘G needs a folder: a plain layer has nothing to unwrap. */
export function canUngroupLayers(ctx: LayerContext): boolean {
  const active = activeLayer(ctx);
  return active !== null && isGroup(active);
}

/**
 * ⇧⌘G: the folder's direct children take its place among its own siblings,
 * in the order they had inside it, and the folder goes — its own opacity,
 * blend mode, mask and effects are discarded along with it.
 */
export function ungroupLayers(ctx: LayerContext): LayerOpResult | null {
  const group = activeLayer(ctx);
  if (group === null || !isGroup(group)) return null;
  const layers = clone(ctx.layers);
  const childIDs = new Set(
    layers.filter((l) => (l.parentID ?? null) === group.id).map((l) => l.id),
  );
  const children = layers.filter((l) => childIDs.has(l.id));
  for (const c of children) c.parentID = group.parentID ?? undefined;
  const next: DocLayer[] = [];
  for (const l of layers) {
    if (l.id === group.id) next.push(...children);
    else if (!childIDs.has(l.id)) next.push(l);
  }
  releaseDetachedClipping(next);
  return done(ctx, next, "Ungroup Layers", {
    activeLayerID: children[0]?.id ?? null,
    selectedLayerIDs: children.map((c) => c.id),
  });
}

/** Move Out of Folder: above its former group, at the group's own level. */
export function moveActiveLayerOutOfGroup(ctx: LayerContext): LayerOpResult | null {
  const active = activeLayer(ctx);
  if (active === null || active.parentID === undefined) return null;
  const group = ctx.layers.find((l) => l.id === active.parentID);
  if (!group) return null;
  return reorderTo(ctx, active.id, group.parentID ?? null, group.id, false);
}

// ---------------------------------------------------------------------------
// Reordering — EditorSession moveActiveLayer / NativeLayerList place
// ---------------------------------------------------------------------------

/** [ and ]: the active layer swaps with the sibling above/below; edges stop it. */
export function canMoveLayer(ctx: LayerContext, offset: number): boolean {
  const active = activeLayer(ctx);
  if (active === null) return false;
  const siblings = ctx.layers.filter((l) => (l.parentID ?? null) === (active.parentID ?? null));
  const index = siblings.findIndex((l) => l.id === active.id);
  if (index < 0) return false;
  return index + offset >= 0 && index + offset < siblings.length;
}

/** [ / ] keys: one sibling swap in the flat array ("Reorder Layers"). */
export function moveActiveLayer(ctx: LayerContext, offset: number): LayerOpResult | null {
  if (!canMoveLayer(ctx, offset)) return null;
  const layers = clone(ctx.layers);
  const active = layers.find((l) => l.id === ctx.activeLayerID)!;
  const siblings = layers.filter((l) => (l.parentID ?? null) === (active.parentID ?? null));
  const sIndex = siblings.findIndex((l) => l.id === active.id);
  const other = siblings[sIndex + offset]!;
  const a = layers.findIndex((l) => l.id === active.id);
  const b = layers.findIndex((l) => l.id === other.id);
  const tmp = layers[a]!;
  layers[a] = layers[b]!;
  layers[b] = tmp;
  return done(ctx, layers, "Reorder Layers");
}

/**
 * One layer (with its whole subtree) placed above `aboveID` inside `parentID`
 * — the shared core of drag-drop and Move Out of Folder (Swift placeLayer).
 */
function reorderTo(
  ctx: LayerContext,
  id: string,
  parentID: string | null,
  aboveID: string | null,
  atBottom: boolean,
): LayerOpResult | null {
  const layers = clone(ctx.layers);
  const moved = layers.find((l) => l.id === id);
  if (!moved || aboveID === id) return null;
  // canPlaceLayer: the destination parent must be a folder (or root), never
  // the layer itself or one of its descendants.
  if (parentID !== null) {
    const parent = layers.find((l) => l.id === parentID);
    if (!parent || !isGroup(parent)) return null;
    if (parentID === id || descendantIDs(layers, id).has(parentID)) return null;
  }
  const blockIDs = descendantIDs(layers, id);
  blockIDs.add(id);
  if (aboveID !== null && blockIDs.has(aboveID)) return null;
  const block = layers.filter((l) => blockIDs.has(l.id));
  const without = layers.filter((l) => !blockIDs.has(l.id));
  let insertion = atBottom ? 0 : without.length;
  if (aboveID !== null) {
    const t = without.findIndex((l) => l.id === aboveID && (l.parentID ?? null) === parentID);
    if (t < 0) return null;
    insertion = t + 1;
  }
  const root = layers.find((l) => l.id === id)!;
  root.parentID = parentID ?? undefined;
  without.splice(insertion, 0, ...block);
  adoptClipping(without, id);
  releaseDetachedClipping(without);
  return withActive(ctx, without, "Move Layer", id);
}

/**
 * Panel drag-drop on the bottom→top flat array: the layer at `fromIndex`
 * (with its subtree) lands just above the layer at `toTargetIndex`, sharing
 * its parent — dropping ON a folder row means into that folder, on top of
 * its contents, which is how layers move in and out of groups
 * (NativeLayerList place: `.on` a group = parent, `.above` a row = sibling).
 */
export function reorder(ctx: LayerContext, fromIndex: number, toTargetIndex: number): LayerOpResult | null {
  if (!Number.isInteger(fromIndex) || !Number.isInteger(toTargetIndex)) return null;
  if (fromIndex < 0 || fromIndex >= ctx.layers.length) return null;
  if (toTargetIndex < 0 || toTargetIndex >= ctx.layers.length) return null;
  if (fromIndex === toTargetIndex) return null;
  const target = ctx.layers[toTargetIndex]!;
  const moved = ctx.layers[fromIndex]!;
  if (descendantIDs(ctx.layers, moved.id).has(target.id)) return null;
  if (isGroup(target)) return reorderTo(ctx, moved.id, target.id, null, false);
  return reorderTo(ctx, moved.id, target.parentID ?? null, target.id, false);
}

// ---------------------------------------------------------------------------
// Clipping masks (⌥⌘G) — LiveLayerMask.swift
// ---------------------------------------------------------------------------

/** The clip graph stays a forest of chains onto non-group, non-adjustment layers. */
function clipGraphValid(layers: DocLayer[], targetID: string, sourceID: string): boolean {
  const sourceOf = new Map(layers.map((l) => [l.id, l.maskSourceID]));
  sourceOf.set(targetID, sourceID);
  const byID = new Map(layers.map((l) => [l.id, l]));
  for (const l of layers) {
    const seen = new Set<string>();
    let current: string | undefined = l.id;
    while (current !== undefined) {
      if (seen.has(current)) return false; // cycle
      seen.add(current);
      const record = byID.get(current);
      if (!record) return false;
      const source = sourceOf.get(current);
      if (source !== undefined) {
        if (isGroup(record)) return false;
        const src = byID.get(source);
        if (!src || isGroup(src) || src.adjustment !== undefined) return false;
      }
      current = sourceOf.get(current);
    }
  }
  return true;
}

export function canLinkMask(ctx: LayerContext, sourceId: string, targetId: string): boolean {
  if (sourceId === targetId) return false;
  const source = ctx.layers.find((l) => l.id === sourceId);
  const target = ctx.layers.find((l) => l.id === targetId);
  if (!source || !target || isGroup(source) || isGroup(target)) return false;
  if (source.adjustment !== undefined) return false;
  return clipGraphValid(ctx.layers, targetId, sourceId);
}

/** Links `targetId` to clip through `sourceId` ("Create Clipping Mask"). */
export function linkMask(ctx: LayerContext, sourceId: string, targetId: string): LayerOpResult | null {
  if (!canLinkMask(ctx, sourceId, targetId)) return null;
  const layers = clone(ctx.layers);
  const target = layers.find((l) => l.id === targetId);
  if (!target || target.maskSourceID === sourceId) return null; // already linked
  target.maskSourceID = sourceId;
  return done(ctx, layers, "Create Clipping Mask");
}

/**
 * Releasing a base releases its clipped children above it that share that
 * base; releasing a child leaves lower siblings untouched.
 */
function removeLiveMask(ctx: LayerContext, targetId: string): LayerOpResult | null {
  const layers = clone(ctx.layers);
  const target = layers.find((l) => l.id === targetId);
  if (!target || target.maskSourceID === undefined) return null;
  const source = target.maskSourceID;
  const siblings = layers.filter((l) => (l.parentID ?? null) === (target.parentID ?? null));
  const tIndex = siblings.findIndex((l) => l.id === targetId);
  const releases: string[] = [];
  for (let i = tIndex; i < siblings.length; i++) {
    const s = siblings[i]!;
    if (s.id === targetId || s.maskSourceID === source) releases.push(s.id);
    else break;
  }
  for (const id of releases) {
    const l = layers.find((x) => x.id === id);
    if (l) delete l.maskSourceID;
  }
  return done(ctx, layers, "Release Clipping Mask");
}

/** Option-click state: can this layer create or release a clipping mask? */
export function canToggleClippingMask(ctx: LayerContext, id: string): boolean {
  const layer = ctx.layers.find((l) => l.id === id);
  if (!layer || isGroup(layer)) return false;
  if (layer.maskSourceID !== undefined) return true;
  const siblings = ctx.layers.filter((l) => (l.parentID ?? null) === (layer.parentID ?? null));
  const index = siblings.findIndex((l) => l.id === id);
  if (index <= 0) return false;
  const below = siblings[index - 1]!;
  if (isGroup(below)) return false;
  return canLinkMask(ctx, below.maskSourceID ?? below.id, id);
}

/**
 * ⌥⌘G: clips to the next lower sibling — sharing its base when that one is
 * already clipped — or releases the existing clip.
 */
export function toggleClippingMask(ctx: LayerContext, id?: string): LayerOpResult | null {
  const targetId = id ?? ctx.activeLayerID;
  if (targetId === null) return null;
  const layer = ctx.layers.find((l) => l.id === targetId);
  if (!layer || isGroup(layer)) return null;
  if (layer.maskSourceID !== undefined) return removeLiveMask(ctx, targetId);
  const siblings = ctx.layers.filter((l) => (l.parentID ?? null) === (layer.parentID ?? null));
  const index = siblings.findIndex((l) => l.id === targetId);
  if (index <= 0) return null;
  const below = siblings[index - 1]!;
  if (isGroup(below)) return null;
  return linkMask(ctx, below.maskSourceID ?? below.id, targetId);
}

// ---------------------------------------------------------------------------
// Merge family (⌘E) — LayerMerge.swift
// ---------------------------------------------------------------------------

export interface MergePlan {
  /** The layers that composite, bottom → top. */
  ids: string[];
  /** Everything that goes: the merged layers and their subtrees. */
  removed: Set<string>;
  /** The result layer's name and parent. */
  name: string;
  parentID: string | null;
  /** The layer whose slot the result takes. */
  anchor: string;
  action: string;
}

/**
 * What ⌘E merges, in stacking order, and where the result goes; null when
 * there is nothing to merge. One layer merges with the layer beneath it in
 * the same folder; several selected layers merge together (with anything
 * their folders hold); a folder merges its contents, and the folder goes.
 */
export function mergePlan(ctx: LayerContext): MergePlan | null {
  const layers = ctx.layers;
  const active = activeLayer(ctx);
  if (active === null) return null;
  if (ctx.selectedLayerIDs.length > 1) {
    const picked = new Set<string>();
    for (const id of ctx.selectedLayerIDs) {
      picked.add(id);
      for (const d of descendantIDs(layers, id)) picked.add(d);
    }
    const ordered = layers.filter((l) => picked.has(l.id));
    if (!ordered.some((l) => !isGroup(l))) return null;
    const top = [...ordered].reverse().find((l) => ctx.selectedLayerIDs.includes(l.id));
    if (!top) return null;
    return {
      ids: ordered.map((l) => l.id),
      removed: picked,
      name: top.name,
      parentID: top.parentID ?? null,
      anchor: top.id,
      action: "Merge Layers",
    };
  }
  if (isGroup(active)) {
    const inside = descendantIDs(layers, active.id);
    const hasPixels = [...inside].some((id) => {
      const l = layers.find((x) => x.id === id);
      return l !== undefined && !isGroup(l);
    });
    if (!hasPixels) return null;
    const ids = layers
      .filter((l) => inside.has(l.id) || l.id === active.id)
      .map((l) => l.id);
    return {
      ids,
      removed: new Set(ids),
      name: active.name,
      parentID: active.parentID ?? null,
      anchor: active.id,
      action: "Merge Group",
    };
  }
  const index = layers.findIndex((l) => l.id === active.id);
  let below: DocLayer | undefined;
  for (let i = index - 1; i >= 0; i--) {
    const l = layers[i]!;
    if ((l.parentID ?? null) === (active.parentID ?? null)) {
      below = l;
      break;
    }
  }
  if (!below || isGroup(below)) return null;
  return {
    ids: [below.id, active.id],
    removed: new Set([below.id, active.id]),
    name: below.name,
    parentID: active.parentID ?? null,
    anchor: active.id,
    action: "Merge Down",
  };
}

export function canMergeLayers(ctx: LayerContext): boolean {
  return mergePlan(ctx) !== null;
}

/** The merge item's title, which follows the selection (Swift mergeTitle). */
export function mergeTitle(ctx: LayerContext): string {
  return mergePlan(ctx)?.action ?? "Merge Down";
}

/**
 * ⌘E. The layers composite as the canvas shows them — blend modes, opacity,
 * masks, clipping and adjustments baked in — into one pixel layer, in their
 * place, as one undo step. The pixels themselves come from Go
 * render.Render over the kept subset (with outside parents and clip sources
 * cut loose); this function owns the ordering: the result is a fresh layer
 * with the plan's name and parent, inserted at the anchor's slot once the
 * removed layers before it are discounted, layers clipped to anything merged
 * re-pointed to the result.
 */
export function mergeLayers(ctx: LayerContext): LayerOpResult | null {
  const plan = mergePlan(ctx);
  if (plan === null) return null;
  const layers = clone(ctx.layers);
  const merged: DocLayer = {
    id: makeId(ctx),
    name: plan.name,
    isVisible: true,
    parentID: plan.parentID ?? undefined,
  };
  const next = layers.filter((l) => !plan.removed.has(l.id));
  // Layers clipped to anything that was merged now clip to the result.
  for (const l of next) {
    if (l.maskSourceID !== undefined && plan.removed.has(l.maskSourceID)) {
      l.maskSourceID = merged.id;
    }
  }
  const slot = layers.findIndex((l) => l.id === plan.anchor);
  const at = slot < 0 ? layers.length : slot;
  let removedBefore = 0;
  for (let i = 0; i < at; i++) {
    if (plan.removed.has(layers[i]!.id)) removedBefore++;
  }
  const insertion = Math.min(Math.max(0, at - removedBefore), next.length);
  next.splice(insertion, 0, merged);
  return withActive(ctx, next, plan.action, merged.id);
}

// ---------------------------------------------------------------------------
// Deleting — EditorSession.deleteLayer / LiveLayerMask.finishDeletingLayer
// ---------------------------------------------------------------------------

/**
 * Removes a layer and its subtree, releasing the clipping of layers that
 * stay (the bake-or-unlink alert is UI work; this is the unlink path). When
 * the active layer went, the layer that took its slot is activated — or the
 * new last one at the end of the list, the CloseTab neighbor rule.
 */
function finishDelete(
  layers: DocLayer[],
  id: string,
  active: string | null,
): { active: string | null } | null {
  const index = layers.findIndex((l) => l.id === id);
  if (index < 0) return null;
  const removed = descendantIDs(layers, id);
  removed.add(id);
  const next = layers.filter((l) => !removed.has(l.id));
  for (const l of next) {
    if (l.maskSourceID !== undefined && removed.has(l.maskSourceID)) delete l.maskSourceID;
  }
  layers.length = 0;
  layers.push(...next);
  if (active !== null && removed.has(active)) {
    return { active: next.length === 0 ? null : next[Math.min(index, next.length - 1)]!.id };
  }
  return { active };
}

export function deleteLayer(ctx: LayerContext, id: string): LayerOpResult | null {
  const layers = clone(ctx.layers);
  const r = finishDelete(layers, id, ctx.activeLayerID);
  if (r === null) return null;
  if (r.active === ctx.activeLayerID) return done(ctx, layers, "Delete Layer");
  return withActive(ctx, layers, "Delete Layer", r.active);
}

export function deleteActiveLayer(ctx: LayerContext): LayerOpResult | null {
  if (ctx.activeLayerID === null) return null;
  return deleteLayer(ctx, ctx.activeLayerID);
}

/**
 * Deletes every selected layer as one undo step (a selected folder takes its
 * contents); with one layer selected, just that one.
 */
export function deleteSelectedLayers(ctx: LayerContext): LayerOpResult | null {
  const ids = ctx.layers.map((l) => l.id).filter((id) => ctx.selectedLayerIDs.includes(id));
  if (ids.length <= 1) return deleteActiveLayer(ctx);
  const layers = clone(ctx.layers);
  let active = ctx.activeLayerID;
  for (const id of ids) {
    const r = finishDelete(layers, id, active);
    if (r !== null) active = r.active;
  }
  if (active === ctx.activeLayerID) return done(ctx, layers, "Delete Layers");
  return withActive(ctx, layers, "Delete Layers", active);
}

/**
 * The trash button and Delete without a selection: an effect row goes first,
 * then a targeted mask, otherwise every selected layer does.
 */
export function deleteLayerOrMask(ctx: LayerContext, target: DeleteTarget = { kind: "layer" }): LayerOpResult | null {
  if (target.kind === "effect") return deleteEffect(ctx, target.layerId, target.effect);
  if (target.kind === "mask") {
    const active = activeLayer(ctx);
    if (active !== null && active.maskFile !== undefined && ctx.selectedLayerIDs.length <= 1) {
      return deleteLayerMask(ctx);
    }
  }
  return deleteSelectedLayers(ctx);
}

// ---------------------------------------------------------------------------
// Context menu — NativeLayerList.contextMenu, titles that follow the selection
// ---------------------------------------------------------------------------

/**
 * The context menu's titles for the current selection. Items the context
 * disables (Rename… on a multi-selection, the mask block without a single
 * masked layer) are left out; the merge item always appears, titled for the
 * context. When the mask thumbnail or an effect row is targeted, the delete
 * slot becomes "Delete Mask" / "Remove <Effect>" (deleteLayerOrMask routing);
 * a mask-targeted menu does not repeat the mask block's own Delete Mask.
 */
export function menuTitle(ctx: LayerContext, target: MenuTarget = { kind: "layer" }): string[] {
  const active = activeLayer(ctx);
  if (active === null) return [];
  const single = ctx.selectedLayerIDs.length === 1;
  const titles: string[] = ["Duplicate Layer"];
  if (single) titles.push("Rename…");
  if (target.kind === "effect") {
    titles.push(`Remove ${EFFECT_KIND_TITLES[target.effect]}`);
  } else if (target.kind === "mask" && active.maskFile !== undefined) {
    titles.push("Delete Mask");
  } else {
    titles.push(ctx.selectedLayerIDs.length > 1 ? "Delete Selected Layers" : "Delete Layer");
  }
  if (canToggleClippingMask(ctx, active.id)) {
    titles.push(active.maskSourceID !== undefined ? "Release Clipping Mask" : "Create Clipping Mask");
  }
  titles.push("Group Selected Layers");
  if (isGroup(active)) titles.push("Ungroup Layers");
  if (active.parentID !== undefined) titles.push("Move Out of Folder");
  titles.push(mergeTitle(ctx));
  if (single && active.maskFile !== undefined) {
    const maskDeduped = target.kind === "mask";
    titles.push(active.maskEnabled === false ? "Enable Mask" : "Disable Mask");
    if (!maskDeduped) titles.push("Delete Mask");
    if (!isGroup(active) && active.adjustment === undefined) {
      titles.push(active.maskLinked === false ? "Link Mask" : "Unlink Mask");
    }
  } else if (single) {
    titles.push("Add Mask");
  }
  titles.push(active.isVisible ? "Hide Layer" : "Show Layer");
  return titles;
}
