# Loomwork Constitution

## Core Principles

## 1. Core Purpose
Loomwork is a local-first orchestrator for working with Large Language Models over
project artifacts. A project holds artifacts (specs, logs, test results, diagrams,
docs, generated content); prompts are executed against those artifacts through
pluggable model providers (local Ollama / LM Studio, remote Azure AI Foundry / AWS
Bedrock), and every result is stored back as a new, versioned artifact. The
orchestrator owns the project/artifact lifecycle and provider selection; it never
owns model-specific logic.

## 2. Technical Commandments
* **Language Target:** Pure Go (Go 1.21+).
* **Zero Native C Dependencies:** External packages must be pure Go. No CGO or
  native binary dependencies allowed, so a single static binary runs on any
  developer workstation, container, or Lambda-style runtime.
* **Standard Library Priority:** Leverage Go standard library packages
  (`net/http`, `encoding/json`, `sync`, `context`, `flag`) wherever possible.
  A third-party dependency must be justified by a capability the standard
  library cannot provide.
* **Error Handling Paradigm:** No `panic()` recovery patterns inside business
  logic. Every error is wrapped with rich diagnostic context (provider, model,
  project, artifact) and bubbled up to the orchestrator layer.
* **Concurrency & Safety:** Any shared mutable state (artifact stores, preset
  registries, in-memory stubs) must be protected via `sync.RWMutex` or be
  structurally immutable, guaranteeing absolute thread safety.
* **Secrets Discipline:** Credentials and endpoints are supplied via environment
  variables or an untracked local config file. No secret, endpoint of a private
  deployment, or API key is ever committed.

## 3. Code Generation & Architecture Standards
* **Test-Driven Foundation:** Every domain rule, provider request/response
  mapping, preset validator, and client contract must be accompanied by native
  Go unit tests (`*_test.go`). HTTP adapters are tested against
  `net/http/httptest` servers, never against live services.
* **Decoupled Interfaces:** The domain layer (`internal/model`) must know nothing
  about HTTP, providers, or storage. Providers must know nothing about projects
  or artifacts: they consume and return structured request/response values only.
  Orchestration wires the two together.
* **One Interface, Many Adapters:** A capability (text generation, image
  generation, note storage) is defined by exactly one Go interface. Adding a new
  backend means adding an adapter, never changing a caller. Incomplete adapters
  ship behind the same interface with explicit, typed "not implemented" errors —
  never with a silently different shape.
* **Registry over Conditionals:** Provider selection and per-model parameter
  defaults are resolved through data-driven registries loaded from config, not
  through `switch` statements sprinkled across call sites.
* **Vertical Slices:** Each session lands a runnable end-to-end path
  (`make build` produces a working binary) rather than horizontal layers that
  cannot be exercised.
* **Deferred Work Is Declared:** Features that are intentionally out of scope are
  recorded in `docs/INTENT.md` with an explicit implemented/deferred marker, plus
  the extension point that will host them.
