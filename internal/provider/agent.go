// Agent adapters are the stateful half of the provider abstraction. Where
// TextGenerator is one stateless call, an agent SDK owns a conversation that
// spans turns and calls back into the host to run tools. Both live behind this
// package so callers keep talking to interfaces, never to a backend.

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AgentKind identifies an agent adapter backend.
type AgentKind string

const (
	// AgentKindClaude is the Claude Agent SDK, reached through a stdio JSON
	// bridge because the SDK is not Go-native.
	AgentKindClaude AgentKind = "claude-agent-sdk"
	// AgentKindStub is the in-process scripted adapter used by tests and by
	// dry runs, so no path requires a live SDK.
	AgentKindStub AgentKind = "stub"
)

// AgentKinds lists every supported agent adapter kind.
func AgentKinds() []AgentKind {
	return []AgentKind{AgentKindClaude, AgentKindStub}
}

// ParseAgentKind validates a raw agent adapter kind.
func ParseAgentKind(raw string) (AgentKind, error) {
	candidate := AgentKind(strings.TrimSpace(strings.ToLower(raw)))
	for _, known := range AgentKinds() {
		if candidate == known {
			return known, nil
		}
	}
	return "", fmt.Errorf("unknown agent adapter %q: supported adapters are %s", raw, joinAgentKinds(AgentKinds()))
}

func joinAgentKinds(kinds []AgentKind) string {
	names := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		names = append(names, string(kind))
	}
	return strings.Join(names, ", ")
}

// ErrSessionClosed is returned when a closed session is used again.
var ErrSessionClosed = errors.New("agent session is closed")

// ErrLateToolRegistration is returned by a backend that can only accept tools
// when a session starts. Callers detect it with errors.Is and declare the tool in
// AgentSessionSpec.Tools instead.
var ErrLateToolRegistration = errors.New("agent backend requires tools at session start")

// ToolResult is what a host tool hands back to the agent. IsError marks a
// failure the agent may recover from; it is not a transport error.
type ToolResult struct {
	Content string `json:"content"`
	IsError bool   `json:"is_error,omitempty"`
}

// ToolHandler runs a registered tool. Input is the raw JSON the agent produced
// for the tool's input schema. Returning an error ends the turn; returning a
// ToolResult with IsError lets the agent try something else.
type ToolHandler func(ctx context.Context, input json.RawMessage) (ToolResult, error)

// ToolDefinition is a host tool in normalized form. Every backend receives the
// same name, description, and JSON Schema; each adapter is responsible for
// translating that into its own tool-registration shape, so an SDK's quirks
// (naming rules, schema dialect, "input_schema" versus "parameters") never reach
// a caller.
type ToolDefinition struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     ToolHandler
}

// Validate checks a tool is registerable.
func (t ToolDefinition) Validate() error {
	if strings.TrimSpace(t.Name) == "" {
		return errors.New("tool name is required")
	}
	if strings.TrimSpace(t.Description) == "" {
		return fmt.Errorf("tool %q needs a description: it is the only thing the model reads to decide when to call it", t.Name)
	}
	if t.Handler == nil {
		return fmt.Errorf("tool %q needs a handler", t.Name)
	}
	return nil
}

// StructuredOutput asks for a JSON result matching a schema. Backends differ:
// some enforce a schema natively, others only obey an instruction. Adapters
// normalize both to the same guarantee — PromptResult.Structured holds a single
// JSON value or the turn fails — so callers never branch on backend support.
type StructuredOutput struct {
	Name        string
	Description string
	Schema      map[string]any
}

// AgentSessionSpec starts a session. SystemPrompt is where an agent definition's
// markdown body goes.
type AgentSessionSpec struct {
	Model        string
	SystemPrompt string
	Tools        []ToolDefinition
	MaxTurns     int
	WorkingDir   string
	Timeout      time.Duration
	Metadata     map[string]string
}

// AgentEventKind classifies a streamed session event.
type AgentEventKind string

const (
	AgentEventSessionStarted AgentEventKind = "session-started"
	AgentEventText           AgentEventKind = "text"
	AgentEventThinking       AgentEventKind = "thinking"
	AgentEventToolCall       AgentEventKind = "tool-call"
	AgentEventToolResult     AgentEventKind = "tool-result"
	AgentEventUsage          AgentEventKind = "usage"
	AgentEventTurnComplete   AgentEventKind = "turn-complete"
	AgentEventError          AgentEventKind = "error"
)

// AgentEvent is one normalized event from a session. The same event set covers
// every backend; an adapter drops what its backend does not report rather than
// inventing a backend-specific event.
type AgentEvent struct {
	Kind      AgentEventKind  `json:"kind"`
	Text      string          `json:"text,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
	ToolInput json.RawMessage `json:"tool_input,omitempty"`
	Result    *ToolResult     `json:"result,omitempty"`
	Usage     *Usage          `json:"usage,omitempty"`
	Err       string          `json:"error,omitempty"`
	At        time.Time       `json:"at"`
}

// PromptRequest is one turn of a session.
type PromptRequest struct {
	Prompt string
	// Structured, when set, requires the turn to end with a JSON value
	// matching the schema.
	Structured *StructuredOutput
}

// PromptResult is the outcome of one turn.
type PromptResult struct {
	Text       string          `json:"text"`
	Structured json.RawMessage `json:"structured,omitempty"`
	StopReason string          `json:"stop_reason,omitempty"`
	Usage      Usage           `json:"usage,omitempty"`
}

// AgentSession is a live conversation with an agent backend.
//
// Events streams normalized events for the life of the session and is closed by
// Close; a caller that does not drain it must still get a working session, so
// adapters must not block on delivery.
type AgentSession interface {
	// RegisterTool adds a host tool to a running session. A backend that can
	// only take tools at session start returns ErrLateToolRegistration rather
	// than accepting a tool it will never offer to the model.
	RegisterTool(tool ToolDefinition) error
	// Send runs one turn and waits for it to finish.
	Send(ctx context.Context, req PromptRequest) (PromptResult, error)
	// Events is the session's event stream.
	Events() <-chan AgentEvent
	// Close ends the session and releases the backend.
	Close() error
}

// AgentAdapter is the single stateful agent interface every agent backend
// implements. It is deliberately separate from TextGenerator: a stateless
// completion and a tool-using session have different lifetimes, so merging them
// would force one to pretend to be the other.
type AgentAdapter interface {
	Name() string
	StartSession(ctx context.Context, spec AgentSessionSpec) (AgentSession, error)
}

// structuredOutputInstruction is the normalization fallback for backends that
// cannot enforce a schema: the same wording is appended for all of them, so the
// caller's contract does not change with the backend.
func structuredOutputInstruction(output StructuredOutput) string {
	schema, err := json.MarshalIndent(output.Schema, "", "  ")
	if err != nil {
		schema = []byte("{}")
	}
	var builder strings.Builder
	builder.WriteString("\n\nRespond with a single JSON value and nothing else: no prose, no code fence.")
	if strings.TrimSpace(output.Description) != "" {
		builder.WriteString(" ")
		builder.WriteString(strings.TrimSpace(output.Description))
	}
	builder.WriteString("\nIt must validate against this JSON Schema:\n")
	builder.Write(schema)
	return builder.String()
}

// extractJSONValue pulls the outermost JSON object or array out of model text,
// tolerating code fences and prose. It is the second half of the structured
// output fallback.
func extractJSONValue(text string) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(text)
	start := strings.IndexAny(trimmed, "{[")
	if start < 0 {
		return nil, fmt.Errorf("response contains no JSON value")
	}
	open := trimmed[start]
	closer := byte('}')
	if open == '[' {
		closer = ']'
	}
	end := strings.LastIndexByte(trimmed, closer)
	if end < start {
		return nil, fmt.Errorf("response contains no complete JSON value")
	}
	candidate := json.RawMessage(trimmed[start : end+1])
	if !json.Valid(candidate) {
		return nil, fmt.Errorf("response is not valid JSON")
	}
	return candidate, nil
}
