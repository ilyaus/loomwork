# Loomwork

A pure-Go orchestrator for running prompts over project artifacts with local and
remote models. You create a **project**, add **artifacts** to it (specs, logs, test
results, diagrams, docs), and run prompts against those artifacts through a
pluggable **model provider** — the result is stored back as a new, versioned
artifact with full lineage and provenance.

This repository currently contains the **foundation**: the domain model, the
provider abstraction, the per-model preset registry, a `cue-note` client, file-backed
persistence, and a CLI vertical slice that works end to end against a local model.
The larger product vision (wiki generation, testing workbench over
[`ilyaus/api-test-runner`](https://github.com/ilyaus/api-test-runner), the
`sdd-qa` generate→run→analyze→refine loop, creative playground, image generation via
`ilyaus/im-gen`) is specified in [`docs/INTENT.md`](docs/INTENT.md) with each part
marked implemented or deferred.

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
- **Pure Go, zero CGO, standard library only** — no third-party dependencies; one
  static binary.

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
`project create|list|show`, `artifact add|list|show|pin|unpin`, `cue list|show`,
`run`, `workbench run`, and `providers`.

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

## Workspace layout

`$LOOMWORK_HOME` (default `~/.loomwork`) holds all state:

```text
~/.loomwork/
  config.json          providers, cue-note endpoint, system prompt   (optional)
  presets.json         per-provider/model parameter presets          (optional)
  projects/<id>.json   one document per project, written atomically
```

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
- **HTTP surface** — a `cmd/server` sibling reusing `internal/orchestrator` unchanged.

[`docs/ROADMAP.md`](docs/ROADMAP.md) sequences these with scope and acceptance criteria.
