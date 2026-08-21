# Agent bridge protocol

Loomwork's stateful agent abstraction is `provider.AgentAdapter`. The first
backend is the Claude Agent SDK, which is a TypeScript package, so it runs as a
child process (`bridge/claude-agent-bridge.mjs`) launched through
`internal/exec.Start`: argv only, no shell, an environment allowlist, and a
bounded lifetime.

The transport is one JSON object per line in each direction. `internal/provider/claudeagent.go`
is the only client; any other bridge (a different SDK, a sidecar) can be dropped
in by speaking the same protocol.

## Setup

```bash
cd bridge && npm install
export ANTHROPIC_API_KEY=...          # or CLAUDE_CODE_OAUTH_TOKEN
export LOOMWORK_CLAUDE_BRIDGE=$PWD/claude-agent-bridge.mjs   # optional
```

Only the variables in `defaultBridgeEnvAllow` reach the bridge; nothing else from
the parent environment is inherited.

## Host to bridge

| Type | Fields | Meaning |
| --- | --- | --- |
| `start_session` | `session.model`, `session.system_prompt`, `session.max_turns`, `session.tools[]`, `session.metadata` | Open the session. Tools are declared here because the SDK builds its in-process tool server at session start. |
| `prompt` | `id`, `prompt`, `structured_output` | Run one turn. `id` correlates the completion event. |
| `tool_result` | `id`, `content`, `is_error` | Answer a `tool_call`. |
| `register_tool` | `tool` | Declare a tool before the session starts. `claudeSession.RegisterTool` returns `ErrLateToolRegistration` instead of sending this after start. |
| `close` | — | End the session. |

A tool is `{name, description, input_schema}`. The bridge passes `input_schema`
to the model as text in the tool description: the schema is runtime data from Go,
and deriving a second Zod copy of it would give a tool two notions of what it
accepts. Validation stays in the host handler.

## Bridge to host

| Type | Fields | Normalized as |
| --- | --- | --- |
| `ready` | — | `AgentEventSessionStarted` (handshake) |
| `session_started` | `session_id` | `AgentEventSessionStarted` |
| `text` | `text` | `AgentEventText` |
| `thinking` | `text` | `AgentEventThinking` |
| `tool_call` | `id`, `name`, `input` | `AgentEventToolCall`, then the handler runs and a `tool_result` is written back |
| `usage` | `usage.input_tokens`, `usage.output_tokens` | `AgentEventUsage` |
| `turn_complete` | `id`, `text`, `structured`, `stop_reason`, `usage` | ends the waiting `Send` |
| `error` | `id`, `message` | `AgentEventError`, and fails the waiting `Send` |

## Structured output

`PromptRequest.Structured` is normalized rather than delegated: the host appends
one schema instruction to the prompt and, when the bridge reports no `structured`
value, extracts the outermost JSON value from the turn text. A caller therefore
gets the same guarantee — a single JSON value or a failed turn — regardless of
whether a backend enforces schemas natively.

## Tool authority

The only tools the model can call are the host tools Loomwork registers at
session start, exposed through an in-process MCP server and pre-approved via
`allowedTools`. The SDK's own tools — `Bash`, `Read`, `Write`, `Edit`, `Glob`,
`Grep`, `WebFetch`, `WebSearch`, `Task` and friends — are listed in
`disallowedTools`, so a spec or requirement that tries to talk to the agent
cannot reach the filesystem, a shell, or the network. Permission mode stays at
`default`: pre-approval covers everything a generation run legitimately needs,
so no prompt can block a non-interactive session.

## Failure behavior

- A bridge that exits mid-turn fails the in-flight `Send` with the process's
  stderr tail rather than hanging.
- A non-JSON or unknown line becomes an `AgentEventError` and the session
  continues; the bridge is not trusted to be well behaved.
- Events are delivered without blocking: a caller that ignores `Events()` still
  gets working turns, and a full buffer drops events instead of stalling the
  session.
