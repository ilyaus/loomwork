# Loomwork — Technical Spec

> Draft for review. Companion to [`INTENT.md`](INTENT.md). Fuller treatments live
> in [`docs/architecture.md`](docs/architecture.md),
> [`docs/REQUIREMENTS.md`](docs/REQUIREMENTS.md), and
> [`docs/schemas/`](docs/schemas/). Where they disagree with
> [`docs/loom-work-vision.md`](docs/loom-work-vision.md), the vision wins.

## 1. Stack and build

- Go 1.24, `CGO_ENABLED=0` → one static binary (`bin/loomwork`). Third-party Go
  modules allowed where they earn their place; AWS SDK for Go v2 is used for
  Bedrock signing only.
- React/Vite UI in `web/ui`, built into `web/dist`, embedded via
  `//go:embed all:dist`. `loomwork serve` binds loopback only.
- `make build` (UI then binary), `make test` (`go test ./...`), `make vet`,
  `gofmt -l .` must be clean. UI regression: `npm --prefix web/ui run test:ui`.

## 2. Layering

```
cmd/loomwork ── CLI: flags → commands → JSON/text
     │
internal/cli ── argument parsing + output formatting only
     │
internal/orchestrator ── prompt-run pipeline (assemble → generate → persist), transport-agnostic
     │            internal/httpapi ── handler layer over the same store, serves embedded UI
     ▼
internal/model     internal/provider   internal/preset   internal/cuenote
 (domain, stdlib)   (adapters)          (registry)        (prompts/notes)
internal/analysis  internal/testgen  internal/traceability  internal/projectimport
     │
internal/store ── DirStore: project.json + entity subfolders, atomic writes, dir lock
```

Dependency rules (review-enforced): `internal/model` imports only the standard
library — no HTTP, providers, or storage. `internal/provider` knows nothing about
projects or artifacts; it maps a normalized `Request` to a `Response`.
`internal/orchestrator` is the only package that combines domain + provider +
preset + store. `internal/httpapi` is a sibling of the CLI reusing the same
store/orchestrator unchanged.

## 3. Storage layout

```
$LOOMWORK_HOME/                         # default ~/.loomwork
  config.json  presets.json
  projects/<project-id>/
    project.json                        # name, description, tags, sources,
                                        #   index cache, import provenance,
                                        #   testDocuments config
    requirements/
      <id>.v<n>.json                    # discrete immutable snapshot per version
      index.json                        # current-version pointer + status per id
    agent-definitions/
      <name>.v<n>.md                    # versioned agent definition (frontmatter + body)
      override-rules/<id>.v<n>.json     # versioned override rules
      current.json                      # active-version pointers
    test-suites/<suite-id>/v<n>/tests/<tc>.json
    executor-config/                    # phase 4
    reports/<suite-id>/v<n>/<timestamp>.json   # append-only run records
```

Every write is temp-file + `os.Rename`; read-modify-write holds a cross-process
directory lock (each CLI call is its own process); readers bypass the lock. A
project written by the older flat `projects/<id>.json` layout stays readable and
migrates to a directory on first write. `store.CreateWithRequirements` stages an
imported project in a temp dir and publishes it atomically.

## 4. Domain model

### Artifact (generic, free-form)
`id`, project-scoped `name`, `type` (`spec` | `log` | `test-result` | `diagram` |
`doc` | `generated`), `version`, `tags[]`, `pinned`, optional `parentId`,
`metadata` (producer provenance), `createdAt`, and a `body` that is **exactly
one** of inline `content` or an external `ref` + media type. Content is
immutable: mutating operations append a new version and chain `parentId`;
`DeriveArtifact` chains across names (target → generated result) forming a
lineage DAG. Pinning is metadata; pinned artifacts are the standing context
included in a run with `--include-pinned`.

### Requirement (typed entity, stored outside the project doc)
`id`, `version`, tester-friendly `text`, optional `source_type`
(`ado` | `confluence` | `github` | `other`) + `source_ref`, `status`
(`active` | `obsolete` | `superseded`), `origin` (`authored` | `extracted`),
`tags[]`, `metadata`, `createdAt`. Wire format fixed by
[`docs/schemas/requirement.schema.json`](docs/schemas/requirement.schema.json).

- `superseded` is set only by writing a newer version; a superseded version's
  status is then frozen. Obsolete requirements are retained, never deleted.
- An update writes the next version; omitted fields inherit from the previous.
- **ID forms** (`internal/model/requirementid.go`): local `req-NNN` or spec-kit
  `<featurePrefix>-FR|NFR|SC-<n>` (e.g. `008-FR-001`). `ReferenceID()` /
  `display_id` exposes the canonical spec form for imported requirements; both
  the storage id and the reference id are accepted on read.
- `Project.Import.RequirementsReadOnly` blocks all requirement writes for an
  imported project; local artifacts, suites, agents, rules, and reports stay
  writable.

### Agent definition + override rule
Agent definition: `agent_name`, `version`, `target` (`claude-agent-sdk` |
`copilot-sdk`), `model`, `tools_allowed[]`, Markdown body. Override rule: `id`,
`version`, `title`, `rationale`, a structured `condition` (methods, path glob,
`scenario` from a closed set, `spec_status`) and an `action`
(`expect-status` | `expect-empty-collection` | `skip-test`), `status`. Schemas:
[`agent-definition.schema.json`](docs/schemas/agent-definition.schema.json).

### Test suite + test case
Suite: `suite_id`, `version`, `title`, `cases[]`. Case
([`test-case.schema.json`](docs/schemas/test-case.schema.json)): `id` (`tc-NNN`),
`name`, `requirement_ids[]` (empty is valid but flags the suite incomplete),
`overrides_applied[]` (`<rule-id>-v<n>`), `scenario` (same closed set as a
rule's condition, so rules and cases match mechanically), `request`
(method + path + …), `expected`.

### Document suite (derived)
`Project.testDocuments.roots` lists relative folder prefixes (spec-kit imports
default to each feature's `specs/<feature>/sdd-qa`). Markdown files under those
roots are grouped in the UI tree under Test Suites with family
`document-suites`, keeping their artifact ids, history, and read API. No files
move; native suite coverage is unchanged.

## 5. Traceability (`internal/traceability`, derived on read)

`RequirementTests` builds `requirement id → []TestLink` from: current test-suite
cases (`requirement_ids`) and the latest scenario artifacts (tag `test-scenario`
or spec-kit FR filenames/frontmatter, scoped by feature). Markdown links require
explicit `Requirements:` fields or spec-kit conventions; they never alter native
coverage. Requirement-ID aliases (local ↔ spec form) resolve to the same target.

## 6. Provider abstraction (`internal/provider`)

```go
type TextGenerator interface {
    Name() string
    Models(ctx) ([]Model, error)
    Generate(ctx, Request) (Response, error)
}
```

`Request` carries model, system prompt, user prompt, ordered context blocks, and
a **normalized** parameter set (`Temperature`, `TopP`, `TopK`, `MaxOutputTokens`,
`Stop`, `Seed`, `RepeatPenalty`, `ContextWindow`) plus an `Extra` map escape
hatch. Each adapter maps normalized params to its wire format and **drops**
unsupported ones (never guesses); every call honors `context` cancellation and a
bounded timeout and wraps errors with provider/model/endpoint context.

| Adapter | Transport |
|---|---|
| Ollama | `POST /api/chat` (`stream:false`), `GET /api/tags`; default `http://localhost:11434` |
| LM Studio | OpenAI-compatible `POST /v1/chat/completions`, `GET /v1/models`; optional bearer |
| Azure AI Foundry | `POST {endpoint}/openai/deployments/{dep}/chat/completions?api-version=…`, key in `api-key` header (Entra ID bearer deferred) |
| AWS Bedrock | `Converse` API, `ListFoundationModels`; SigV4 via AWS SDK for Go v2; creds from env or shared-config profile |

Separate `ImageGenerator` interface (async: submit job → poll → collect) with an
`im-gen` adapter; implemented, not yet CLI-exposed. `provider.ErrNotImplemented`
is the typed sentinel for an adapter landed behind the interface before its
mapping exists.

## 7. Preset registry (`internal/preset`)

Keyed by `provider` + `model` (+ optional `*`-model provider fallback), loaded
from `presets.json`. Resolution (later wins): built-in provider defaults →
`*`-model defaults → model `defaults` → named preset → explicit caller override.
Selector syntax `provider/model[#preset]`; only the first `/` and last `#` are
structural. Load-time validation: known provider kind, non-empty model, unique
preset names per key, ranges (temperature 0–2, top-p 0–1, top-k ≥ 0, max
tokens ≥ 1, repeat penalty ≥ 0, context window ≥ 1); errors name the offending
`provider/model#preset`. Unknown-preset errors list what is available.

## 8. Prompt-run lifecycle (`internal/orchestrator`)

1. **Resolve** — load project, find target artifact, resolve selector → params.
2. **Assemble** — system prompt + pinned artifacts (if `--include-pinned`) +
   target artifact body; prompt text inline, from file, or a rendered cue
   (`{{var}}` substitution, strict on unresolved vars).
3. **Generate** — `TextGenerator.Generate` with bounded timeout.
4. **Persist** — new artifact, `type=generated`, `parent=target`,
   `version = next for its name`, metadata records provider / model / preset /
   prompt digest / duration / finish reason. A failed run leaves the project
   untouched.

## 9. Interfaces

### CLI (`loomwork <group> <command>`, all scriptable, `--json`, `--home`)

```
project     create | import | list | show | source
requirement create | list | show | update | set-status
artifact    add | list | show | pin | unpin
analysis    run | import
agent-definition create | update | list | show |
            rule-create | rule-update | rule-set-status | rule-list | rule-show
test-suite  generate | import | list | show
cue         list | show
run                              # prompt run
workbench   run                  # shell out to api-test-runner, ingest report
serve                            # local API + embedded UI, loopback only
providers                        # list configured providers / presets
```

`project import --path ABS --format spec-kit [--features a,b] [--preview]`:
`--preview` inspects (feature list, counts, warnings) without writing; without it
a snapshot project is created with `requirementsReadOnly` set.

### HTTP API (`internal/httpapi`, `GET /api/openapi.json` is the contract)

Explicit `switch` on path segments in `server.go`; one handler file per entity;
`openapi_test.go` fails if a documented path is not routed. Key routes:

```
GET  /api/health · /api/workspace · /api/models · /api/openapi.json
GET|POST                 /api/projects
GET                      /api/projects/{ref}
GET|POST                 /api/projects/{ref}/sources
POST                     /api/project-import           · POST /api/project-import/preview
GET|POST                 /api/projects/{ref}/requirements
GET · PUT                /api/projects/{ref}/requirements/{id}          (+ /history, + /status)
GET                      /api/projects/{ref}/requirement-tests         (traceability)
GET|POST                 /api/projects/{ref}/agent-definitions         (+ /{name})
GET|POST                 /api/projects/{ref}/override-rules            (+ /{id}, + /{id}/status)
GET|POST                 /api/projects/{ref}/test-suites
GET|POST                 /api/projects/{ref}/artifacts
GET                      /api/projects/{ref}/items                     (unified tree)
GET                      /api/projects/{ref}/items/{family}/{itemRef}  (+ /history)
GET|POST                 /api/projects/{ref}/reports                   (append-only, JSON must parse)
GET                      /api/projects/{ref}/testability               (derived rollup)
GET|PUT                  /api/projects/{ref}/test-document-settings
POST                     /api/projects/{ref}/chat
```

### Browser UI (`web/ui/src`)

`pages/` (landing, project desktop), `components/` (Explorer, Overview, list
views, ChatDock, `viewers/` registry keyed by artifact type then media type,
`dialogs/` one form per entity), `lib/` (desktop tab model, theme, formatting).
One `styles.css` with CSS variables for both themes. The Markdown renderer builds
React elements directly — no `dangerouslySetInnerHTML`. `FileTree` is shared by
Explorer and file-family lists; folder state per project/family/view in
`sessionStorage`.

## 10. Configuration and secrets

`LOOMWORK_HOME` (default `~/.loomwork`) holds `config.json`, `presets.json`,
`projects/`. Provider endpoints may live in `config.json`; **credentials come
only from environment variables** (`AZURE_AI_API_KEY`, `AWS_ACCESS_KEY_ID` /
`AWS_SECRET_ACCESS_KEY` / `AWS_SESSION_TOKEN`, `LMSTUDIO_API_KEY`,
`CUENOTE_API_TOKEN`). Example configs in `config/*.example.json` carry
placeholders only; real configs are git-ignored. Secrets are never logged,
echoed in errors, or written into artifacts or project files.

## 11. Phase status (see `docs/ROADMAP.md`)

| Phase | Scope | State |
|---|---|---|
| 1 | Project dirs, document links, requirement CRUD + versioning | done (CLI + UI) |
| 2 | LLM document analysis, requirement extraction | done (CLI) |
| 3 | Agent definitions, override rules, one agent SDK, test generation | done (CLI; UI manages definitions/rules/import) |
| — | Spec-kit project import, requirement-ID aliasing, traceability, document suites | done (`re-eval`) |
| 4 | Execution contract (local + remote), report ingestion, HTML render | planned (UI already renders `reports/` JSON) |
| 5 | Run comparison (pass/fail, latency, structural body delta) + testability dashboard | rollup done; comparison planned |
