# Roadmap

[`docs/loom-work-vision.md`](loom-work-vision.md) is the authoritative product
spec; its phased build plan is the roadmap. This document restates those phases
with the concrete Loomwork packages, commands, and schemas each one touches, and
records what already exists.

Every phase ships with a **fixed JSON schema for its core artifact, drafted and
reviewed before implementation** — those schemas are the contract between store,
browser UI, and any agent or executor integration. They live in
[`docs/schemas/`](schemas/).

| Phase | Scope | Core schema | Status |
|---|---|---|---|
| 1 | Project directory management, document links, requirement CRUD + versioning | [`requirement.schema.json`](schemas/requirement.schema.json) | **done (CLI + browser UI)** |
| 2 | LLM document analysis: gap/question lists, requirement extraction | [`document-analysis.schema.json`](schemas/document-analysis.schema.json) | **done (CLI)** |
| 3 | Agent definitions, override rules, one agent SDK integration, test generation | `agent-definition.schema.json`, `test-case.schema.json` | planned |
| 4 | Execution contract (local + remote executor), JSON report ingestion, HTML rendering | `execution-report.schema.json`, `executor-config.schema.json` | planned |
| 5 | Run comparison (pass/fail, latency, structural body delta) and testability dashboard | — (derived views) | planned |

The **browser UI** is the intended primary surface and is built incrementally on
top of these phases. The earlier `serve`/`initial_ui` HTTP+UI attempt was
**discarded and removed from the repository**; the UI was rebuilt fresh over the
typed domain entities rather than continued. `internal/httpapi` is a handler
layer over the unchanged `store.DirStore`, its assets are embedded from `web/`,
and `loomwork serve` binds the loopback interface only. The CLI remains the
reference surface, and `internal/orchestrator` stays transport agnostic.

---

## Phase 1 — project shell and requirements (no LLM calls) — *done*

**Deliverable.** A working project shell: projects as directories, document
source links, and typed requirement CRUD with versioning.

**Store.** `store.DirStore` lays each project out per the vision's data model:

```text
<projects-root>/<project-id>/
  project.json                # name, description, tags, doc source links, index cache
  requirements/
    req-001.v1.json           # discrete retrievable snapshot
    req-001.v2.json           # v1 retained, marked superseded
    index.json                # current-version pointer per requirement id
  agent-definitions/          # phase 3
  test-suites/                # phase 3
  executor-config/            # phase 4
  reports/                    # phase 4
```

Writes are atomic (temp file + rename) and read-modify-write cycles hold a
directory lock, since every CLI invocation is its own process. Projects written
by the earlier flat `<project-id>.json` layout are still readable and migrate to
a directory on first write.

**Domain.** `model.Requirement` is a typed entity — deliberately not a
`model.Artifact` — with id, version, tester-friendly text, `source_type` /
`source_ref`, status (`active`/`obsolete`/`superseded`), origin
(`authored`/`extracted`), tags, and metadata. `model.DocumentSource` carries a
project's links (GitHub/Confluence/ADO/other) plus optional local or S3 copies.
Obsolete requirements are retained, never deleted; `superseded` is set only by
writing a newer version, and such a version's status is then frozen.

**CLI.**

```text
loomwork project create --name NAME [--source "name=NAME,type=github,url=URL[,local=PATH][,s3=URI]" ...]
loomwork project source --project REF --source "name=NAME,type=ado,url=URL" ...
loomwork requirement create     --project REF (--text TEXT | --text-file PATH) \
                                [--source-type TYPE] [--source-ref REF] [--status STATUS] \
                                [--origin authored|extracted] [--tags a,b]
loomwork requirement list       --project REF [--status STATUS]
loomwork requirement show       --project REF --requirement ID [--version N | --history]
loomwork requirement update     --project REF --requirement ID [--text TEXT | --text-file PATH]
loomwork requirement set-status --project REF --requirement ID --status STATUS [--version N]
```

**Browser UI.** `loomwork serve` (`internal/httpapi` + embedded `web/` assets)
serves the directory-of-projects landing view, a project view with its document
sources, and requirement management with version history over the same JSON
requirement schema. Testability figures (last-tested date, coverage, open gaps)
are placeholders until phases 4-5.

**Remaining in this phase.** Reading a project by pointing at an existing
directory outside the workspace root.

## Phase 2 — LLM-driven document analysis — *done for the CLI*

**Scope.** Analyze a project's document sources with a single provider first,
producing a QA-readiness assessment plus a list of gaps and open questions, and
extracting requirements into the phase-1 store. Extraction writes the same
`Requirement` schema with `origin: extracted` and provenance in `metadata`, so no
reader changes.

**Also required.** A manual-import path, because QA engineers often perform this
analysis outside Loomwork.

**Extension points.** `provider.TextGenerator`, the preset registry, and
`store.DirStore.CreateRequirement`. Draft `document-analysis.schema.json` first.

**Done.** `internal/analysis` analyzes a project's document sources through any
configured `provider.TextGenerator`, stores the analysis (fixed by
[`document-analysis.schema.json`](schemas/document-analysis.schema.json)) as a
versioned `doc` artifact, and writes extracted requirements through the phase-1
store with `origin: extracted` and provider/model provenance in `metadata`.

```text
loomwork analysis run    --project REF --model provider/model[#preset] \
                         [--system TEXT] [--name NAME] [--tags a,b] [--no-extract] \
                         [--temperature N] [--top-p N] [--max-tokens N] [--seed N]
loomwork analysis import --project REF --file PATH [--name NAME] [--tags a,b] [--no-extract]
```

**Remaining in this phase.** Analysis views in the browser UI.

## Phase 3 — agent definitions, override rules, and test generation

**Scope.** Versioned agent-definition Markdown files (`<name>.v3.md` plus a
`current.json` pointer in `agent-definitions/`), the override-rule schema, and
one agent SDK integration (Claude Agent SDK first) generating REST API test
suites into versioned `test-suites/`.

**Interface change.** This phase introduces the stateful `AgentAdapter`
(sessions, tool registration, event streams) **alongside** the existing
`provider.TextGenerator`, which stays as-is for single-shot generation.

**Highest-risk area.** Override rules must let the agent *reason* about business
rules that take precedence over the literal OpenAPI spec. Every generated test
case records `overrides_applied[]` and the requirement ids it covers, so
misapplication is auditable without reading reasoning transcripts.

## Phase 4 — execution contract and report ingestion

**Scope.** Loomwork never executes tests. It hands a suite plus a versioned
executor configuration to a local executable, or uploads to S3 and polls a remote
run API, then ingests the returned JSON report. Reports are append-only, keyed by
`(suite_version, run_timestamp)` under `reports/`, and rendered as HTML.

**Existing groundwork.** `internal/exec` (argv-only process runner with an
environment allowlist and bounded timeout) and `internal/ingest` (report → typed
result mapping) already exist from the `workbench run` command and are the
starting point for the local executor path.

## Phase 5 — comparison and testability dashboard

**Scope.** Run-to-run and environment-to-environment comparison: pass/fail
deltas, latency deltas, and **structural** response-body comparison (fields
missing or unexpected — presence only, never value diffing). Plus the derived
testability view per project (last-tested outcome, requirements covered,
requirements with no executed test) rolled up into a directory-of-projects
landing view.

---

## Deferred, outside the vision's phases

Wiki generation and the creative playground (preset sweeps, `im-gen` image
generation, side-by-side comparison) remain planned but are not on the
QA-workbench critical path. The Azure AI Foundry and AWS Bedrock adapters are now
implemented: Foundry over its OpenAI-compatible deployment API, Bedrock over
`Converse` with SigV4 signing delegated to the AWS SDK for Go v2 (third-party
modules are permitted since the standard-library-only rule was lifted). Azure
Entra ID (bearer token) credentials are the one piece still deferred.

## Not planned

Executing tests inside Loomwork, model hosting or fine-tuning, reimplementing
`api-test-runner`/`im-gen`/`cue-note` functionality, a relational database, and
multi-tenant concerns (accounts, RBAC, quotas) remain non-goals, as stated in
[`docs/INTENT.md`](INTENT.md).
