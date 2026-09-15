# Loomwork — Intent

> Draft for review. The authoritative product spec is
> [`docs/loom-work-vision.md`](docs/loom-work-vision.md); delivery order is
> [`docs/ROADMAP.md`](docs/ROADMAP.md). This file is the short "why and what",
> [`SPEC.md`](SPEC.md) is the "how". Where any of these disagree, the vision wins.

## What it is

Loomwork is a **single-user, local-first QA workbench**. It organizes the inputs
and outputs of API testing — documentation sources, requirements, agent
definitions, override rules, generated test suites, and execution reports — for
one service under test, and shows their traceability. It does **not** run tests.

A project is a **directory on disk**, not a database row: `project.json` plus one
subfolder per entity family. State is recovered by scanning the directory.

## Why it exists

QA engineers testing REST services juggle requirements in ADO/Confluence, a
Swagger spec, business rules that contradict that spec, hand-written or
agent-generated tests, and run reports from a separate runner. Nothing ties those
together or answers "which requirements have no executed test". Loomwork is the
control plane that does, with every artifact typed and versioned.

## Principles

- **Non-executing control plane.** Loomwork stores, versions, links, and
  displays. Execution is delegated to an external local or remote runner through
  a defined contract.
- **Local-first, no database.** Filesystem project directories; optional S3 sync
  is the only remote copy. No accounts, no RBAC.
- **Traceability is the core value.** Test case → requirement(s); requirement →
  source of record; run → suite version; test case → the override rule version it
  followed.
- **One interface, many adapters.** LLM/agent access sits behind a single Go
  interface so Ollama, LM Studio, Azure AI Foundry, AWS Bedrock, and the
  Claude/Copilot agent SDKs are swappable without touching callers.
- **Typed entities, not stringly-typed blobs.** QA concepts with their own
  lifecycle (requirements, agent definitions, override rules, test suites) are
  typed and versioned. The generic `Artifact` stays the store for free-form
  material (specs, logs, reports, generated docs).
- **Every version is a discrete snapshot.** Updates append a new version; nothing
  is deleted. No diff view; comparison (later) is structural, not value-level.
- **Deferred work is declared** in this repo with its extension point, never left
  implicit.

## Scope today

| Area | Status |
|---|---|
| Directory-per-project store, atomic writes, cross-process lock | done |
| Document source links (ADO / Confluence / GitHub / other) | done |
| Typed versioned requirements — CLI + browser UI, status, origin, source refs | done |
| Spec-kit **project import** (preview + snapshot, feature selection, read-only requirements) | done |
| Requirement-ID aliasing: local `req-001` ↔ spec `008-FR-001`, both accepted on read | done |
| LLM document analysis + requirement extraction (`origin: extracted`) | done (CLI) |
| Agent definitions + override rules, versioned; browser UI manages both | done |
| Test-suite generation (agent SDK) and import, per-suite versioning | done (CLI + UI import) |
| **Traceability** view: requirement → test links derived on read from suite cases + scenario docs | done |
| **Document suites**: Markdown test folders grouped under Test Suites without moving files | done |
| Testability rollup (`GET /testability`, landing cards, overview) | done (derived) |
| Provider adapters: Ollama, LM Studio, Azure AI Foundry, AWS Bedrock (`Converse`) | done |
| `ImageGenerator` + `im-gen` adapter | done, not CLI-exposed |
| Per-model preset registry with load-time validation | done |
| `cue-note` client: interface + HTTP impl + in-memory stub, cue-driven runs | done |
| Workbench mechanical step: `workbench run` shells out to `api-test-runner`, ingests report | done (CLI runner only) |
| Browser UI over the local API (React/Vite, embedded) | in progress (`re-eval`) |
| Execution contract: local + remote executor, report ingestion, HTML render | planned (phase 4) |
| Run-to-run / env-to-env comparison (pass/fail, latency, structural body delta) | planned (phase 5) |
| Workbench Lambda path, wiki flow, creative playground / sweeps | deferred |

## Non-goals

- Executing tests, hosting or fine-tuning models.
- Reimplementing `api-test-runner`, `im-gen`, or `cue-note`.
- A relational database; multi-tenant concerns (accounts, RBAC, quotas).
- Value-level diffing of responses or requirement versions.

## Required input for a prompt run

A project, a target artifact (id or name), a `provider/model[#preset]` selector,
and provider connection details (base URL for local; endpoint + env-var
credentials for remote).
