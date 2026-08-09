# Project Intent

## Overview
Loomwork is a lightweight, local-first Go orchestrator ("loom") for model-assisted
work over project artifacts. A user creates a **project**, fills it with
**artifacts** (specifications, logs, test results, diagrams, documents), and then
runs **prompts** against those artifacts using **local or remote LLMs**. Every
prompt run produces a new versioned artifact inside the project, so the project
accumulates a traceable chain of inputs, prompts, and generated outputs.

Loomwork is an orchestrator, not a model host and not a domain tool. It owns:
- the project/artifact lifecycle and versioning,
- provider selection and per-model parameter presets,
- the prompt-run pipeline (resolve artifact → build request → call provider → store result),
- integration with sibling services (`api-test-runner`, `im-gen`, `cue-note`).

Everything domain specific (wiki generation, test analysis, creative work) is a
*use case* expressed on top of that foundation, not a new subsystem.

## Full Product Vision

### 1. Projects and artifacts (implemented now — foundation)
A project is a named container with tags and a set of artifacts. An artifact has a
type (`spec`, `log`, `test-result`, `diagram`, `doc`, `generated`), a content body
or an external reference (file path/URL), tags, a version, an optional parent
(lineage), and a "pinned" flag. Pinned artifacts are the durable context a user
wants automatically included in prompt runs; unpinned artifacts are working
material. Prompt runs never mutate an input artifact: they append a new artifact
version whose parent is the input.

### 2. Model providers (implemented now — foundation, core abstraction)
One `provider.TextGenerator` interface with adapters:
- **Ollama** — local HTTP API (`/api/generate`, `/api/chat`, `/api/tags`). *Implemented.*
- **LM Studio** — local OpenAI-compatible HTTP API (`/v1/chat/completions`, `/v1/models`). *Implemented.*
- **Azure AI Foundry** — API-key/credential-based remote inference. *Scaffolded behind the interface; config wiring in place; request/response mapping deferred.*
- **AWS Bedrock** — remote inference via AWS credentials/SigV4. *Scaffolded behind the interface; config wiring in place; request signing deferred.*

One `provider.ImageGenerator` interface with an adapter for the local
[`ilyaus/im-gen`](https://github.com/ilyaus/im-gen) FastAPI service (submit job →
poll status → collect artifacts). *Implemented against the documented im-gen
contract; not yet wired into a CLI command.*

### 3. Per-model presets (implemented now — foundation)
Different models expose different tunable parameters (temperature, top-p, top-k,
context window, max output tokens, repeat penalty, reasoning effort, …). Presets
are stored per `provider + model` in a config-driven registry with validation, so
callers ask for `ollama/qwen3:8b#code-review` rather than hardcoding numbers.

### 4. cue-note integration (implemented now — behind an interface)
[`ilyaus/cue-note`](https://github.com/ilyaus/cue-note) is the system of record for
reusable prompts ("cues") and notes. Loomwork consumes it through a
`cuenote.Client` interface with two implementations: an HTTP client against the
assumed REST contract (documented in [`docs/cue-note-contract.md`](cue-note-contract.md))
and an in-memory stub used for local work and tests, so Loomwork is never blocked
on cue-note's completion.

### 5. Wiki flow (deferred)
Generate and maintain a project wiki from artifacts: chunk large specs/docs, run a
templated prompt chain per chunk, assemble a navigable page tree, and re-generate
incrementally when source artifacts change. Extension point: a `runner`-style
package consuming `model.Project` + `provider.TextGenerator` + presets, emitting
`ArtifactTypeDoc` artifacts. Nothing about the foundation changes for it.

### 6. Testing workbench (deferred)
An interactive surface that drives the existing
[`ilyaus/api-test-runner`](https://github.com/ilyaus/api-test-runner) — its CLI
binary locally and its Lambda handler remotely — plus that repo's
`.specify/extensions/sdd-qa` **generate → run → analyze → refine** loop:
1. **generate** — from an API/CLI contract artifact, produce Markdown scenarios (LLM prompt run → `spec` artifacts).
2. **run** — shell out to `api-test-runner` (or invoke the Lambda) and ingest the JSON/CSV report as a `test-result` artifact.
3. **analyze** — prompt-run over the report to classify failures (harness defect vs. genuine contract drift) into a `doc` artifact.
4. **refine** — regenerate only harness-defective scenarios and re-run, keeping lineage via artifact parents.
Extension points: an `internal/exec`-style process runner for the CLI, an
`internal/ingest` mapper for report → artifact, and preset entries for the
analysis models. Deferred entirely in this session; no code assumes its shape
beyond the artifact types already defined.

### 7. Creative playground (deferred)
Free-form multi-modal experimentation: prompt sweeps across models/presets,
side-by-side comparison of outputs, seed sweeps against `im-gen`, and promotion of
good results into pinned artifacts. Extension points: the existing
`provider.ImageGenerator` adapter, the preset registry (sweep = iterate presets),
and `cue-note` for storing the winning prompts.

### 8. Interfaces (partially implemented)
A CLI (`cmd/loomwork`) is the foundation's entry point. An HTTP server and a
richer UI are deferred; the orchestration package is deliberately transport
agnostic so a server can be added without touching domain or provider code.

## Implemented vs. Deferred — summary

| Area | Status |
|---|---|
| Project & artifact domain model, versioning, pinning, lineage | **Implemented** |
| JSON file-backed project store | **Implemented** |
| `TextGenerator` interface + Ollama adapter | **Implemented** |
| `TextGenerator` + LM Studio (OpenAI-compatible) adapter | **Implemented** |
| Azure AI Foundry adapter | Scaffolded (config + interface only) |
| AWS Bedrock adapter | Scaffolded (config + interface only) |
| `ImageGenerator` interface + `im-gen` HTTP adapter | **Implemented** (not CLI-exposed) |
| Per-model preset registry + validation | **Implemented** |
| `cue-note` client interface + HTTP impl + in-memory stub | **Implemented** (contract assumed) |
| CLI vertical slice: project → artifact → prompt run → new artifact | **Implemented** |
| Wiki generation flow | Deferred |
| Testing workbench / `api-test-runner` + `sdd-qa` loop | Deferred |
| Creative playground / sweeps / comparisons | Deferred |
| HTTP server, auth, multi-user, remote persistence | Deferred |

## Required Input
To run a prompt the user must supply:
- a project (created by Loomwork, persisted under a workspace directory),
- an artifact reference (id or name) to use as context,
- a provider + model selector, optionally with a preset name,
- provider connection details: base URL for local providers; endpoint plus
  credentials via environment variables for remote providers.

## Non-Goals
- Hosting or fine-tuning models.
- Reimplementing `api-test-runner`, `im-gen`, or `cue-note` functionality.
- Multi-tenant service concerns (accounts, RBAC, quotas).
