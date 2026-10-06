<script lang="ts">
  /**
   * The Camera Raw slider row (CameraRawSlider.swift): label (double-click
   * resets), gradient track, numeric field. Option-drag on tone sliders
   * switches to the clipping view via the onalt callback; releasing Alt
   * clears it (the panel installs the keyup listener).
   */

  let {
    label,
    value,
    min,
    max,
    step = 1,
    decimals = 0,
    reset = 0,
    track,
    help = "",
    oninput,
    onreset,
    onalt,
  }: {
    label: string;
    value: number;
    min: number;
    max: number;
    step?: number;
    decimals?: number;
    reset?: number;
    /** Track gradient colors, left → right (nil keeps the system track). */
    track?: string[];
    help?: string;
    oninput: (value: number) => void;
    /** Defaults to writing the reset value back through oninput. */
    onreset?: () => void;
    /** Present on the tone sliders whose Option-drag shows the clip view. */
    onalt?: (value: number, clipping: 0 | 1 | 2) => void;
  } = $props();

  function changed(event: Event & { currentTarget: HTMLInputElement }): void {
    const raw = +event.currentTarget.value;
    const snapped = Math.round(raw * 10 ** decimals) / 10 ** decimals;
    if (onalt) {
      const alt = typeof InputEvent !== "undefined" && event instanceof InputEvent
        ? Boolean((event as unknown as { altKey?: boolean }).altKey)
        : false;
      onalt(snapped, alt ? 1 : 0);
    }
    oninput(snapped);
  }

  function resetValue(): void {
    if (onreset) onreset();
    else oninput(reset);
  }

  const gradient = $derived(
    track && track.length >= 2
      ? `background: linear-gradient(90deg, ${track.join(", ")})`
      : "",
  );
</script>

<label class="cr-slider" title={help}>
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <span class="cr-label" ondblclick={resetValue}>{label}</span>
  <input
    class="cr-track"
    class:gradient={!!track}
    style={gradient}
    type="range"
    {min}
    {max}
    {step}
    {value}
    oninput={changed}
    ondblclick={resetValue}
    aria-label={label}
  />
  <input
    class="cr-number"
    type="number"
    {min}
    {max}
    {step}
    {value}
    onchange={(e) => oninput(+e.currentTarget.value)}
    aria-label={label}
  />
</label>

<style>
  .cr-slider {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
  }

  .cr-label {
    min-width: 76px;
    color: var(--text-dim);
    cursor: default;
  }

  .cr-track {
    flex: 1;
    height: 14px;
  }

  .cr-track.gradient {
    appearance: none;
    height: 8px;
    border-radius: 4px;
    border: 1px solid var(--border);
  }

  .cr-track.gradient::-webkit-slider-thumb {
    appearance: none;
    width: 12px;
    height: 16px;
    border-radius: 3px;
    background: var(--text);
    border: 1px solid var(--bg-panel);
    cursor: ew-resize;
  }

  .cr-number {
    width: 60px;
  }
</style>
