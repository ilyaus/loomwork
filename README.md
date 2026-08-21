# Loomwork

A single-user, local-first **QA workbench** that organizes documentation sources,
requirements, agent-generated test suites, and execution reports — without
executing the tests itself. A **project** is a directory on disk holding typed,
versioned QA entities plus free-form **artifacts** (specs, logs, test results,
docs); prompts run against those artifacts through a pluggable **model provider**
and the result is stored back as a new versioned artifact with full lineage.

[`docs/loom-work-vision.md`](docs/loom-work-vision.md) is the authoritative
product spec and [`docs/ROADMAP.md`](docs/ROADMAP.md) sequences its five phases.
What exists today: the project directory store, document source links, typed
versioned **requirements** (phase 1), plus the foundation the later phases build
on — the provider abstraction, the per-model preset registry, a `cue-note`
client, and a CLI vertical slice that works end to end against a local model.
LLM document analysis, agent definitions and override rules, test generation, the
execution contract, and the browser UI are phases 2–5.

## Features

- **Generic artifact model** — one `Artifact` shape covers every use case:
  type (`spec`, `log`, `test-result`, `diagram`, `doc`, `generated`), inline content
  *or* a local reference, tags, per-name versioning, pinning, parent lineage, and
  free-form metadata. Prompt runs never mutate an artifact; they derive a new one.
- **One provider interface, many backends** — `provider.TextGenerator` with
  fully implemented **Ollama** (native API) and **LM Studio** (OpenAI-compatible)
  adapters, plus **Azure AI Foundry** and **AWS Bedrock** scaffolds behind the same
  interface and config wiring. A separate `provider.ImageGenerator` interface has an
  adapter for the local `im-gen` service (submit → poll → collect).
- **Per-model presets** — tunable parameters live in data, keyed by
  `provider` + `model`, with wildcard provider defaults, named presets, load-time
  validation, and a `provider/model#preset` selector syntax.
- **Pinned standing context** — pin artifacts once and include them in any run with
  `--include-pinned`; this is the primitive the deferred wiki/workbench flows build on.
- **cue-note client** — an interface with an HTTP implementation (contract in
  [`docs/cue-note-contract.md`](docs/cue-note-contract.md)) and an in-memory stub, so
  work here is not blocked on that service shipping.
- **Typed QA entities, not stringly-typed blobs** — `model.Requirement` carries
  its own id, versions, status, origin, and source back-reference; agent
  definitions, test suites, and reports follow in later phases.
- **Directory-per-project storage** — `project.json` plus a subfolder per entity
  family, atomic writes, and a cross-process lock. No database.
- **One static binary** — `CGO_ENABLED=0`; third-party Go modules are allowed
  where they earn their place.

## Build

```bash
make build      # builds bin/loomwork with CGO_ENABLED=0
make test       # runs the unit test suite
make vet fmt    # static analysis and formatting
```

## Quick start (local Ollama)

```bash
export LOOMWORK_HOME=~/.loomwork          # optional; this is the default

loomwork project create --name triage --tags ops
loomwork artifact add --project triage --name api.log --type log --file ./api.log --pin
loomwork run --project triage --artifact api.log \
    --model ollama/qwen3:8b#log-triage \
    --prompt "Summarize the distinct errors and their likely root cause." \
    --name triage-summary --include-pinned
loomwork artifact show --project triage --artifact triage-summary
```

```text
api.log -> triage-summary (art-89fbbc12429af3a932a91c60b01d190e v1)
provider: ollama   model: qwen3:8b   preset: log-triage
duration: 2.4s     tokens: 120 prompt / 24 completion
```

Every command accepts `--home PATH` (workspace override) and `--json`
(machine-readable output). `loomwork --help` lists the full command set:
`project create|list|show|source`,
`requirement create|list|show|update|set-status`,
`artifact add|list|show|pin|unpin`, `cue list|show`, `run`, `workbench run`, and
`providers`.

### Document sources and requirements

A project links to the documentation it is tested against, and requirements are a
first-class versioned entity rather than a document:

```bash
loomwork project create --name checkout \
    --source "name=spec,type=confluence,url=https://wiki/checkout,local=./checkout.pdf"
loomwork project source --project checkout \
    --source "name=stories,type=ado,url=https://dev.azure.com/org/proj"

loomwork requirement create --project checkout --text "Cart totals include tax" \
    --source-type ado --source-ref AB#1234 --tags cart
loomwork requirement update --project checkout --requirement req-001 \
    --text "Cart totals include tax and shipping"
loomwork requirement show --project checkout --requirement req-001 --history
loomwork requirement set-status --project checkout --requirement req-001 --status obsolete
```

An update writes the next version and marks the previous one `superseded`; every
version stays a discrete retrievable snapshot and obsolete requirements are
retained for audit rather than deleted. The wire format is fixed by
[`docs/schemas/requirement.schema.json`](docs/schemas/requirement.schema.json),
so QA-authored and (later) LLM-extracted requirements share one schema and one
store.

### Reusable prompts (cues)

A prompt can come from a cue-note cue instead of `--prompt`/`--prompt-file`:

```bash
loomwork cue list --tag ops
loomwork cue show --cue log-triage
loomwork run --project triage --artifact api.log --model ollama/qwen3:8b \
    --cue log-triage --var service=checkout --var since=2024-05-01
```

`--cue` accepts a cue id or an exact (case-insensitive) name; an ambiguous name is
rejected rather than guessed. `{{var}}` placeholders in the cue body are filled from
`--var key=value`, and a missing value fails before any provider is called. The
generated artifact records `cue` and `cueId` metadata so a run can be traced back to
the prompt that produced it. Point the CLI at a cue-note instance with the
`cuenote.baseUrl` config key (default `http://localhost:8090`).

### Testing workbench

`workbench run` executes the sibling
[`api-test-runner`](https://github.com/ilyaus/api-test-runner) CLI over scenario
artifacts and ingests its JSON report as a `test-result` artifact:

```bash
loomwork workbench run --project triage --scenarios orders-scenarios \
    --base-url http://localhost:9999 --auth-config auth.json --name orders.results
```

Scenario artifacts (Markdown, typically `spec` type — hand-written or produced by
a prompt run) are materialized into a temporary directory and passed to the
runner via `--scenarios`. The runner's stdout report becomes the artifact body,
its aggregate counts (`outcome`, `total`, `passed`, `failed`, `skipped`) land in
artifact metadata, and the artifact's parent is the first scenario artifact so
lineage is preserved. The binary is located via `--runner`, the
`workbench.runnerPath` config key, or an `api-test-runner` PATH lookup. The
process runs without a shell, with only the environment variables named in
`workbench.env`, and is killed after `--timeout` seconds
(`workbench.timeoutSeconds`, default 10 minutes). Extra runner flags pass through
with repeatable `--arg` (e.g. `--arg --max-parallelism --arg 2`); `--dry-run`
validates scenarios without HTTP calls. The sdd-qa generate→run→analyze→refine
loop composes this with ordinary `run` invocations — see
[`docs/ROADMAP.md`](docs/ROADMAP.md) item 2.

Artifact bodies come from exactly one of `--content TEXT` (inline),
`--file PATH` (copied into the project), or `--ref PATH` (referenced in place and
read at run time). Adding an artifact under an existing name creates the next
revision rather than overwriting.

### Browser UI

```
loomwork serve [--addr 127.0.0.1:8787]
```

Serves a single-page workbench UI and its JSON API over the same workspace the
CLI uses. The assets are embedded in the binary, so there is nothing to install
or build separately.

This first increment covers the Phase-1 domain only: the directory-of-projects
landing view (per-project counts come from the cached `project.json` index, so
no project scan is needed), a project view with its document source links, and
requirement management — create, list, inspect version history, save a new
version, and change status. Last-tested date, requirement coverage, and
open-gaps counts are rendered as placeholders until later phases populate them.
LLM surfaces are not exposed yet.

`--addr` accepts a bare port or a loopback address; non-loopback hosts are
rejected. Loomwork is local-first and single-user, so the server has no
authentication and must stay on the loopback interface. Requirement payloads on
the wire match [`docs/schemas/requirement.schema.json`](docs/schemas/requirement.schema.json)
exactly.

## Workspace layout

`$LOOMWORK_HOME` (default `~/.loomwork`) holds all state:

```text
~/.loomwork/
  config.json          providers, cue-note endpoint, system prompt   (optional)
  presets.json         per-provider/model parameter presets          (optional)
  projects/<project-id>/
    project.json       metadata, document source links, index cache
    requirements/      req-001.v1.json, req-001.v2.json, index.json
    agent-definitions/ (phase 3)
    test-suites/       (phase 3)
    executor-config/   (phase 4)
    reports/           (phase 4)
```

Every file is written atomically (temp file + rename) and read-modify-write
cycles hold a directory lock, so concurrent CLI invocations cannot lose each
other's changes. Projects stored by the earlier flat `projects/<id>.json` layout
are still read and migrate to a directory on first write.

Both JSON files are optional — with no configuration at all, Loomwork targets
Ollama on `http://localhost:11434` and LM Studio on `http://localhost:1234/v1`.

## Configuring providers

Copy [`config/config.example.json`](config/config.example.json) to
`$LOOMWORK_HOME/config.json` and edit the endpoints. Each entry is keyed by a name
you choose; `kind` selects the adapter (`ollama`, `lmstudio`, `azure`, `bedrock`,
`imgen`) and defaults to the key when omitted.

**Credentials are never read from config or source — only from the environment:**

| Variable | Used by |
|---|---|
| `LMSTUDIO_API_KEY` | LM Studio / OpenAI-compatible servers (optional bearer token) |
| `AZURE_AI_API_KEY` | Azure AI Foundry adapter |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, `AWS_REGION` | Bedrock adapter |
| `CUENOTE_API_TOKEN` | cue-note HTTP client |

`loomwork providers` prints each configured provider, its endpoint, its preset
groups, and whether its credentials are present — without printing any secret.

## How presets work

Different models expose different useful parameters, so they are configuration, not
code. Copy [`config/presets.example.json`](config/presets.example.json) to
`$LOOMWORK_HOME/presets.json`:

```json
{
  "entries": [
    {
      "provider": "ollama",
      "model": "qwen3:8b",
      "defaults": { "temperature": 0.2, "top_p": 0.9, "num_ctx": 8192 },
      "presets": {
        "log-triage": { "temperature": 0.1, "max_output_tokens": 2048 }
      }
    }
  ]
}
```

- Selector syntax is `provider/model[#preset]`; the model segment may contain `/`
  (e.g. `lmstudio/qwen/qwen3-8b#summarize`).
- Resolution order, later winning: built-in provider defaults → `*`-model entry →
  model `defaults` → named preset → CLI flags (`--temperature`, `--top-p`,
  `--top-k`, `--max-tokens`, `--seed`).
- Parameters are normalized and mapped per adapter (see the mapping table in
  [`docs/architecture.md`](docs/architecture.md)); an `extra` object passes
  backend-specific knobs straight through. Unsupported parameters are dropped, not
  guessed at.
- Invalid presets fail at load time with the offending `provider/model#preset` named.

## Documentation

| Document | Contents |
|---|---|
| [`.specify/memory/constitution.md`](.specify/memory/constitution.md) | Non-negotiable engineering principles |
| [`docs/INTENT.md`](docs/INTENT.md) | Full product vision, implemented vs. deferred |
| [`docs/REQUIREMENTS.md`](docs/REQUIREMENTS.md) | Functional and non-functional requirements |
| [`docs/architecture.md`](docs/architecture.md) | Layering, domain model, provider mapping, presets, persistence |
| [`docs/cue-note-contract.md`](docs/cue-note-contract.md) | Assumed cue-note REST contract |
| [`docs/ROADMAP.md`](docs/ROADMAP.md) | Sequenced next work items with acceptance criteria |

## Layout

```text
cmd/loomwork          CLI entry point (argument parsing and output only)
internal/model        projects, artifacts, versioning, lineage (stdlib only)
internal/provider     TextGenerator/ImageGenerator interfaces and adapters
internal/preset       config-driven provider+model preset registry
internal/cuenote      cue-note interface, HTTP client, in-memory stub
internal/exec         argv-only process runner: env allowlist, timeout, no shell
internal/ingest       api-test-runner report → test-result artifact mapping
internal/orchestrator prompt-run pipeline: resolve → assemble → generate → persist
internal/store        atomic JSON project persistence behind an interface
internal/config       workspace paths, configuration loading, defaults
```

## Roadmap (deferred)

These are specified in `docs/INTENT.md` and have named extension points, but are
**not** implemented here:

- **Wiki generation** — chunked multi-pass runs producing linked `doc` artifacts.
- **Testing workbench Lambda path** — the CLI loop is implemented (`workbench run`);
  invoking the runner's Lambda handler remotely is not.
- **Creative playground** — preset sweeps plus image generation wired into the CLI
  through the existing `im-gen` adapter.
- **Azure AI Foundry and Bedrock adapters** — constructors, config, and credential
  handling exist; `Generate` returns `provider.ErrNotImplemented`.

[`docs/ROADMAP.md`](docs/ROADMAP.md) sequences these with scope and acceptance criteria.
