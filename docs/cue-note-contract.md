# cue-note — Assumed API Contract

**Status: ASSUMED.** At the time of writing, [`ilyaus/cue-note`](https://github.com/ilyaus/cue-note)
contains no API implementation, so Loomwork's HTTP client (`internal/cuenote/http.go`)
targets the contract below. It is a design proposal, not a verified interface.
The in-memory implementation (`internal/cuenote/memory.go`) satisfies the same Go
interface, so the foundation and all tests run without cue-note.

When cue-note ships, reconcile it with this document; only
`internal/cuenote/http.go` and this file should need to change — callers depend on
the `cuenote.Client` interface, not on the wire format.

## Conventions

- Base URL: configured (`cuenote.baseUrl`), default `http://localhost:8090`.
- All bodies are JSON; all timestamps are RFC 3339 UTC.
- Auth: optional `Authorization: Bearer <token>`, token read from the
  `CUENOTE_API_TOKEN` environment variable only.
- `404` maps to `cuenote.ErrNotFound`; any other non-2xx becomes an error
  carrying the status code and a truncated body.

## Resources

### Cue — a reusable, optionally templated prompt

```json
{
  "id": "cue-0001",
  "name": "spec-review",
  "body": "Review the following spec for gaps:\n\n{{spec}}",
  "tags": ["review", "spec"],
  "variables": ["spec"],
  "metadata": {"author": "ilyaus"},
  "createdAt": "2026-01-01T00:00:00Z",
  "updatedAt": "2026-01-01T00:00:00Z"
}
```

`variables` are the `{{name}}` placeholders in `body`. Loomwork derives them
locally when the service omits them; rendering with a missing variable is an
error, never an empty substitution.

### Note — free-form text, optionally scoped to a project

```json
{
  "id": "note-0001",
  "projectId": "prj-8f...",
  "title": "Ollama tuning observations",
  "body": "qwen3:8b at temperature 0.1 produced the most stable diffs.",
  "tags": ["tuning"],
  "metadata": {},
  "createdAt": "2026-01-01T00:00:00Z",
  "updatedAt": "2026-01-01T00:00:00Z"
}
```

## Endpoints

| Method & path | Purpose | Query / body | Response |
|---|---|---|---|
| `GET /api/v1/cues` | list cues | `tag` (repeatable), `q` (substring search), `limit` | `{"cues": [Cue]}` |
| `GET /api/v1/cues/{id}` | fetch one cue | — | `Cue` |
| `POST /api/v1/cues` | create a cue | `Cue` (id/timestamps server-assigned) | created `Cue` |
| `GET /api/v1/notes` | list notes | `projectId`, `tag` (repeatable), `q`, `limit` | `{"notes": [Note]}` |
| `GET /api/v1/notes/{id}` | fetch one note | — | `Note` |
| `POST /api/v1/notes` | create a note | `Note` | created `Note` |

## Deliberate omissions

Update, delete, pagination cursors, and cue versioning are not part of the
assumed contract: Loomwork's foundation does not need them, and guessing their
shape would create migration debt. Add them to the interface when cue-note
defines them.
