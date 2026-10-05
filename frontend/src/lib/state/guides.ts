/**
 * Guides: user-placed alignment lines, editable from the rulers.
 *
 * Semantics ported from the macOS original (read-only truth):
 * - `reference/Swift/Compositor/Document/Guides.swift` — CanvasGuide, GuideDrag
 *   and the EditorSession drag lifecycle (create → move → finish/cancel; the
 *   document is only updated when the drag finishes).
 * - `internal/domain/document.go` — Guide { id, axis, position } with the
 *   manifest JSON shape; `internal/domain/validate.go` — at most
 *   MAX_GUIDES guides, |position| ≤ MAX_GUIDE_POSITION, position may be
 *   negative.
 *
 * Undo is a pure state machine: every mutating operation returns a
 * `GuideEdit` whose `undo` mutations restore the prior state; the caller
 * feeds them back via `applyEdits`. Drag positions are stored raw — snapping
 * during a drag is the caller's job (see ./snap.ts), mirroring Swift's
 * `snappedGuidePosition` living beside the state, not inside it.
 */

/** Orientation of a guide; values match Go's domain.GuideAxis JSON encoding. */
export type GuideAxis = "horizontal" | "vertical";

/** One alignment guide. Horizontal guides sit at a document Y, vertical at an X. */
export interface Guide {
  id: string;
  axis: GuideAxis;
  /** Document pixels; may be negative (manifests allow it). */
  position: number;
}

/** Limits from internal/domain/validate.go (project-format.md "Limits"). */
export const MAX_GUIDES = 1_000;
export const MAX_GUIDE_POSITION = 1_000_000;

const AXES: readonly GuideAxis[] = ["horizontal", "vertical"];

export function isGuideAxis(value: unknown): value is GuideAxis {
  return typeof value === "string" && (AXES as readonly string[]).includes(value);
}

/** A finite position within the manifest limit. */
export function isValidGuidePosition(position: number): boolean {
  return Number.isFinite(position) && Math.abs(position) <= MAX_GUIDE_POSITION;
}

/**
 * True when a dragged guide position has left the document along its own
 * axis — the "release over the ruler deletes the guide" gesture. Swift
 * decides this from the actual pointer position over the ruler control;
 * here we approximate it as "position beyond the document edge", with an
 * optional tolerance in document pixels.
 */
export function overRuler(
  position: number,
  axis: GuideAxis,
  doc: { width: number; height: number },
  tolerance = 0,
): boolean {
  const length = axis === "vertical" ? doc.width : doc.height;
  return position < -tolerance || position > length + tolerance;
}

/**
 * One inverse step. Applying it (via applyEdits) undoes part of an edit.
 * - insert: put `guide` back at `index` (undoes a removal/clear)
 * - remove: drop the guide with `id` (undoes an add)
 * - setPosition: restore a prior position (undoes a move)
 */
export type GuideMutation =
  | { kind: "insert"; index: number; guide: Guide }
  | { kind: "remove"; id: string }
  | { kind: "setPosition"; id: string; position: number };

/** A completed change plus everything needed to undo it. */
export interface GuideEdit {
  /** History label, mirroring the Swift edit names. */
  label: "New Guide" | "Move Guide" | "Delete Guide" | "Clear Guides";
  undo: GuideMutation[];
}

/** In-progress create or move; the document is updated only on finish. */
export interface GuideDrag {
  id: string;
  axis: GuideAxis;
  position: number;
  isNew: boolean;
  /** The position the guide started from; null for a new guide. */
  original: number | null;
}

function newGuideId(): string {
  return typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : `guide-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

/**
 * The guide list plus locks and the drag in progress. Pure document-space
 * state: no viewport, no stores, no I/O.
 */
export class GuidesState {
  #guides: Guide[] = [];
  #locked = new Set<string>();
  #drag: GuideDrag | null = null;

  /** Committed guides (excludes any drag in progress — see displayedGuides). */
  get guides(): readonly Guide[] {
    return this.#guides;
  }

  /** The drag in progress, if any (Swift GuideDrag). */
  get drag(): GuideDrag | null {
    return this.#drag;
  }

  /** Guides as currently shown, including an in-progress drag (Swift displayedGuides). */
  get displayedGuides(): readonly Guide[] {
    if (!this.#drag) return this.#guides;
    const current: Guide = {
      id: this.#drag.id,
      axis: this.#drag.axis,
      position: this.#drag.position,
    };
    const index = this.#guides.findIndex((g) => g.id === current.id);
    if (index >= 0) {
      const copy = this.#guides.slice();
      copy[index] = current;
      return copy;
    }
    if (this.#drag.isNew) return [...this.#guides, current];
    return this.#guides;
  }

  /** Guides locked against moving and deleting. Locks are session state, not undoable. */
  isLocked(id: string): boolean {
    return this.#locked.has(id);
  }

  setLocked(id: string, locked: boolean): void {
    if (locked) this.#locked.add(id);
    else this.#locked.delete(id);
  }

  get canClearGuides(): boolean {
    return this.#guides.length > 0;
  }

  /**
   * Appends a guide. Rejects (returning null): duplicate ids, more than
   * MAX_GUIDES guides, invalid positions and invalid axes.
   */
  addGuide(guide: Guide): GuideEdit | null {
    if (!isGuideAxis(guide.axis) || !isValidGuidePosition(guide.position)) return null;
    if (typeof guide.id !== "string" || guide.id === "") return null;
    if (this.#guides.some((g) => g.id === guide.id)) return null;
    if (this.#guides.length >= MAX_GUIDES) return null;

    this.#guides.push({ ...guide });
    return { label: "New Guide", undo: [{ kind: "remove", id: guide.id }] };
  }

  /**
   * Moves a guide to a new position. Returns null (nothing to record) when
   * the guide is unknown or locked, the position is invalid, or nothing
   * changed — mirroring Swift's `drag.original != drag.position` guard.
   */
  moveGuide(id: string, position: number): GuideEdit | null {
    if (this.#locked.has(id) || !isValidGuidePosition(position)) return null;
    const guide = this.#guides.find((g) => g.id === id);
    if (!guide || guide.position === position) return null;

    const from = guide.position;
    guide.position = position;
    return { label: "Move Guide", undo: [{ kind: "setPosition", id, position: from }] };
  }

  /** Removes a guide; locked guides may not be deleted. */
  deleteGuide(id: string): GuideEdit | null {
    if (this.#locked.has(id)) return null;
    const index = this.#guides.findIndex((g) => g.id === id);
    if (index < 0) return null;

    const [removed] = this.#guides.splice(index, 1);
    return { label: "Delete Guide", undo: [{ kind: "insert", index, guide: removed }] };
  }

  /**
   * Clears every guide (locked ones too, like Photoshop's Clear Guides) and
   * returns them in order for undo. Null when there was nothing to clear.
   */
  clearAll(): GuideEdit | null {
    if (this.#guides.length === 0) return null;
    const removed = this.#guides;
    this.#guides = [];
    return {
      label: "Clear Guides",
      undo: removed.map((guide, index) => ({ kind: "insert", index, guide }) as GuideMutation),
    };
  }

  // --- Drag lifecycle (Swift Guides.swift beginGuideCreation … finishGuideDrag) ---

  /** Starts creating a guide; `id` defaults to a fresh UUID-style id. */
  beginCreation(axis: GuideAxis, position: number, id: string = newGuideId()): boolean {
    if (!isGuideAxis(axis)) return false;
    this.#drag = { id, axis, position, isNew: true, original: null };
    return true;
  }

  /** Starts moving an existing guide; refused for unknown or locked guides. */
  beginMove(id: string): boolean {
    const guide = this.#guides.find((g) => g.id === id);
    if (!guide || this.#locked.has(id)) return false;
    this.#drag = {
      id: guide.id,
      axis: guide.axis,
      position: guide.position,
      isNew: false,
      original: guide.position,
    };
    return true;
  }

  /** Updates the in-progress drag position (raw; snapping is the caller's job). */
  moveDrag(position: number): void {
    if (!this.#drag || !Number.isFinite(position)) return;
    this.#drag = { ...this.#drag, position };
  }

  /**
   * Finishes the drag. `delete` is true when the pointer was released over a
   * ruler (see overRuler): a new guide is discarded, an existing one removed.
   * Otherwise a new guide is committed or a moved guide updated — an
   * unchanged move records no edit.
   */
  finishDrag(deleteFlag: boolean): GuideEdit | null {
    const drag = this.#drag;
    if (!drag) return null;
    this.#drag = null;

    if (deleteFlag) {
      if (drag.isNew) return null;
      return this.deleteGuide(drag.id);
    }
    if (drag.isNew) {
      return this.addGuide({ id: drag.id, axis: drag.axis, position: drag.position });
    }
    return this.moveGuide(drag.id, drag.position);
  }

  /** Aborts the drag without touching the document. */
  cancelDrag(): void {
    this.#drag = null;
  }

  /** Applies (inverse) mutations by hand — how a history stack replays edits. */
  applyEdits(mutations: readonly GuideMutation[]): void {
    for (const m of mutations) {
      switch (m.kind) {
        case "insert":
          this.#guides.splice(Math.min(m.index, this.#guides.length), 0, { ...m.guide });
          break;
        case "remove": {
          const index = this.#guides.findIndex((g) => g.id === m.id);
          if (index >= 0) this.#guides.splice(index, 1);
          break;
        }
        case "setPosition": {
          const guide = this.#guides.find((g) => g.id === m.id);
          if (guide) guide.position = m.position;
          break;
        }
      }
    }
  }
}

// --- Manifest serialization (the `guides` array of the .comp document JSON) ---

/** The exact JSON shape Go's domain.Guide marshals to. */
export interface ManifestGuide {
  id: string;
  axis: GuideAxis;
  position: number;
}

/** Guides as stored in the manifest (Go marshals `guides` with omitempty). */
export function toManifestGuides(guides: readonly Guide[]): ManifestGuide[] {
  return guides.map((g) => ({ id: g.id, axis: g.axis, position: g.position }));
}

export type FromManifestResult =
  | { ok: true; guides: Guide[] }
  | { ok: false; error: string };

/**
 * Reads the manifest `guides` array. An absent field reads as no guides (Go's
 * Guides() returns nil for versions 1–7 or when guides were removed). Every
 * entry is validated against the same limits Go's Validate enforces: known
 * axis, finite position within ±MAX_GUIDE_POSITION, at most MAX_GUIDES
 * entries — plus duplicate-id rejection, which this state machine requires
 * even though Go's Validate does not check it.
 */
export function fromManifestGuides(raw: unknown): FromManifestResult {
  if (raw === undefined || raw === null) return { ok: true, guides: [] };
  if (!Array.isArray(raw)) return { ok: false, error: "guides must be an array" };
  if (raw.length > MAX_GUIDES) {
    return { ok: false, error: `参考线数量 ${raw.length} 超过 ${MAX_GUIDES} 上限` };
  }

  const guides: Guide[] = [];
  const seen = new Set<string>();
  for (const entry of raw) {
    if (typeof entry !== "object" || entry === null) {
      return { ok: false, error: "参考线条目必须是对象" };
    }
    const { id, axis, position } = entry as Record<string, unknown>;
    if (typeof id !== "string" || id === "") {
      return { ok: false, error: "参考线缺少有效 id" };
    }
    if (seen.has(id)) return { ok: false, error: `参考线 ID 重复: ${id}` };
    seen.add(id);
    if (!isGuideAxis(axis)) {
      return { ok: false, error: `参考线 ${id} 方向无效: ${String(axis)}` };
    }
    if (typeof position !== "number" || !isValidGuidePosition(position)) {
      return { ok: false, error: `参考线 ${id} 位置超出 ±${MAX_GUIDE_POSITION} px` };
    }
    guides.push({ id, axis, position });
  }
  return { ok: true, guides };
}
