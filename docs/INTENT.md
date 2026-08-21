# Project Intent

> **Authoritative product spec:** [`docs/loom-work-vision.md`](loom-work-vision.md).
> Where this document and the vision disagree, the vision wins and this document
> is corrected. This file records how the vision maps onto what exists in the
> codebase today; [`docs/ROADMAP.md`](ROADMAP.md) records the order of delivery.

## Overview
Loomwork is a single-user, local-first **QA workbench**: it organizes
documentation sources, requirements, agent definitions and override rules,
generated test suites, and execution reports for a service under test — without
executing the tests itself. Execution is delegated to a local executable or a
remote service through a defined contract, and reports are ingested back.

A project is a **directory on disk**, not a database record. It holds
`project.json` (name, description, document source links, cached index) plus one
subfolder per entity family: `requirements/`, `agent-definitions/`,
`test-suites/`, `executor-config/`, `reports/`.

Loomwork is a non-executing control plane with first-class QA domain entities. It
owns:
- the project directory lifecycle and document source links,
- typed, versioned QA entities — requirements today; agent definitions, override
  rules, test suites, executor configs, and reports in later phases,
- traceability: a test case links to requirements, a requirement links to its
  source of record (ADO/Confluence/GitHub), a run links to a suite version,
- provider and agent access behind one internal interface so Ollama, LM Studio,
  Azure Foundry, Bedrock, and the Claude/Copilot agent SDKs are interchangeable,
- the artifact/prompt-run pipeline that the above is built on (resolve artifact →
  build request → call provider → store result),
- integration with sibling services (`api-test-runner`, `im-gen`, `cue-note`).

The generic `Artifact` remains the store for free-form material (specs, logs,
reports, generated documents). QA concepts that carry their own lifecycle are
**typed entities**, not artifacts — `model.Requirement` is the first of them.

## Full Product Vision

### 1. Projects and artifacts (implemented now — foundation)
A project is a named container with tags and a set of artifacts. An artifact has a
type (`spec`, `log`, `test-result`, `diagram`, `doc`, `generated`), a content body
or an external reference (file path/URL), tags, a version, an optional parent
(lineage), and a "pinned" flag. Pinned artifacts are the durable context a user
wants automatically included in prompt runs; unpinned artifacts are working
material. Prompt runs never mutate an input artifact: they append a new artifact
version whose parent is the input.

### 1b. Document sources and requirements (implemented now — phase 1)
A project records its **document sources** as links into their system of record
(GitHub, Confluence, Azure ADO, other) with an optional local path or S3 URI for
a copy of the exported document.

**Requirements** are a typed entity (`model.Requirement`), not an artifact. Each
requirement has a stable id, tester-friendly text, an optional `source_type` /
`source_ref` back-reference, a status (`active`, `obsolete`, `superseded`), and an
origin (`authored` by a QA engineer or `extracted` by document analysis — both
write the same schema to the same store). Versions are discrete retrievable
snapshots: an update writes `req-001.v2.json` and marks v1 superseded, and
nothing is ever deleted, so obsolete requirements stay auditable. Current-version
pointers live in `requirements/index.json`; the wire format is fixed by
[`docs/schemas/requirement.schema.json`](schemas/requirement.schema.json). No LLM
is involved in this phase.

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
on cue-note's completion. The CLI exposes cues through `cue list`, `cue show`, and
`run --cue REF [--var key=value]`, which renders a cue's `{{var}}` template into the
prompt and records `cue`/`cueId` provenance on the generated artifact.

### 5. Wiki flow (deferred)
Generate and maintain a project wiki from artifacts: chunk large specs/docs, run a
templated prompt chain per chunk, assemble a navigable page tree, and re-generate
incrementally when source artifacts change. Extension point: a `runner`-style
package consuming `model.Project` + `provider.TextGenerator` + presets, emitting
`ArtifactTypeDoc` artifacts. Nothing about the foundation changes for it.

### 6. Testing workbench (CLI loop implemented; Lambda deferred)
An interactive surface that drives the existing
[`ilyaus/api-test-runner`](https://github.com/ilyaus/api-test-runner) — its CLI
binary locally and its Lambda handler remotely — plus that repo's
`.specify/extensions/sdd-qa` **generate → run → analyze → refine** loop:
1. **generate** — from an API/CLI contract artifact, produce Markdown scenarios (LLM prompt run → `spec` artifacts).
2. **run** — shell out to `api-test-runner` (or invoke the Lambda) and ingest the JSON/CSV report as a `test-result` artifact.
3. **analyze** — prompt-run over the report to classify failures (harness defect vs. genuine contract drift) into a `doc` artifact.
4. **refine** — regenerate only harness-defective scenarios and re-run, keeping lineage via artifact parents.
The mechanical step is implemented as `workbench run`: `internal/exec` runs the
CLI binary (argv-only, environment allowlist, bounded timeout — no shell) and
`internal/ingest` maps its stdout JSON report to a `test-result` artifact whose
parent is the scenario artifact. The generate/analyze/refine steps are ordinary
(optionally cue-driven) `run` invocations. The Lambda path remains deferred.

### 7. Creative playground (deferred)
Free-form multi-modal experimentation: prompt sweeps across models/presets,
side-by-side comparison of outputs, seed sweeps against `im-gen`, and promotion of
good results into pinned artifacts. Extension points: the existing
`provider.ImageGenerator` adapter, the preset registry (sweep = iterate presets),
and `cue-note` for storing the winning prompts.

### 8. Interfaces (partially implemented)
A CLI (`cmd/loomwork`) is the entry point today. The end state is a browser UI
over a local backend API. An earlier `serve`/`initial_ui` HTTP+UI attempt has
been **discarded and removed**; the browser UI will be rebuilt fresh against the
typed domain entities once they exist. The orchestration package stays transport
agnostic so that rebuild touches neither domain nor provider code.

## Implemented vs. Deferred — summary

| Area | Status |
|---|---|
| Project & artifact domain model, versioning, pinning, lineage | **Implemented** |
| Directory-per-project store (`project.json` + entity subfolders) | **Implemented** |
| Project document source links (GitHub/Confluence/ADO + local/S3 copies) | **Implemented** |
| Typed `Requirement` entity: versioning, status, source refs, CLI CRUD | **Implemented** |
| LLM document analysis, requirement extraction (phase 2) | Deferred |
| Agent definitions, override rules, `AgentAdapter`, test generation (phase 3) | Deferred |
| Execution contract, report ingestion, HTML rendering (phase 4) | Deferred |
| Run comparison and testability dashboard (phase 5) | Deferred |
| `TextGenerator` interface + Ollama adapter | **Implemented** |
| `TextGenerator` + LM Studio (OpenAI-compatible) adapter | **Implemented** |
| Azure AI Foundry adapter | Scaffolded (config + interface only) |
| AWS Bedrock adapter | Scaffolded (config + interface only) |
| `ImageGenerator` interface + `im-gen` HTTP adapter | **Implemented** (not CLI-exposed) |
| Per-model preset registry + validation | **Implemented** |
| `cue-note` client interface + HTTP impl + in-memory stub | **Implemented** (contract assumed) |
| CLI cue listing/inspection and cue-driven prompt runs | **Implemented** |
| CLI vertical slice: project → artifact → prompt run → new artifact | **Implemented** |
| Wiki generation flow | Deferred |
| Testing workbench: `workbench run` + report ingestion | **Implemented** (CLI runner only) |
| Testing workbench Lambda path | Deferred |
| Creative playground / sweeps / comparisons | Deferred |
| Browser UI over a local backend API | Deferred (previous attempt discarded) |
| Auth, multi-user, remote persistence | Deferred |

## Required Input
To run a prompt the user must supply:
- a project (created by Loomwork, persisted under a workspace directory),
- an artifact reference (id or name) to use as context,
- a provider + model selector, optionally with a preset name,
- provider connection details: base URL for local providers; endpoint plus
  credentials via environment variables for remote providers.

## Non-Goals
- **Executing tests.** Loomwork stores, organizes, versions, and displays; a
  local executable or a remote service runs the tests and returns reports.
- Hosting or fine-tuning models.
- Reimplementing `api-test-runner`, `im-gen`, or `cue-note` functionality.
- Multi-tenant service concerns (accounts, RBAC, quotas).
- A relational database. Project state lives in the project directory; an
  optional S3 sync is the only remote copy.
- Value-level diffing of responses or requirement versions. Comparison is
  structural (fields present/absent) and versions are discrete snapshots.
