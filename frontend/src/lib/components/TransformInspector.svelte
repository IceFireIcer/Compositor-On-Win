<script lang="ts">
  import {
    applyTransformNumeric,
    cancelTransformSession,
    commitTransformSession,
    flipTransform,
    transformCommitPending,
    transformDraft,
    transformError,
    transformSession,
  } from "../state/transform";
  import { SAMPLINGS, type NumericInput, type Sampling } from "../state/transformSession";

  type NumericKey = "x" | "y" | "w" | "h" | "rotation";

  const draft = $derived($transformDraft);
  const pending = $derived($transformCommitPending);
  const error = $derived($transformError);

  function numericValue(key: NumericKey): number | undefined {
    if (!draft) return undefined;
    switch (key) {
      case "x": return draft.origin[0];
      case "y": return draft.origin[1];
      case "w": return draft.size[0];
      case "h": return draft.size[1];
      case "rotation": return draft.rotation;
    }
  }

  function applyNumericField(key: NumericKey, event: Event): boolean {
    if (!(event.currentTarget instanceof HTMLInputElement)) return false;
    const raw = event.currentTarget.value.trim();
    if (raw === "") return false;
    const value = Number(raw);
    if (!Number.isFinite(value)) return false;
    const input: NumericInput = { [key]: value } as NumericInput;
    return applyTransformNumeric(input);
  }

  function setSampling(event: Event): void {
    const value = (event.currentTarget as HTMLSelectElement).value as Sampling;
    if (SAMPLINGS.includes(value)) applyTransformNumeric({ sampling: value });
  }

  function onKeydown(event: KeyboardEvent, numericKey?: NumericKey): void {
    if (event.key === "Escape" && !pending) {
      event.preventDefault();
      cancelTransformSession();
    } else if (
      event.key === "Enter" &&
      !pending &&
      numericKey !== undefined &&
      event.currentTarget instanceof HTMLInputElement
    ) {
      event.preventDefault();
      // `change` fires after Enter in some browsers. Apply the visible field
      // first so the request cannot serialize the previous number.
      if (applyNumericField(numericKey, event)) void apply();
    }
  }

  async function apply(): Promise<void> {
    if (pending) return;
    await commitTransformSession();
  }
</script>

{#if $transformSession && draft}
  <section
    class="inspector"
    aria-label="Transform inspector"
  >
    <div class="header">
      <h2>Transform</h2>
      <span class="hint">Non-destructive</span>
    </div>

    <div class="fields">
      <label for="transform-x">X</label>
      <input
        id="transform-x"
        aria-label="X"
        type="number"
        step="any"
        min="-30000"
        max="30000"
        value={numericValue("x")}
        disabled={pending}
        onchange={(event) => applyNumericField("x", event)}
        onkeydown={(event) => onKeydown(event, "x")}
      />

      <label for="transform-y">Y</label>
      <input
        id="transform-y"
        aria-label="Y"
        type="number"
        step="any"
        min="-30000"
        max="30000"
        value={numericValue("y")}
        disabled={pending}
        onchange={(event) => applyNumericField("y", event)}
        onkeydown={(event) => onKeydown(event, "y")}
      />

      <label for="transform-width">W</label>
      <input
        id="transform-width"
        aria-label="W"
        type="number"
        step="any"
        min="1"
        max="30000"
        value={numericValue("w")}
        disabled={pending}
        onchange={(event) => applyNumericField("w", event)}
        onkeydown={(event) => onKeydown(event, "w")}
      />

      <label for="transform-height">H</label>
      <input
        id="transform-height"
        aria-label="H"
        type="number"
        step="any"
        min="1"
        max="30000"
        value={numericValue("h")}
        disabled={pending}
        onchange={(event) => applyNumericField("h", event)}
        onkeydown={(event) => onKeydown(event, "h")}
      />

      <label for="transform-rotation">Rotation</label>
      <input
        id="transform-rotation"
        aria-label="Rotation"
        type="number"
        step="any"
        min="-360"
        max="360"
        value={numericValue("rotation")}
        disabled={pending}
        onchange={(event) => applyNumericField("rotation", event)}
        onkeydown={(event) => onKeydown(event, "rotation")}
      />

      <label for="transform-sampling">Sampling</label>
      <select
        id="transform-sampling"
        aria-label="Sampling"
        value={draft.sampling}
        disabled={pending}
        onchange={setSampling}
        onkeydown={onKeydown}
      >
        {#each SAMPLINGS as sampling}
          <option value={sampling}>{sampling}</option>
        {/each}
      </select>
    </div>

    <div class="flips" aria-label="Flip controls">
      <button
        type="button"
        aria-label="Horizontal flip"
        aria-pressed={draft.flipX}
        disabled={pending}
        onclick={() => flipTransform("x")}
        onkeydown={onKeydown}
      >
        Flip H
      </button>
      <button
        type="button"
        aria-label="Vertical flip"
        aria-pressed={draft.flipY}
        disabled={pending}
        onclick={() => flipTransform("y")}
        onkeydown={onKeydown}
      >
        Flip V
      </button>
    </div>

    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}

    <div class="actions">
      <button type="button" aria-label="Cancel transform" onclick={cancelTransformSession} onkeydown={onKeydown}>
        Cancel
      </button>
      <button
        type="button"
        class="primary"
        aria-label="Apply transform"
        disabled={pending}
        onclick={() => void apply()}
        onkeydown={onKeydown}
      >
        {pending ? "Applying..." : "Apply"}
      </button>
    </div>
  </section>
{/if}

<style>
  .inspector {
    display: grid;
    gap: 14px;
    box-sizing: border-box;
    width: 100%;
    min-width: 0;
    padding: 14px 12px 16px;
    background: var(--bg-panel);
    color: var(--text);
    border-bottom: 1px solid var(--border);
  }

  .header,
  .actions,
  .flips {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .header h2 {
    margin: 0;
    color: var(--text-strong);
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.02em;
  }

  .hint {
    margin-left: auto;
    color: var(--text-dim);
    font-size: 11px;
  }

  .fields {
    display: grid;
    grid-template-columns: 68px minmax(0, 1fr);
    align-items: center;
    gap: 8px;
  }

  label {
    color: var(--text-dim);
    font-size: 12px;
  }

  input,
  select,
  button {
    min-height: 30px;
    box-sizing: border-box;
    background: var(--bg-raised);
    color: var(--text);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 4px 7px;
    font: inherit;
  }

  input,
  select {
    width: 100%;
  }

  button {
    cursor: pointer;
  }

  button[aria-pressed="true"] {
    border-color: var(--accent);
    color: var(--accent);
  }

  .flips button {
    flex: 1;
  }

  .actions {
    justify-content: flex-end;
    padding-top: 12px;
    border-top: 1px solid var(--border);
  }

  .actions button {
    min-width: 72px;
  }

  .primary {
    background: var(--accent-action);
    color: #fff;
    border-color: var(--accent-action);
  }

  .primary:hover:not(:disabled) {
    background: var(--accent-hover);
  }

  .error {
    margin: 0;
    color: var(--danger);
    font-size: 12px;
  }

  :is(input, select, button):disabled {
    cursor: default;
    opacity: 0.6;
  }

  :is(input, select, button):focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
</style>
