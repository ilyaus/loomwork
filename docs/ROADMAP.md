# Roadmap

[`docs/INTENT.md`](INTENT.md) describes *what* Loomwork is meant to become and
marks each area implemented or deferred. This document sequences the deferred
areas into independently shippable units of work, in the order that maximizes
what the next unit can build on.

Each item is scoped to one session, is mergeable on its own, and states the
extension points it uses so it does not reshape the foundation. An item that
requires changing a foundation interface says so explicitly — that is the signal
to review the change carefully rather than absorb it.

| # | Item | Depends on | Foundation change |
|---|---|---|---|
| 1 | cue-note wiring | — | additive only |
| 2 | Testing workbench | 1 (optional) | two new packages |
| 3 | Wiki flow | 1 (optional) | none |
| 4 | Creative playground | — | none |
| 5 | Azure / Bedrock adapters | — | none |
| 6 | HTTP surface (`cmd/server`) | — | none |

---

## 1. cue-note wiring — *done*

**Why first.** It is the smallest item, the client already exists, and every
later item wants reusable prompts rather than prompt strings pasted into shell
history.

**Scope.** Expose the existing `cuenote.Client` through the CLI: list and show
cues, and resolve a cue into the prompt for a run.

```text
loomwork cue list [--tag a,b] [--search TEXT] [--limit N]
loomwork cue show --cue REF
loomwork run --cue REF [--var key=value ...]   # instead of --prompt/--prompt-file
```

`REF` is a cue id or an exact (case-insensitive) name; an ambiguous name is an
error rather than a silent pick. `{{var}}` placeholders are rendered from
`--var`, and an unresolved variable fails the run before any provider call.
Generated artifacts record `cue` and `cueId` in their metadata.

**Extension points.** `cuenote.Client`, `Cue.Render`, and a new additive
`RunRequest.Metadata` for provenance that the orchestrator merges into the
generated artifact. No provider, model, or store change.

**Done when.** The three commands work against a stub cue-note server; a run
driven by a cue is indistinguishable from one driven by `--prompt` except for the
extra metadata; unresolved variables and unreachable cue-note produce actionable
errors.

## 2. Testing workbench — *done*

**Scope.** Drive [`ilyaus/api-test-runner`](https://github.com/ilyaus/api-test-runner)
and its `.specify/extensions/sdd-qa` generate→run→analyze→refine loop as chained
prompt runs:

1. **generate** — prompt-run over an API contract artifact producing Markdown
   scenarios as `spec` artifacts.
2. **run** — execute the `api-test-runner` CLI over those scenarios.
3. **analyze** — prompt-run over the ingested report classifying each failure as
   harness defect vs. contract drift, producing a `doc` artifact.
4. **refine** — regenerate only harness-defective scenarios, re-run, keep lineage
   through artifact parents.

**New packages.** `internal/exec` (a context-aware process runner: argv, working
directory, environment allowlist, timeout, captured stdout/stderr — no shell) and
`internal/ingest` (map an `api-test-runner` JSON/CSV report to a `test-result`
artifact plus a normalized summary).

**Resolutions.** The binary comes from `--runner`, then the `workbench.runnerPath`
config key, then an `api-test-runner` PATH lookup — never built from a checkout.
The Lambda path is out of scope; CLI only. The loop is composable: **generate**,
**analyze**, and **refine** are ordinary `run` invocations (optionally cue-driven),
while the mechanical step is a dedicated command:

```text
loomwork workbench run --project REF --scenarios ART[,ART...] --base-url URL \
    [--runner PATH] [--auth-config PATH | --token-provider-config PATH] \
    [--dry-run] [--arg VALUE ...] [--timeout SECONDS] [--name NAME] [--tags a,b]
```

Scenario artifacts are materialized into a temp directory, the runner executes
with an allowlisted environment (`workbench.env`) and a bounded timeout, and its
stdout JSON report is ingested as a `test-result` artifact whose parent is the
first scenario artifact, with `outcome`/`total`/`passed`/`failed`/`skipped`/
`exitCode`/`scenarios` metadata.

**Done when.** A contract artifact can go through all four steps against a stub
`api-test-runner`, with every intermediate artifact persisted and lineage intact.

## 3. Wiki flow

**Scope.** Generate a navigable wiki from a project's artifacts: chunk large
specs/docs, run a templated prompt chain per chunk, assemble a page tree, and
regenerate only pages whose source artifacts changed.

**Extension points.** A `runner`-style package over `model.Project`,
`provider.TextGenerator`, and the preset registry, emitting `doc` artifacts.
Incremental regeneration keys off the source artifact id + version already
recorded in generated metadata, so no new bookkeeping is required.

**Open question.** Chunking belongs in a shared package if the workbench also
needs it for large reports — item 2 landed without one (reports are stored
verbatim), so chunking can start as a wiki-local concern.

## 4. Creative playground

**Scope.** Preset sweeps (same prompt across models/presets, outputs compared
side by side) and image generation wired into the CLI through the existing
`provider.ImageGenerator`/`im-gen` adapter, which today has no command. Promote a
winning output by pinning it.

**Extension points.** The preset registry (a sweep is an iteration over preset
names), the image adapter, and cue-note for storing winning prompts.

**Note.** Sweeps are the first feature that issues concurrent provider calls;
bound the concurrency and keep the per-run persistence path unchanged (each
result is an ordinary derived artifact, written through `store.Update`).

## 5. Azure AI Foundry and AWS Bedrock adapters

**Scope.** Replace `provider.ErrNotImplemented` in `Generate` with real request
and response mapping. Constructors, configuration, and credential resolution
already exist and stay as they are.

**Notes.** Bedrock needs SigV4; implementing it from the standard library is the
CGO-free choice but is the bulk of the work, so Azure should land first. Keep the
"drop unsupported parameters rather than invent a mapping" rule — that rule is
what keeps the normalized `Params` honest.

**Done when.** Both adapters generate against their real endpoints, and
`providers` reports them as configured rather than scaffolded.

## 6. HTTP surface

**Scope.** A `cmd/server` sibling exposing the same operations over HTTP, reusing
`internal/orchestrator` unchanged — the orchestrator is already transport
agnostic, so this is a handler layer plus request/response types.

**Caveat.** The store's cross-process lock is directory-wide and its readers
intentionally bypass it. That is right for a CLI, but a long-lived multi-client
server should revisit it (per-project locking, or a different store
implementation behind the existing `ProjectStore` interface) before it becomes a
throughput or consistency problem.

---

## Not planned

Model hosting or fine-tuning, reimplementing `api-test-runner`/`im-gen`/`cue-note`
functionality, and multi-tenant concerns (accounts, RBAC, quotas) remain
non-goals, as stated in [`docs/INTENT.md`](INTENT.md).
