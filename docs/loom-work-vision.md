## Overview

Loom-work is a single-user, local-first, browser-based QA workbench that organizes documentation, requirements, agent-generated test suites, and execution reports without itself executing tests. It uses pluggable LLM/agent providers (Azure Foundry, AWS Bedrock, Ollama, Claude Agent SDK, Copilot SDK) to analyze service documentation, surface gaps, and generate REST API test suites governed by versioned agent-definition files and override rules. This document translates the confirmed product vision into a phased functional and technical specification suitable for handoff to an implementation agent (e.g., Devin, Claude).

## Guiding Principles

- **Non-executing control plane**: Loom-work stores, organizes, versions, and displays. It never runs tests directly; execution is delegated to a local executable or remote service via a defined contract.
- **Local-first, no DB (initially)**: Single QA engineer, filesystem/directory-based project storage, with optional S3 sync for artifacts. A directory-of-projects model replaces multi-tenant database concerns.
- **Provider-agnostic agent layer**: LLM/agent access abstracted behind one internal interface so Azure Foundry, Bedrock, Ollama, Claude Agent SDK, and Copilot SDK are interchangeable backends, not hardcoded integrations.
- **Traceability as the core value**: Every test case links to one or more requirements; every requirement links to its official source (ADO, Confluence, GitHub); every test run links to a versioned test suite.
- **Builder vs. runtime agents are distinct concerns**: Devin (or Claude) is used only to *build* Loom-work. Claude Agent SDK / Copilot SDK are used *inside* Loom-work at runtime for document analysis and test generation. These must not be conflated in implementation instructions.

## System Architecture

```
Browser (Frontend UI)
        |
Backend API service (local process)
        |
   ┌────┴─────────────────────────────┐
   |                                  |
Project Store                   Agent Adapter Layer
(filesystem, per-project        (Claude Agent SDK, Copilot SDK,
 directory, optional S3 sync)    Azure Foundry, Bedrock, Ollama)
   |                                  |
Artifacts:                      Agent inputs:
- doc links/copies               - Agent definition MD (versioned)
- requirements (versioned)       - Test templates
- agent definition files         - Swagger/OpenAPI spec
- test templates                 - Custom override rules
- test suites (versioned)        - Service requirements
- execution reports (JSON)
        |
Test Execution Contract
   |                    |
Local Executor      Remote Executor
(filesystem I/O)    (S3 upload + poll/status API)
```

## Functional Modules

### 1. Project Management

A project is a directory on disk (or synced S3 prefix), not a database record. Each project directory contains subfolders for documents, requirements, agent-definitions, test-suites, and reports.

- Create new project: name, description, initial document/source links.
- Load existing project by pointing at its directory.
- View a "directory of projects" landing screen showing status summary per project (last tested date, requirement coverage %, open gaps count) without needing all projects loaded simultaneously.
- Switch between projects one at a time (single active project context); no concurrent multi-project editing session is required.
- No relational database dependency at this stage; project state is derived by scanning the directory structure and reading versioned JSON/MD files. A lightweight per-project index file (e.g., `project.json`) can cache metadata to avoid full directory scans on load.

### 2. Documentation & Requirements Management

**Document sources**: stored as links (GitHub, Confluence, Azure ADO, etc.) plus optional local/S3 copies of artifact files (PDFs, exported pages, spec files).

**Requirements** are a first-class, tester-facing artifact distinct from raw source documents:

- Requirements are written in tester-friendly language, extracted or authored from source documentation, and each carries a back-reference to its original source (ADO story ID/link, Confluence page, GitHub doc).
- Requirements are versioned internally (v1, v2, ...) — no diff view is required initially, but each version is a discrete, retrievable snapshot so a requirement that changes or is clarified does not silently overwrite history.
- Requirements can become obsolete; an explicit status field (`active`, `obsolete`, `superseded`) should be tracked, with obsolete requirements retained for audit rather than deleted.
- Requirements can be entered directly by a QA engineer (bypassing LLM extraction) or generated via LLM/agent document analysis — both paths write to the same requirement store and schema.

**Document analysis (LLM/agent-driven)**:

- Produces a QA-readiness assessment: whether the service/spec is sufficiently defined for functional/integration testing, whether requirements and API spec are in sync.
- Produces a list of open questions and gaps requiring QA/engineering follow-up.
- This analysis can alternatively be authored manually and imported, since QA engineers may perform this work outside Loom-work.

### 3. Agent Definition & Override Rules Management

Agent definition files (Markdown) are versioned configuration artifacts, not just prompts — they encode how an LLM/agent should reason about a given test-generation task (e.g., "REST API test generation agent").

**Override rules are the most critical and highest-risk component of the system.** They must let the agent *reason*, not just pattern-match, about cases where custom business rules take precedence over what the Swagger/OpenAPI spec literally states. Two concrete confirmed examples:

- A `GET` for a non-existent item returns an empty list (HTTP 200) rather than `404`, even though a naive reading of the spec might suggest `404` is expected — the override rule must instruct the agent to treat this as the correct, intended behavior rather than a defect.
- "Do not test missing-authentication cases" — because authentication is out of scope for this service's test responsibility, even if the spec technically defines auth-related responses.

Recommended structure for an agent definition file:

```markdown
---
agent_name: rest-api-test-generator
version: v3
target: claude-agent-sdk
tools_allowed: [read_swagger, read_requirements, read_override_rules, write_test_file, run_validator]
---

# Role
You generate REST API test suites from a Swagger/OpenAPI spec, requirements,
and override rules. Override rules take precedence over the literal spec
whenever they conflict.

# Override Rule Handling
- Treat each override rule as a correction to expected behavior, not a
  suggestion. State explicitly, per generated test, which rule (if any)
  it depended on.
- If the spec and an override rule conflict, follow the override rule and
  annotate the test with a comment referencing the rule ID.
- If no override rule addresses a given ambiguity, default to the spec and
  flag the ambiguity as an open question rather than guessing silently.

# Inputs
- swagger_spec_path
- requirements[] (versioned, with ADO/Confluence references)
- override_rules[] (versioned)
- test_templates[]
```

Versioning for agent definitions and override rules is internal (v1, v2, v3...) with no diff requirement at this stage — each version is stored as a discrete file (`rest-api-test-generator.v3.md`), and the "current" version is a pointer/symlink or a `current.json` manifest entry.

To reduce silent misapplication of override rules, every generated test case should carry a machine-readable annotation (e.g., `overrides_applied: ["auth-not-tested-v1", "empty-list-on-missing-v2"]`) so a QA engineer can audit which rules shaped which tests without re-reading agent reasoning transcripts.

### 4. Test Generation

- Agents (Claude Agent SDK or Copilot SDK) receive: Swagger/OpenAPI spec, requirements (current version), override rules (current version), test templates, and validator executables.
- Generated test suites are validated against provided executable validators before being considered a candidate version.
- Test suites are versioned (v1, v2, ...) with an explicit "latest/current" designation; older versions remain retrievable but are not the default execution target.
- Pre-existing, externally created tests can be imported directly into a project's test-suite store, bypassing generation, and are versioned the same way as agent-generated ones.
- Every test case must declare links to one or more requirement IDs; a test suite with unlinked test cases should be flagged as incomplete in the UI rather than silently accepted.

### 5. Test Execution Contract

Loom-work never executes tests; it hands off to an executor and receives structured results back.

| Execution Mode | Trigger Flow | Report Retrieval |
|---|---|---|
| Local executor | Loom-work passes the test suite (or its local path) plus runner configuration directly to the local executable | Local executor writes report files and returns their filesystem location to Loom-work |
| Remote executor | Loom-work uploads the test suite to S3, then calls the remote "run" API with the S3 location and runner configuration | Loom-work polls the remote service for run status; completed reports are returned in the poll response and stored locally/S3 alongside the test suite version |

Configuration for the executor (runner location, parameters, auth/token provider settings, target environment) is itself a versioned artifact per project, separate from the test suite content, so the same tests can be run against different environments without duplicating test files.

Each execution produces one JSON report, retained indefinitely against its test-suite version — a single suite accumulates many run records over time, so reports are stored as an append-only list keyed by `(suite_version, run_timestamp)`, not overwritten.

### 6. Reporting & Comparison

- Raw JSON execution reports are rendered as user-friendly HTML documents within Loom-work (pass/fail counts, per-test status, latency per test).
- **Run-to-run and environment-to-environment comparison** (e.g., QA vs. PROD, or two historical runs of the same suite version) must support:
  - Pass/fail status deltas per test case.
  - Latency comparison per test case across runs.
  - **Response body structural comparison**: fields missing in one run's response vs. the other, and fields present but unexpected (extra) — structural presence/absence only, not value-level diffing, per the confirmed requirement.
- Reports should be linkable back to the requirements exercised by the underlying test suite, enabling the testability status view described below.

### 7. Testability Status Dashboard

For each project, Loom-work maintains a derived status view (not necessarily a stored fact — it can be computed from existing artifacts on load):

- Last-tested timestamp and outcome summary for the current test suite version.
- List of requirements covered by at least one test case in the latest run.
- List of requirements with zero linked, executed test cases (untested/gap list).
- Optional rollup at the project-directory level so the "directory of projects" landing view can show each project's testability health at a glance.

## Data Model (File-Based)

Since the system avoids a database initially, each entity is a file or small set of files within the project directory. Suggested layout:

```
/projects/<project-id>/
  project.json                 # name, description, doc source links, index cache
  requirements/
    req-001.v1.json
    req-001.v2.json            # superseded by v2; v1 retained, status updated
    index.json                 # current version pointer per requirement id
  agent-definitions/
    rest-api-test-generator.v1.md
    rest-api-test-generator.v3.md
    override-rules.v2.md
    current.json                # pointer to active version per agent
  test-suites/
    suite-orders-api/
      v1/tests/...
      v2/tests/...
      current.json
  executor-config/
    local.json
    remote.json
  reports/
    suite-orders-api/
      v2/
        2026-08-10T14-00Z.json
        2026-08-15T09-30Z.json
```

Minimal recommended fields per entity:

| Entity | Key Fields |
|---|---|
| Requirement | id, version, text (tester-friendly), source_type (ADO/Confluence/GitHub), source_ref, status (active/obsolete/superseded) |
| Agent definition | agent_name, version, target_provider, tools_allowed, body (markdown) |
| Test case | id, requirement_ids[], overrides_applied[], request definition, expected outcome |
| Execution report | suite_id, suite_version, run_timestamp, executor_mode (local/remote), results[], latency_ms per test |
| Executor config | mode (local/remote), runner_location_or_endpoint, parameters, auth/token settings |

## Provider & Agent Adapter Layer

A single internal interface should mediate all LLM/agent calls so Azure Foundry, Bedrock, Ollama, Claude Agent SDK, and Copilot SDK are swappable without touching business logic:

```ts
interface AgentAdapter {
  startSession(config: AgentDefinition): SessionHandle;
  sendPrompt(session: SessionHandle, input: AgentInput): Promise<AgentOutput>;
  registerTool(session: SessionHandle, tool: ToolSpec): void;
  streamEvents(session: SessionHandle): AsyncIterable<AgentEvent>;
}
```

Claude Agent SDK and Copilot SDK differ in tool-calling and session semantics, so the adapter should normalize tool registration and structured output rather than exposing provider-specific quirks to the rest of Loom-work.

## Phased Build Plan

| Phase | Scope | Key Deliverable |
|---|---|---|
| 1 | Project directory management, document links/artifact storage, requirement CRUD with versioning | Working project shell, no LLM calls yet |
| 2 | LLM-driven document analysis (single provider first) producing gap/question lists and requirement extraction | Manual-import path for externally produced analysis |
| 3 | Agent definition file management + override-rule schema + one agent SDK integration (recommend Claude Agent SDK first) for REST API test generation | Generated + validated test suite, versioned |
| 4 | Execution contract: local executor integration, remote executor S3 upload + polling, JSON report ingestion | HTML report rendering |
| 5 | Run comparison (pass/fail delta, latency delta, structural body diff) and testability status dashboard | Directory-of-projects landing view with per-project health |

Each phase should ship with its own fixed JSON schema for its core artifact (agent definition, test case, execution report, requirement) drafted and reviewed *before* implementation begins, since these schemas are shared contracts between the frontend, backend, and any agent/executor integration.

## Open Design Questions for Implementation

- Exact schema for `override_rules` — should a rule be free-text guidance consumed by the agent's reasoning, or a structured condition/action pair the adapter can also apply deterministically as a post-generation check? A hybrid (structured metadata + free-text rationale) is recommended so both agent reasoning and automated auditing are possible.
- Whether requirement version pointers use a simple "current.json" manifest (as sketched above) or git-based versioning for free history/diffing later, even though diffing is not required now — git costs little extra and preserves an upgrade path.
- How S3 sync reconciles with the "no DB" constraint if two devices touch the same project directory — out of scope for a single-user local-first v1 but worth flagging as a v2 concern.