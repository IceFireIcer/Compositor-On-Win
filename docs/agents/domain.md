# Domain Docs

How the engineering skills should consume this repo's domain documentation when exploring the codebase.

## Before exploring, read these

- **`GLOSSARY.md`** at the repo root, or
- **`GLOSSARY-MAP.md`** at the repo root if it exists: it points at one `GLOSSARY.md` per context. Read each one relevant to the topic.
- **`docs/adr/`**: read ADRs that touch the area you're about to work in. In multi-context repos, also check `src/<context>/docs/adr/` for context-scoped decisions.

This repo also carries domain documentation that predates the glossary and is **authoritative today** — read the relevant one before working in its area:

- `docs/spec.md` — specification and user stories; the definition of "feature parity".
- `docs/feature-parity.md` — the single authoritative feature list; check the milestone column before splitting work and the status column at acceptance.
- `docs/architecture.md` — target architecture, Swift→Go/TS module mapping, IPC contract.
- `reference/Swift/docs/project-format.md` — authoritative `.comp` format spec (v1–v11).

If any of these files don't exist, **proceed silently**. Don't flag their absence; don't suggest creating them upfront. The `/domain-modeling` skill (reached via `/grill-with-docs` and `/improve-codebase-architecture`) creates them lazily when terms or decisions actually get resolved.

## File structure

**Single-context repo.** One `GLOSSARY.md` and one `docs/adr/` at the root:

```
/
├── GLOSSARY.md
├── docs/
│   ├── adr/
│   │   ├── 0001-use-wails-svelte-go.md
│   │   └── 0002-comp-format-compatibility.md
│   └── ...
├── internal/          ← Go packages
└── frontend/          ← Svelte 5 + WebGPU
```

Neither `GLOSSARY.md` nor `GLOSSARY-MAP.md` exists yet; ADRs are written as `docs/adr/NNNN-slug.md`.

## Use the glossary's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in `GLOSSARY.md`. Don't drift to synonyms the glossary explicitly avoids.

If the concept you need isn't in the glossary yet, that's a signal: either you're inventing language the project doesn't use (reconsider) or there's a real gap (note it for `/domain-modeling`).

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0007 (event-sourced orders), but worth reopening because…_

This repo's ADRs are binding in a strong sense: `.comp` v11 compatibility (ADR-0002) and the sRGB-space special blend modes are **hard invariants**, not preferences. A proposal that breaks either needs an explicit ADR change and user confirmation, not a silent override.
