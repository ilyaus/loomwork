# Product and Functional Requirements

The authoritative product spec is [`loom-work-vision.md`](loom-work-vision.md);
this document decomposes it into testable requirements and must be corrected
whenever the two disagree.

Scope note: requirements marked **[FOUNDATION]** are in scope for the current
implementation. **[PHASE n]** marks a requirement belonging to that phase of the
vision's build plan (see [`ROADMAP.md`](ROADMAP.md)). Requirements marked
**[DEFERRED]** are recorded for later sessions and must not constrain the
foundation beyond the extension points named in [`INTENT.md`](INTENT.md).

## 1. Functional Requirements

### FR-001: Project Lifecycle **[FOUNDATION]**
* A project has a stable id, a name, optional description, tags, creation and
  update timestamps, document source links, and a set of artifacts.
* Project names must be unique within a workspace; creating a duplicate name must
  fail with a diagnostic naming the conflicting project id.
* A project must be serializable to and from JSON without loss, so any store
  (file, object storage, database) can persist it.

### FR-001a: Project Directory Layout **[PHASE 1]**
* A project is persisted as a directory `<projects-root>/<project-id>/` holding
  `project.json` plus the subfolders `requirements/`, `agent-definitions/`,
  `test-suites/`, `executor-config/`, and `reports/`. All subfolders exist from
  project creation, before any entity is written into them.
* `project.json` is the metadata document and index cache: name, description,
  tags, document source links, timestamps, and derived counts (total and active
  requirements) so a directory-of-projects view needs no full scan.
* Writes must be atomic (temp file + rename) and read-modify-write cycles must be
  serialized across processes by a directory lock, since every CLI invocation is
  a separate process.
* A project written by an earlier flat `<project-id>.json` layout must remain
  readable and be migrated to a directory the first time it is written.

### FR-001b: Document Source Links **[PHASE 1]**
* A project records any number of named document sources, each with a source type
  (`ado`, `confluence`, `github`, `other`), a URL, and optionally a local path or
  an S3 URI for a stored copy of the document.
* A source must carry at least one location (URL, local copy, or S3 copy);
  unknown source types must be rejected.
* Re-adding a source with an existing name updates it rather than duplicating it.

### FR-001c: Requirement Entity **[PHASE 1]**
* A requirement is a typed domain entity, not a generic artifact. Its fields are
  id, version, tester-friendly text, optional `source_type` and `source_ref`
  back-reference, status, origin, tags, metadata, and a creation timestamp. The
  wire format is fixed by
  [`schemas/requirement.schema.json`](schemas/requirement.schema.json).
* Status is one of `active`, `obsolete`, or `superseded`. Obsolete requirements
  are retained for audit, never deleted. A superseded version's status is frozen,
  because a newer version already carries the current text.
* Origin is `authored` (entered by a QA engineer) or `extracted` (produced by
  later LLM document analysis). Both paths write the same schema to the same
  store.
* A `source_ref` requires a `source_type`; empty text is rejected.

### FR-001d: Requirement Versioning and Persistence **[PHASE 1]**
* Each requirement version is a discrete retrievable snapshot stored as its own
  file, `requirements/<requirement-id>.v<version>.json`. No diff view is
  required.
* An update writes the next version and marks the previous one `superseded`;
  earlier versions remain byte-for-byte retrievable. Fields the update omits are
  inherited from the previous version.
* `requirements/index.json` records, per requirement id, the current-version
  pointer, every retained version, and the current status.
* Requirement ids follow a stable `req-NNN` sequence within a project.

### FR-001e: Requirement CLI **[PHASE 1]**
* `requirement create` accepts text inline or from a file plus optional source
  type/reference, status, origin, and tags.
* `requirement list` lists the current version of every requirement, optionally
  filtered by status. `requirement show` returns the current version, a specific
  version, or the full retained history.
* `requirement update` creates the next version; `requirement set-status` changes
  the status of the current or a specific version.
* `project create --source` and `project source --source` manage document source
  links.

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
* Commands: `project create|list|show|source`,
  `requirement create|list|show|update|set-status`,
  `artifact add|list|show|pin|unpin`, `run` (prompt run),
  `workbench run`, `providers` (list configured providers/presets).
* Every command must be scriptable: non-zero exit code on failure, diagnostics on
  stderr, and machine-readable JSON output available via a flag.
* The workspace directory is configurable via flag or `LOOMWORK_HOME`, defaulting
  to `~/.loomwork`.

### FR-013: Wiki Generation Flow **[DEFERRED]**
### FR-014: Testing Workbench over `api-test-runner` + `sdd-qa` loop **[DEFERRED]**
### FR-015: Creative Playground (sweeps, comparisons, promotion) **[DEFERRED]**
### FR-016: LLM document analysis and requirement extraction **[PHASE 2 — DEFERRED]**
### FR-017: Agent definitions, override rules, `AgentAdapter`, test generation **[PHASE 3 — DEFERRED]**
### FR-018: Execution contract, report ingestion, HTML rendering **[PHASE 4 — DEFERRED]**
### FR-019: Run comparison and testability dashboard **[PHASE 5 — DEFERRED]**
### FR-020: Browser UI over a local backend API **[DEFERRED]**
* The earlier `serve`/`initial_ui` HTTP+UI attempt was discarded and removed; the
  UI will be rebuilt fresh over the typed domain entities.

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

### NFR-004: Single Static Binary
* `CGO_ENABLED=0` builds must produce a single static binary with no native
  dependencies.
* Third-party Go modules are allowed where they clearly earn their place; the
  earlier standard-library-only rule no longer applies. Prefer the standard
  library when it is adequate, and keep each dependency justified.

### NFR-005: Test Coverage of Contracts
* Domain rules, preset validation/resolution, provider request/response mapping
  (via `httptest`), and the cue-note stub must all have unit tests. Tests must
  never require a live model server.

### NFR-006: Extensibility Cost
* Adding a provider must require only a new adapter plus a registry entry; no
  changes to domain, orchestration, or CLI code.
