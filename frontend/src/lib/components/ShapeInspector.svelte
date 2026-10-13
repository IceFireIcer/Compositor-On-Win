<script lang="ts">
  import { onDestroy } from "svelte";
  import { rgbToHex } from "../state/color";
  import { createShapeInspector, type ShapeCommit, type ShapeTarget } from "../state/shapeStyle";

  let { target = null, commit }: { target?: ShapeTarget | null; commit?: ShapeCommit } = $props();
  // The factory gets the initial value for SSR; the effect below remains the
  // live parent-to-inspector synchronization point.
  // svelte-ignore state_referenced_locally
  const session = createShapeInspector(target);
  const uid = $props.id();
  $effect(() => session.sync(target));
  onDestroy(session.dispose);

  const state = $derived($session);
  const draft = $derived(state.draft);
  const locked = $derived(!commit || state.pending || state.awaitingSnapshot);
  const color = $derived(draft ? `#${rgbToHex({ r: draft.red, g: draft.green, b: draft.blue })}` : "#000000");
</script>

{#if draft}
  <section class="inspector" aria-label="Shape style inspector" aria-busy={state.pending}>
    <header><h2>Shape style</h2><span>{draft.kind}</span></header>
    <form onsubmit={(event) => { event.preventDefault(); void session.save(commit); }}>
      <fieldset disabled={locked}>
        <legend class="sr-only">Shape style</legend>
        <label for="{uid}-color">{draft.kind === "Line" ? "Line color" : "Fill color"}</label>
        <div class="color-fields">
          <input type="color" value={color} aria-label="Choose shape color"
            oninput={(event) => session.update("color", event.currentTarget.value)} />
          <input id="{uid}-color" type="text" value={state.values.color} spellcheck={false}
            aria-invalid={!!state.fields.color} aria-describedby={state.fields.color ? `${uid}-color-error` : undefined}
            oninput={(event) => session.update("color", event.currentTarget.value)} />
        </div>
        {#if state.fields.color}<p id="{uid}-color-error" class="error">{state.fields.color}</p>{/if}

        {#if draft.kind === "Rectangle"}
          <label for="{uid}-radius">Corner radius (px)</label>
          <input id="{uid}-radius" type="number" min="0" max="5000" step="any" value={state.values.cornerRadius}
            aria-invalid={!!state.fields.cornerRadius} aria-describedby="{uid}-radius-help"
            oninput={(event) => session.update("cornerRadius", event.currentTarget.value)} />
          <p id="{uid}-radius-help" class:error={!!state.fields.cornerRadius}>
            {state.fields.cornerRadius ?? "0–5000 px. Large radii form a capsule."}
          </p>
        {:else if draft.kind === "Line"}
          <label for="{uid}-width">Line width (px)</label>
          <input id="{uid}-width" type="number" min="1" max="5000" step="any" value={state.values.lineWidth}
            aria-invalid={!!state.fields.lineWidth} aria-describedby="{uid}-width-help"
            oninput={(event) => session.update("lineWidth", event.currentTarget.value)} />
          <p id="{uid}-width-help" class:error={!!state.fields.lineWidth}>
            {state.fields.lineWidth ?? "1–5000 px. Endpoints stay unchanged."}
          </p>
        {/if}
      </fieldset>

      {#if !commit}<p role="status">Shape style editing is unavailable.</p>
      {:else if state.awaitingSnapshot}<p role="status">Saved. Refreshing shape…</p>{/if}
      {#if state.error}<p class="error" role="alert">{state.error}</p>{/if}
      <div class="actions">
        <button type="button" disabled={locked || (!state.dirty && !Object.keys(state.fields).length)}
          onclick={session.reset}>Reset</button>
        <button class="primary" type="submit" disabled={locked || !state.dirty || !!Object.keys(state.fields).length}>
          {state.pending ? "Saving…" : "Apply"}
        </button>
      </div>
    </form>
  </section>
{/if}

<style>
  .inspector { padding: 12px; color: var(--text); background: var(--bg-panel); font-size: 12px; }
  header, .actions, .color-fields { display: flex; align-items: center; gap: 8px; }
  header { justify-content: space-between; margin-bottom: 12px; }
  h2 { margin: 0; font-size: 13px; font-weight: 600; }
  header span, p { color: var(--text-dim); }
  fieldset { display: grid; gap: 8px; border: 0; padding: 0; margin: 0; min-width: 0; }
  input, button { box-sizing: border-box; min-height: 28px; border: 1px solid var(--border);
    border-radius: 4px; padding: 4px 7px; background: var(--bg-raised); color: var(--text); font: inherit; }
  input { width: 100%; min-width: 0; }
  input[type="color"] { width: 36px; flex: 0 0 36px; padding: 2px; }
  p { margin: 0; line-height: 1.5; }
  form > p { margin-top: 8px; }
  .actions { justify-content: flex-end; margin-top: 12px; }
  button { cursor: pointer; min-width: 64px; }
  .primary { background: var(--accent); color: var(--bg-panel); border-color: var(--accent); }
  .error { color: var(--danger); }
  input[aria-invalid="true"] { border-color: var(--danger); }
  :is(input, button):disabled { cursor: default; opacity: 0.6; }
  :is(input, button):focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  .sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
</style>
