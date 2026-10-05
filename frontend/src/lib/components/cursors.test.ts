import { describe, expect, it } from "vitest";
import { Modifier, has } from "../state/modifiers";
import {
  COMPOSITE_CURSOR_COUNT,
  cursorForTool,
  isDataUriCursor,
  selectionCursor,
} from "./cursors";

const NONE = 0;

describe("tool -> cursor mapping (EditorCanvas.swift toolCursor, line 1444)", () => {
  it("gives the hand tool and a held Space the open hand (grab)", () => {
    expect(cursorForTool({ tool: "hand", bits: NONE })).toContain("grab");
    expect(cursorForTool({ tool: "move", bits: Modifier.Space })).toContain(
      "grab",
    );
  });

  it("shows the closed hand while panning", () => {
    expect(cursorForTool({ tool: "move", bits: NONE, panning: true })).toContain(
      "grabbing",
    );
  });

  it("maps the type tool to the I-beam and idle to the plain arrow", () => {
    expect(cursorForTool({ tool: "type", bits: NONE })).toBe("text");
    expect(cursorForTool({ tool: "idle", bits: NONE })).toBe("default");
  });

  it("zoom tool flips between zoom in and zoom out with Alt", () => {
    const zoomIn = cursorForTool({ tool: "zoom", bits: NONE });
    const zoomOut = cursorForTool({ tool: "zoom", bits: Modifier.Alt });
    expect(zoomIn).not.toBe(zoomOut);
    expect(isDataUriCursor(zoomIn)).toBe(true);
    expect(isDataUriCursor(zoomOut)).toBe(true);
  });

  it("selection tools get a crosshair badge for add (Shift) and subtract (Alt)", () => {
    for (const tool of ["marquee", "lasso", "wand"] as const) {
      const replace = cursorForTool({ tool, bits: NONE });
      const add = cursorForTool({ tool, bits: Modifier.Shift });
      const subtract = cursorForTool({ tool, bits: Modifier.Alt });
      expect(isDataUriCursor(replace)).toBe(true);
      expect(add).not.toBe(replace);
      expect(subtract).not.toBe(replace);
      expect(add).not.toBe(subtract);
    }
  });

  it("eyedropper badges add (Shift) and remove (Alt)", () => {
    const plain = cursorForTool({ tool: "eyedropper", bits: NONE });
    expect(isDataUriCursor(plain)).toBe(true);
    expect(cursorForTool({ tool: "eyedropper", bits: Modifier.Shift })).not.toBe(plain);
    expect(cursorForTool({ tool: "eyedropper", bits: Modifier.Alt })).not.toBe(plain);
  });

  it("move tool approximates the move badge, Alt-drag the duplicate badge", () => {
    expect(cursorForTool({ tool: "move", bits: NONE })).toContain("move");
    expect(cursorForTool({ tool: "move", bits: Modifier.Alt })).toContain("copy");
  });

  it("painting/editing tools fall back to the crosshair", () => {
    for (const tool of ["crop", "brush", "spotHealing", "blur", "gradient", "shape"]) {
      expect(cursorForTool({ tool, bits: NONE })).toBe("crosshair");
    }
    // Clone Stamp with Alt picks a source: plain crosshair (EditorCanvas.swift:1467).
    expect(cursorForTool({ tool: "cloneStamp", bits: Modifier.Alt })).toBe(
      "crosshair",
    );
  });

  it("Space overrides every tool cursor (Swift checks spaceHeld first)", () => {
    for (const tool of ["zoom", "type", "brush", "idle"]) {
      expect(cursorForTool({ tool, bits: Modifier.Space })).toContain("grab");
    }
  });
});

describe("selection cursors", () => {
  it("distinguishes replace, add and subtract for every selection tool", () => {
    for (const icon of ["marquee", "lasso", "wand"] as const) {
      const cursors = ["replace", "add", "subtract"].map((mode) =>
        selectionCursor(icon, mode as "replace" | "add" | "subtract"),
      );
      expect(new Set(cursors).size).toBe(3);
    }
  });
});

describe("data-URI cursor format", () => {
  it("embeds an SVG with hotspot coordinates and a CSS keyword fallback", () => {
    const c = cursorForTool({ tool: "zoom", bits: NONE });
    // url("data:image/svg+xml;utf8,...") HX HY, fallback
    expect(c).toMatch(
      /^url\("data:image\/svg\+xml;utf8,[^"]+"\) \d+ \d+, [a-z-]+$/,
    );
  });

  it("every composite cursor is visual-smoke level but well-formed", () => {
    // Walk every tool x modifier combination the surface can produce.
    const combos: Array<{ tool: string; bits: number; panning?: boolean }> = [];
    for (const tool of [
      "move", "marquee", "lasso", "wand", "crop", "brush", "spotHealing",
      "cloneStamp", "blur", "gradient", "shape", "type", "eyedropper",
      "hand", "zoom", "idle",
    ]) {
      for (const bits of [
        NONE,
        Modifier.Shift,
        Modifier.Alt,
        Modifier.Ctrl,
        Modifier.Space,
        Modifier.Shift | Modifier.Alt,
      ]) {
        combos.push({ tool, bits });
      }
    }
    combos.push({ tool: "move", bits: NONE, panning: true });

    for (const c of combos) {
      const cursor = cursorForTool(c);
      const ok =
        isDataUriCursor(cursor) ||
        [
          "grab", "grabbing", "text", "default", "crosshair", "move",
          "copy", "zoom-in", "zoom-out",
        ].some((k) => cursor === k);
      expect(ok, `cursor for ${JSON.stringify(c)}: ${cursor}`).toBe(true);
    }
  });

  it("ships at least 10 composite badge cursors (ticket V07)", () => {
    expect(COMPOSITE_CURSOR_COUNT).toBeGreaterThanOrEqual(10);
  });

  it("encodes the plus badge for add cursors and the minus badge for subtract", () => {
    const add = selectionCursor("marquee", "add");
    const subtract = selectionCursor("marquee", "subtract");
    // encodeURIComponent encodes '+' as %2B; the minus is a plain '-' in a path.
    expect(decodeURIComponent(add)).toContain("+");
    expect(decodeURIComponent(subtract)).toContain("M22 16h-4");
    expect(decodeURIComponent(subtract)).not.toContain("v4");
  });

  it("keeps has() import used for modifier queries", () => {
    expect(has(Modifier.Shift, Modifier.Shift)).toBe(true);
  });
});
