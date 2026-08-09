# System Architecture Specification

## 1. Layering

```text
  cmd/loomwork  (CLI: flags -> commands -> JSON/text output)
        │
        ▼
  internal/orchestrator   prompt-run pipeline: assemble -> generate -> persist
        │            │            │
        ▼            ▼            ▼
  internal/model  internal/provider  internal/preset  internal/cuenote
   (domain)        (adapters)         (registry)        (prompts/notes)
        │
        ▼
  internal/store  (JSON file persistence of projects)
```

Dependency rules (enforced by review, verified by `go vet` + tests):

* `internal/model` imports only the standard library. It knows nothing about HTTP,
  providers, presets, or storage.
* `internal/provider` imports only the standard library plus `internal/config`.
  It knows nothing about projects or artifacts — it takes a `Request` and returns
  a `Response`.
* `internal/preset` depends on `internal/provider` only for the normalized
  parameter type; it never performs I/O beyond reading its config file.
* `internal/orchestrator` is the only package allowed to combine domain, provider,
  preset, and store concerns.
* `cmd/loomwork` contains argument parsing and output formatting only — no
  business logic. A future HTTP server is a sibling of `cmd/loomwork`, reusing
  `internal/orchestrator` unchanged.

## 2. Prompt-Run Lifecycle

```text
  [ Phase 1: Resolve ]
        │  load project from store -> find target artifact (name or id)
        │  resolve provider+model+preset selector -> parameters
        ▼
  [ Phase 2: Assemble ]
        │  system prompt + pinned artifacts (standing context) + target artifact body
        │  prompt text: inline, from file, or rendered cue from cue-note
        ▼
  [ Phase 3: Generate ]
        │  provider.TextGenerator.Generate(ctx, Request) with bounded timeout
        ▼
  [ Phase 4: Persist ]
           new artifact (type=generated, parent=target, version=next for its name)
           metadata: provider, model, preset, prompt digest, duration, finish reason
           project saved atomically (temp file + rename); failure leaves project untouched
```

## 3. Domain Model (`internal/model`)

```text
Project
  ID, Name, Description, Tags[], CreatedAt, UpdatedAt
  Artifacts[]  (append-only for content; metadata may change in place)

Artifact
  ID, Name, Type, Version, Tags[], Pinned, ParentID
  Body { Content | Ref, MediaType }     // exactly one of Content/Ref
  Metadata map[string]string            // producer provenance, free-form
  CreatedAt
```

* **Types**: `spec`, `log`, `test-result`, `diagram`, `doc`, `generated`. The type
  is deliberately generic: a wiki page, a QA analysis, and a creative draft are all
  `doc`/`generated` artifacts, so new use cases need no schema change.
* **Versioning**: artifacts are identified for humans by `Name`; `(Name, Version)`
  is unique. `AddArtifact` bumps the version and chains `ParentID`, giving a linear
  history per name, while `DeriveArtifact` chains across names (target artifact →
  generated result) giving a lineage DAG.
* **Pinning**: a boolean on the artifact, queried via `PinnedArtifacts()`. It is
  the mechanism by which later use cases (wiki, workbench) assemble standing
  context without any new concept.
* **Immutability of content**: no API mutates `Body`. This keeps prompt runs
  reproducible and audit trails intact.

## 4. Provider Abstraction (`internal/provider`)

One interface for text:

```go
type TextGenerator interface {
    Name() string                                        // adapter identity, e.g. "ollama"
    Models(ctx context.Context) ([]Model, error)         // discovery / capability probe
    Generate(ctx context.Context, req Request) (Response, error)
}
```

`Request` carries a *normalized* parameter set (`Params`) plus an `Extra`
`map[string]any` escape hatch for backend-specific knobs. Each adapter maps
normalized parameters onto its own wire format:

| Normalized | Ollama (`options`) | LM Studio / OpenAI | Azure AI Foundry (planned) | Bedrock (planned) |
|---|---|---|---|---|
| `Temperature` | `temperature` | `temperature` | `temperature` | model-specific body |
| `TopP` | `top_p` | `top_p` | `top_p` | model-specific body |
| `TopK` | `top_k` | *(unsupported, ignored)* | *(model dependent)* | model-specific body |
| `MaxOutputTokens` | `num_predict` | `max_tokens` | `max_tokens` | model-specific body |
| `Stop` | `stop` | `stop` | `stop` | model-specific body |
| `Seed` | `seed` | `seed` | `seed` | model-specific body |
| `RepeatPenalty` | `repeat_penalty` | `frequency_penalty`-adjacent *(ignored)* | *(model dependent)* | model-specific body |
| `ContextWindow` | `num_ctx` | *(server-side)* | *(server-side)* | *(server-side)* |

Unsupported parameters are dropped, never guessed at — the mapping table above is
the contract, and each adapter's tests assert its wire body.

Adapter status:

* `ollama.go` — implemented (`POST /api/chat` with `stream:false`, `GET /api/tags`).
* `lmstudio.go` — implemented (`POST /v1/chat/completions`, `GET /v1/models`,
  optional bearer token). Any other OpenAI-compatible local server works by
  pointing the base URL at it.
* `azure.go` — scaffold: constructor validates endpoint/deployment/api-version and
  reads the key from the environment; `Generate` returns `ErrNotImplemented`.
* `bedrock.go` — scaffold: constructor validates region/model id and reads AWS
  credentials from the environment; `Generate` returns `ErrNotImplemented`.
  SigV4 signing is a pure-Go implementation task (no CGO), deliberately deferred.

Image generation is a separate interface because its shape is genuinely different
(asynchronous, multi-artifact):

```go
type ImageGenerator interface {
    Name() string
    Models(ctx context.Context) ([]Model, error)
    GenerateImages(ctx context.Context, req ImageRequest) (ImageResult, error)
}
```

The `imgen` adapter targets the local `ilyaus/im-gen` FastAPI service: `POST /jobs`
returns a job id, `GET /jobs/{id}` is polled until `succeeded`/`failed`, and the
result's artifacts (filename, path, download URL, size, seed) are returned. Polling
interval and overall deadline are caller-controlled via config and `context`.

`provider.BuildTextGenerator(cfg)` and `provider.BuildImageGenerator(cfg)` are the
only factories: each maps a `Kind` to an adapter, so callers select providers by
name from configuration and adding a backend never touches call sites. The
orchestrator takes the text factory as an injectable function, so tests substitute
a fake generator with no network.

## 5. Preset Registry (`internal/preset`)

Different models expose different useful parameter ranges, so parameters live in
data, not code.

```json
{
  "entries": [
    {
      "provider": "ollama",
      "model": "qwen3:8b",
      "defaults": { "temperature": 0.2, "top_p": 0.9, "num_ctx": 8192 },
      "presets": {
        "code-review":   { "temperature": 0.1, "max_output_tokens": 2048 },
        "brainstorm":    { "temperature": 0.9, "top_p": 0.95 }
      }
    }
  ]
}
```

* **Key**: `provider` + `model`. Lookup is exact; a `*` model entry provides
  provider-wide fallbacks.
* **Selector syntax**: `provider/model[#preset]` (e.g. `ollama/qwen3:8b#code-review`,
  `lmstudio/openai/gpt-oss-20b`). The model segment may itself contain `/`; only
  the first `/` and the last `#` are structural.
* **Resolution order** (later wins): built-in provider defaults → `*`-model entry
  defaults → model `defaults` → named preset → explicit caller overrides.
* **Validation at load time**: known provider kind, non-empty model, unique preset
  names per key, and range checks (temperature 0–2, top-p 0–1, top-k ≥ 0,
  max output tokens ≥ 1, repeat penalty ≥ 0, context window ≥ 1). Every error names
  the offending `provider/model#preset`, so a bad config fails fast and legibly.
* **Unknown preset** errors list the available preset names for that key.

Because presets resolve to `provider.Params`, a sweep (creative playground, later
session) is just "iterate presets" — no new abstraction needed.

## 6. cue-note Client (`internal/cuenote`)

```go
type Client interface {
    ListCues(ctx context.Context, filter CueFilter) ([]Cue, error)
    GetCue(ctx context.Context, id string) (Cue, error)
    CreateCue(ctx context.Context, c Cue) (Cue, error)
    ListNotes(ctx context.Context, filter NoteFilter) ([]Note, error)
    GetNote(ctx context.Context, id string) (Note, error)
    CreateNote(ctx context.Context, n Note) (Note, error)
}
```

Two implementations: `HTTPClient` (assumed REST contract, see
[`cue-note-contract.md`](cue-note-contract.md)) and `MemoryClient` (thread-safe,
deterministic ids, used by tests until the service ships). Template rendering
(`{{var}}` substitution with strict unresolved-variable errors) lives in the shared
layer so both implementations behave identically.

## 7. Persistence (`internal/store`)

The foundation ships a JSON file store: one file per project under
`$LOOMWORK_HOME/projects/<project-id>.json`, plus an index derived by scanning that
directory. Writes are atomic (write temp file → `os.Rename`). The store is defined
by an interface (`store.ProjectStore`) so object storage or a database can be
substituted later without touching orchestration.

## 8. Configuration and Secrets (`internal/config`)

* `LOOMWORK_HOME` (default `~/.loomwork`) holds `config.json`, `presets.json`, and
  `projects/`.
* Provider endpoints may be set in `config.json`; credentials are read **only** from
  environment variables (`AZURE_AI_API_KEY`, `AWS_ACCESS_KEY_ID`/
  `AWS_SECRET_ACCESS_KEY`/`AWS_SESSION_TOKEN`, `LMSTUDIO_API_KEY`,
  `CUENOTE_API_TOKEN`).
* Example configs live in `config/*.example.json` with placeholder values; real
  configs are git-ignored.

## 9. Deferred-Feature Extension Points

| Deferred feature | Hosts on |
|---|---|
| Wiki flow | `orchestrator` prompt runs emitting `doc` artifacts; chunking helper |
| Testing workbench | new `internal/exec` process runner for `api-test-runner`, `internal/ingest` for report → `test-result` artifact, presets for analysis models |
| `sdd-qa` generate→run→analyze→refine loop | chained prompt runs with artifact lineage as the loop's state |
| Creative playground | existing `ImageGenerator` adapter + preset iteration |
| HTTP surface | new `cmd/server` over the same `orchestrator` API |
