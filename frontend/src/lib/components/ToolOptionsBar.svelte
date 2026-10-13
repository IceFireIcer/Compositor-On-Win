<script lang="ts">
  import TextOptionsBar from "./TextOptionsBar.svelte";
  import {
    activeTool,
    brushSettings,
    cloneSettings,
    gradientSettings,
    healSettings,
    shapeSettings,
    selectionSettings,
    smudgeSettings,
    type SelectionCombination,
    toolModes,
    updateBrushSettings,
    updateCloneSettings,
    updateGradientSettings,
    updateHealSettings,
    updateShapeSettings,
    updateSelectionSettings,
    updateSmudgeSettings,
    updateToolModes,
    updateWandSettings,
    wandSettings,
  } from "../state/tools";
  import { parseHex, rgbToHex, setBackground, setForeground } from "../state/color";
  import { palette, setShowsSampleRing, showsSampleRing } from "../state/palette";

  function numberValue(event: Event): number {
    return Number((event.currentTarget as HTMLInputElement).value);
  }

  function selectValue(event: Event): string {
    return (event.currentTarget as HTMLSelectElement).value;
  }

  function setPaletteColor(which: "foreground" | "background", value: string): void {
    const color = parseHex(value);
    if (!color) return;
    palette.update((current) =>
      which === "foreground" ? setForeground(current, color) : setBackground(current, color),
    );
  }
</script>

{#if $activeTool === "type"}
  <TextOptionsBar />
{:else}
  <div class="options" role="toolbar" aria-label="Tool options">
    {#if $activeTool === "marquee"}
      <label>
        Shape
        <select
          aria-label="Marquee shape"
          value={$toolModes.marquee}
          onchange={(event) => updateToolModes({ marquee: selectValue(event) as "rect" | "ellipse" })}
        >
          <option value="rect">Rectangle</option>
          <option value="ellipse">Ellipse</option>
        </select>
      </label>
    {:else if $activeTool === "lasso"}
      <label>
        Mode
        <select
          aria-label="Lasso mode"
          value={$toolModes.lasso}
          onchange={(event) => updateToolModes({ lasso: selectValue(event) as "freehand" | "polygon" })}
        >
          <option value="freehand">Freehand</option>
          <option value="polygon">Polygon</option>
        </select>
      </label>
    {:else if $activeTool === "wand"}
      <label>
        Mode
        <select
          aria-label="Magic selection mode"
          value={$toolModes.wand}
          onchange={(event) => updateToolModes({ wand: selectValue(event) as "wand" | "object" })}
        >
          <option value="wand">Wand</option>
          <option value="object">Object</option>
        </select>
      </label>
      <label>
        Tolerance
        <input
          type="number"
          min="0"
          max="255"
          step="1"
          value={$wandSettings.tolerance}
          onchange={(event) => updateWandSettings({ tolerance: numberValue(event) })}
        />
      </label>
      <label class="check">
        <input
          type="checkbox"
          checked={$wandSettings.contiguous}
          onchange={(event) => updateWandSettings({ contiguous: (event.currentTarget as HTMLInputElement).checked })}
        />
        Contiguous
      </label>
      <label>
        Sample
        <select
          aria-label="Wand sample size"
          value={$wandSettings.sampleRadius}
          onchange={(event) => updateWandSettings({ sampleRadius: numberValue(event) })}
        >
          <option value="0">Point</option>
          <option value="1">3 x 3</option>
          <option value="2">5 x 5</option>
        </select>
      </label>
      <label class="check">
        <input
          type="checkbox"
          checked={$wandSettings.sampleAllLayers}
          onchange={(event) => updateWandSettings({ sampleAllLayers: (event.currentTarget as HTMLInputElement).checked })}
        />
        All layers
      </label>
    {:else if $activeTool === "gradient"}
      <label>
        Shape
        <select
          aria-label="Gradient shape"
          value={$gradientSettings.shape}
          onchange={(event) => updateGradientSettings({ shape: selectValue(event) as "Linear" | "Radial" })}
        >
          <option value="Linear">Linear</option>
          <option value="Radial">Radial</option>
        </select>
      </label>
      <label>
        Source
        <select
          aria-label="Gradient source"
          value={$gradientSettings.source}
          onchange={(event) => updateGradientSettings({ source: selectValue(event) as "foregroundBackground" | "foregroundTransparent" })}
        >
          <option value="foregroundBackground">Foreground to background</option>
          <option value="foregroundTransparent">Foreground to transparent</option>
        </select>
      </label>
      <label>
        Opacity
        <input
          type="number"
          min="1"
          max="100"
          step="1"
          value={Math.round($gradientSettings.opacity * 100)}
          onchange={(event) => updateGradientSettings({ opacity: numberValue(event) / 100 })}
        />
        %
      </label>
      <label class="check">
        <input
          type="checkbox"
          checked={$gradientSettings.reversed}
          onchange={(event) => updateGradientSettings({ reversed: (event.currentTarget as HTMLInputElement).checked })}
        />
        Reverse
      </label>
    {:else if $activeTool === "shape"}
      <label>
        Kind
        <select
          aria-label="Shape kind"
          value={$shapeSettings.kind}
          onchange={(event) => updateShapeSettings({ kind: selectValue(event) as "Rectangle" | "Ellipse" | "Line" })}
        >
          <option value="Rectangle">Rectangle</option>
          <option value="Ellipse">Ellipse</option>
          <option value="Line">Line</option>
        </select>
      </label>
      {#if $shapeSettings.kind === "Rectangle"}
        <label>
          Corner radius
          <input
            type="number"
            min="0"
            max="5000"
            step="1"
            value={$shapeSettings.cornerRadius}
            onchange={(event) => updateShapeSettings({ cornerRadius: numberValue(event) })}
          />
        </label>
      {/if}
      {#if $shapeSettings.kind === "Line"}
        <label>
          Line width
          <input
            type="number"
            min="1"
            max="5000"
            step="1"
            value={$shapeSettings.lineWidth}
            onchange={(event) => updateShapeSettings({ lineWidth: numberValue(event) })}
          />
        </label>
      {/if}
    {:else if $activeTool === "brush" || $activeTool === "cloneStamp" || $activeTool === "blur" || $activeTool === "spotHealing"}
      {#if $activeTool === "spotHealing"}
        <label>
          Mode
          <select
            aria-label="Healing mode"
            value={$healSettings.mode}
            onchange={(event) => updateHealSettings({ mode: numberValue(event) })}
          >
            <option value="0">Content-aware</option>
            <option value="1">Create texture</option>
            <option value="2">Proximity match</option>
          </select>
        </label>
      {:else if $activeTool === "brush"}
        <label>
          Mode
          <select
            aria-label="Brush mode"
            value={$toolModes.brush}
            onchange={(event) => updateToolModes({ brush: selectValue(event) as "paint" | "erase" })}
          >
            <option value="paint">Paint</option>
            <option value="erase">Erase</option>
          </select>
        </label>
      {:else if $activeTool === "blur"}
        <label>
          Mode
          <select
            aria-label="Blur tool mode"
            value={$toolModes.blur}
            onchange={(event) => updateToolModes({ blur: selectValue(event) as "liquify" | "blur" | "smudge" })}
          >
            <option value="liquify">Liquify</option>
            <option value="blur">Blur</option>
            <option value="smudge">Smudge</option>
          </select>
        </label>
      {/if}
      <label>
        Diameter
        <input
          type="number"
          min="1"
          max="2000"
          step="1"
          value={$activeTool === "spotHealing" ? $healSettings.diameter : $brushSettings.diameter}
          onchange={(event) => $activeTool === "spotHealing"
            ? updateHealSettings({ diameter: numberValue(event) })
            : updateBrushSettings({ diameter: numberValue(event) })}
        />
      </label>
      <label>
        Hardness
        <input
          type="number"
          min="0"
          max="100"
          step="1"
          value={Math.round(($activeTool === "spotHealing" ? $healSettings.hardness : $brushSettings.hardness) * 100)}
          onchange={(event) => $activeTool === "spotHealing"
            ? updateHealSettings({ hardness: numberValue(event) / 100 })
            : updateBrushSettings({ hardness: numberValue(event) / 100 })}
        />
        %
      </label>
      {#if $activeTool !== "blur"}
        <label>
          Opacity
          <input
            type="number"
            min="1"
            max="100"
            step="1"
            value={Math.round(($activeTool === "spotHealing" ? $healSettings.opacity : $brushSettings.opacity) * 100)}
            onchange={(event) => $activeTool === "spotHealing"
              ? updateHealSettings({ opacity: numberValue(event) / 100 })
              : updateBrushSettings({ opacity: numberValue(event) / 100 })}
          />
          %
        </label>
      {/if}
      {#if $activeTool === "brush"}
        <label>
          Smoothing
          <input
            type="number"
            min="0"
            max="100"
            step="1"
            value={$brushSettings.smoothing}
            onchange={(event) => updateBrushSettings({ smoothing: numberValue(event) })}
          />
        </label>
      {/if}
      {#if $activeTool === "cloneStamp"}
        <label class="check">
          <input
            type="checkbox"
            checked={$cloneSettings.aligned}
            onchange={(event) => updateCloneSettings({ aligned: (event.currentTarget as HTMLInputElement).checked })}
          />
          Aligned
        </label>
        <label class="check">
          <input
            type="checkbox"
            checked={$cloneSettings.sampleAllLayers}
            onchange={(event) => updateCloneSettings({ sampleAllLayers: (event.currentTarget as HTMLInputElement).checked })}
          />
          All layers
        </label>
      {/if}
      {#if $activeTool === "blur"}
        <label>
          Strength
          <input
            type="number"
            min="1"
            max="100"
            step="1"
            value={Math.round($smudgeSettings.strength * 100)}
            onchange={(event) => updateSmudgeSettings({ strength: numberValue(event) / 100 })}
          />
          %
        </label>
        {#if $toolModes.blur === "blur"}
          <label>
            Blur radius
            <input
              type="number"
              min="0.5"
              max="50"
              step="0.5"
              value={$smudgeSettings.blurRadius}
              onchange={(event) => updateSmudgeSettings({ blurRadius: numberValue(event) })}
            />
          </label>
        {/if}
      {/if}
    {:else if $activeTool === "move"}
      <span class="hint">
        Drag to move or transform the active layer.
      </span>
    {:else if $activeTool === "eyedropper"}
      <label class="check">
        <input
          type="checkbox"
          checked={$showsSampleRing}
          onchange={(event) => setShowsSampleRing((event.currentTarget as HTMLInputElement).checked)}
        />
        Sample ring
      </label>
      <span class="hint">Click the canvas to sample a color.</span>
    {/if}

    {#if $activeTool === "marquee" || $activeTool === "lasso" || $activeTool === "wand"}
      <label>
        Mode
        <select
          aria-label="Selection combination"
          value={$selectionSettings.combination}
          onchange={(event) => updateSelectionSettings({ combination: selectValue(event) as SelectionCombination })}
        >
          <option value="replace">New</option>
          <option value="add">Add</option>
          <option value="subtract">Subtract</option>
        </select>
      </label>
      {#if $activeTool === "lasso" || $activeTool === "wand" || ($activeTool === "marquee" && $toolModes.marquee === "ellipse")}
        <label class="check">
          <input
            type="checkbox"
            checked={$selectionSettings.antialiased}
            onchange={(event) => updateSelectionSettings({ antialiased: (event.currentTarget as HTMLInputElement).checked })}
          />
          Anti-alias
        </label>
      {/if}
    {/if}

    <div class="palette" aria-label="Foreground and background colors">
      <label title="Foreground color">
        FG
        <input
          aria-label="Foreground color"
          type="color"
          value={`#${rgbToHex($palette.foreground)}`}
          oninput={(event) => setPaletteColor("foreground", (event.currentTarget as HTMLInputElement).value)}
        />
      </label>
      <label title="Background color">
        BG
        <input
          aria-label="Background color"
          type="color"
          value={`#${rgbToHex($palette.background)}`}
          oninput={(event) => setPaletteColor("background", (event.currentTarget as HTMLInputElement).value)}
        />
      </label>
    </div>
  </div>
{/if}

<style>
  .options {
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 30px;
    padding: 4px 10px;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
    color: var(--text-dim);
    font-size: 12px;
    overflow-x: auto;
  }

  label {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    flex: 0 0 auto;
    white-space: nowrap;
  }

  input,
  select {
    min-height: 22px;
    box-sizing: border-box;
    background: var(--bg-panel);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 2px 4px;
    font: inherit;
  }

  input[type="number"] {
    width: 64px;
  }

  input[type="color"] {
    width: 28px;
    height: 22px;
    padding: 0;
  }

  input[type="checkbox"] {
    min-height: auto;
    accent-color: var(--accent);
  }

  .check {
    gap: 5px;
  }

  .palette {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    margin-left: auto;
    padding-left: 8px;
    border-left: 1px solid var(--border);
  }

  .hint {
    color: var(--text-dim);
  }

  :is(input, select):focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
</style>
