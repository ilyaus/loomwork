# Product and Functional Requirements

Scope note: requirements marked **[FOUNDATION]** are in scope for the current
implementation. Requirements marked **[DEFERRED]** are recorded for later sessions
and must not constrain the foundation beyond the extension points named in
[`INTENT.md`](INTENT.md).

## 1. Functional Requirements

### FR-001: Project Lifecycle **[FOUNDATION]**
* A project has a stable id, a name, optional description, tags, creation and
  update timestamps, and a set of artifacts.
* Project names must be unique within a workspace; creating a duplicate name must
  fail with a diagnostic naming the conflicting project id.
* A project must be serializable to and from JSON without loss, so any store
  (file, object storage, database) can persist it.

### FR-002: Artifact Model **[FOUNDATION]**
* An artifact has: id, project-scoped name, type, version, tags, pinned flag,
  optional parent id, optional producer metadata, timestamps, and a body that is
  either inline content or an external reference (path or URL) with a media type.
* Supported types: `spec`, `log`, `test-result`, `diagram`, `doc`, `generated`.
  Unknown types must be rejected at construction time.
* An artifact must carry exactly one of inline content or a reference; both empty
  or both present is a validation error.
* Artifacts are immutable in content: mutating operations produce a new artifact
  version rather than editing an existing record. Metadata-only operations
  (pinning, tagging) may update in place.

### FR-003: Versioning and Lineage **[FOUNDATION]**
* Adding a new revision of an artifact name assigns `version = previous + 1` and
  sets the new artifact's parent to the previous revision's id.
* A project must expose the latest version for a given artifact name, and the full
  ordered history for that name.
* Deriving an artifact from another (e.g. a prompt result) records the source
  artifact id as parent and the producing provider/model/preset as metadata.

### FR-004: Pinning **[FOUNDATION]**
* Any artifact may be pinned or unpinned; pinning is metadata, never a copy.
* The project must expose the set of pinned artifacts so orchestration can include
  them as standing context in prompt runs.
* Pinning applies to a specific artifact version; pinning a new version does not
  implicitly unpin older ones, but the caller may query pinned-latest only.

### FR-005: Text Generation Provider Interface **[FOUNDATION]**
* Exactly one interface (`provider.TextGenerator`) defines text generation:
  `Generate(ctx, Request) (Response, error)` plus `Name()` and a capability probe
  (`Models(ctx)`).
* A `Request` carries model, system prompt, user prompt, ordered context blocks,
  and a normalized parameter set (temperature, top-p, top-k, max output tokens,
  stop sequences, seed, repeat penalty, plus a provider-specific `Extra` map).
* A `Response` carries generated text, model actually used, finish reason, token
  usage when available, and provider-specific raw metadata.
* Adapters must translate the normalized parameters into their wire format and
  must ignore, not fail on, parameters their backend does not support — except
  where ignoring would silently change semantics, which must be an error.
* Every network call must honor `context.Context` cancellation and a configurable
  timeout, and must wrap transport/status errors with provider, model, and
  endpoint context.

### FR-006: Local Provider Adapters **[FOUNDATION]**
* **Ollama** — talk to the native local API: `POST /api/generate` (or `/api/chat`)
  with `stream=false`, and `GET /api/tags` for model discovery. Default base URL
  `http://localhost:11434`, overridable.
* **LM Studio** — talk to the local OpenAI-compatible API:
  `POST /v1/chat/completions` and `GET /v1/models`. Default base URL
  `http://localhost:1234/v1`, overridable. An optional API key is sent as a bearer
  token when configured.
* Non-2xx responses must produce an error containing the status code and a
  truncated response body.

### FR-007: Remote Provider Scaffolds **[FOUNDATION — scaffold only]**
* **Azure AI Foundry** and **AWS Bedrock** adapters must exist, implement
  `provider.TextGenerator`, and be constructible from configuration
  (endpoint/deployment/API version/region/model id, credentials from environment).
* Until completed they must return a typed `ErrNotImplemented`-wrapped error from
  `Generate`, so callers can detect the condition without string matching.
* Construction must still validate configuration and surface missing credentials,
  so wiring can be verified before request mapping exists.

### FR-008: Image Generation Provider **[FOUNDATION]**
* A separate `provider.ImageGenerator` interface defines image generation
  asynchronously: submit a job, poll status, and return generated artifact
  descriptors (filename, path/download URL, media type, size, seed).
* An adapter must implement it against the local `im-gen` service:
  `POST /jobs`, `GET /jobs/{id}`, `GET /jobs/{id}/result`, `GET /models`.
* Polling must be bounded by context and a configurable interval, and a `failed`
  job must return the service-reported error message.

### FR-009: Per-Model Preset Registry **[FOUNDATION]**
* Presets are keyed by `provider` + `model` + preset name, loaded from a JSON
  config file, with an optional per-`provider`+`model` `defaults` entry.
* Resolution order: named preset values override model defaults, which override
  provider-level built-in defaults; an explicit caller override wins over all.
* The registry must validate on load: known provider names, non-empty model,
  numeric ranges (temperature 0–2, top-p 0–1, top-k ≥ 0, max tokens ≥ 1,
  repeat penalty ≥ 0), and no duplicate preset names for the same key. Errors must
  name the offending entry.
* Requesting an unknown preset must fail with a diagnostic listing the presets
  available for that provider+model.
* Selectors must be parseable from a single string: `provider/model[#preset]`.

### FR-010: cue-note Client **[FOUNDATION]**
* A `cuenote.Client` interface must cover: list/get/create cues (reusable prompts,
  optionally templated), and list/get/create notes attached to a project.
* An HTTP implementation must target the contract documented in
  [`cue-note-contract.md`](cue-note-contract.md), with base URL and optional API
  token from configuration.
* An in-memory implementation must satisfy the same interface with thread-safe
  storage, so the foundation and its tests never require a running cue-note.
* Template rendering of a cue (`{{var}}` substitution) must be part of the shared
  layer, not of a specific implementation, and must fail on unresolved variables.

### FR-011: Prompt Run Orchestration **[FOUNDATION]**
* A prompt run takes a project, a target artifact, a provider selector, an
  optional preset, and a prompt (inline or a cue id), assembles the request
  (system prompt + pinned artifacts + target artifact content), calls the
  provider, and appends the response as a new artifact.
* The produced artifact defaults to type `generated`, records provider, model,
  preset, prompt digest, and duration in its metadata, and sets its parent to the
  target artifact.
* A failed run must not modify the project.

### FR-012: CLI Vertical Slice **[FOUNDATION]**
* Commands: `project create|list|show`, `artifact add|list|show|pin|unpin`,
  `run` (prompt run), `providers` (list configured providers/presets).
* Every command must be scriptable: non-zero exit code on failure, diagnostics on
  stderr, and machine-readable JSON output available via a flag.
* The workspace directory is configurable via flag or `LOOMWORK_HOME`, defaulting
  to `~/.loomwork`.

### FR-013: Wiki Generation Flow **[DEFERRED]**
### FR-014: Testing Workbench over `api-test-runner` + `sdd-qa` loop **[DEFERRED]**
### FR-015: Creative Playground (sweeps, comparisons, promotion) **[DEFERRED]**
### FR-016: HTTP server / multi-user surface **[DEFERRED]**

## 2. Non-Functional Requirements

### NFR-001: Configuration and Secret Handling
* All provider endpoints and credentials come from environment variables or an
  untracked local config file; example configs contain placeholders only.
* Secrets must never be logged, echoed in errors, or written into artifacts or
  project files.

### NFR-002: Bounded Network Behavior
* Every provider call has a configurable timeout (default 120s for generation,
  10s for discovery) enforced via `context.WithTimeout`.
* Local-provider defaults must work with no configuration on a developer machine.

### NFR-003: Thread Safety
* Stores, registries, and stub clients must be safe for concurrent use, protected
  by `sync.RWMutex` or immutability.

### NFR-004: Pure Go, Zero CGO
* `CGO_ENABLED=0` builds must produce a single static binary; no native
  dependencies, and standard library first.

### NFR-005: Test Coverage of Contracts
* Domain rules, preset validation/resolution, provider request/response mapping
  (via `httptest`), and the cue-note stub must all have unit tests. Tests must
  never require a live model server.

### NFR-006: Extensibility Cost
* Adding a provider must require only a new adapter plus a registry entry; no
  changes to domain, orchestration, or CLI code.
