package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ilyaus/loomwork/internal/exec"
)

// BridgeEnvVar names the environment variable holding the path to the Claude
// Agent SDK bridge script.
const BridgeEnvVar = "LOOMWORK_CLAUDE_BRIDGE"

// DefaultBridgeScript is the bridge shipped with the repository, resolved
// relative to the working directory when the environment does not name one.
const DefaultBridgeScript = "bridge/claude-agent-bridge.mjs"

// DefaultClaudeModel is used when a session does not name a model.
const DefaultClaudeModel = "claude-sonnet-4-5"

// defaultBridgeEnvAllow is the environment the bridge may see. It is an
// allowlist rather than the parent environment because the bridge is a network
// client holding credentials.
var defaultBridgeEnvAllow = []string{
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_BASE_URL",
	"CLAUDE_CODE_OAUTH_TOKEN",
	"HOME",
	"PATH",
	"NODE_PATH",
	"NODE_OPTIONS",
	"SSL_CERT_FILE",
	"http_proxy",
	"https_proxy",
	"no_proxy",
}

// ClaudeAgentConfig configures the Claude Agent SDK adapter. The SDK is a
// TypeScript package, so it runs as a child process that speaks one JSON object
// per line over stdio; see bridge/claude-agent-bridge.mjs and
// docs/agent-bridge-protocol.md.
type ClaudeAgentConfig struct {
	// BridgeArgv is the bridge command. Empty means node with the script
	// named by LOOMWORK_CLAUDE_BRIDGE, or DefaultBridgeScript.
	BridgeArgv []string
	// WorkingDir is the bridge's working directory.
	WorkingDir string
	// EnvAllow overrides the environment allowlist.
	EnvAllow []string
	// ExtraEnv sets explicit NAME=value pairs for the bridge.
	ExtraEnv []string
	// DefaultModel is used when a session spec names no model.
	DefaultModel string
	// Timeout bounds the whole session, not one turn.
	Timeout time.Duration
}

func (c ClaudeAgentConfig) bridgeArgv() []string {
	if len(c.BridgeArgv) > 0 {
		return c.BridgeArgv
	}
	script := strings.TrimSpace(os.Getenv(BridgeEnvVar))
	if script == "" {
		script = DefaultBridgeScript
	}
	return []string{"node", script}
}

// ClaudeAgentAdapter is the AgentAdapter backed by the Claude Agent SDK bridge.
type ClaudeAgentAdapter struct {
	cfg ClaudeAgentConfig
}

// NewClaudeAgentAdapter builds the adapter. It does not start the bridge; a
// missing bridge or credential surfaces when a session starts.
func NewClaudeAgentAdapter(cfg ClaudeAgentConfig) *ClaudeAgentAdapter {
	return &ClaudeAgentAdapter{cfg: cfg}
}

// Name identifies the adapter.
func (a *ClaudeAgentAdapter) Name() string { return string(AgentKindClaude) }

// StartSession launches the bridge and completes the session handshake.
func (a *ClaudeAgentAdapter) StartSession(ctx context.Context, spec AgentSessionSpec) (AgentSession, error) {
	for _, tool := range spec.Tools {
		if err := tool.Validate(); err != nil {
			return nil, err
		}
	}
	model := strings.TrimSpace(spec.Model)
	if model == "" {
		model = strings.TrimSpace(a.cfg.DefaultModel)
	}
	if model == "" {
		model = DefaultClaudeModel
	}
	workingDir := spec.WorkingDir
	if workingDir == "" {
		workingDir = a.cfg.WorkingDir
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = a.cfg.Timeout
	}
	envAllow := a.cfg.EnvAllow
	if len(envAllow) == 0 {
		envAllow = defaultBridgeEnvAllow
	}

	process, err := exec.Start(ctx, exec.Command{
		Argv:     a.cfg.bridgeArgv(),
		Dir:      workingDir,
		Env:      envAllow,
		ExtraEnv: a.cfg.ExtraEnv,
		Timeout:  timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("claude agent bridge: %w", err)
	}

	session := &claudeSession{
		process: process,
		events:  make(chan AgentEvent, 256),
		tools:   map[string]ToolHandler{},
		ready:   make(chan bridgeEvent, 1),
		done:    make(chan struct{}),
	}
	tools := make([]bridgeTool, 0, len(spec.Tools))
	for _, tool := range spec.Tools {
		session.tools[tool.Name] = tool.Handler
		tools = append(tools, bridgeTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: normalizeToolSchema(tool.InputSchema),
		})
	}

	go session.readLoop()

	if err := session.write(bridgeRequest{
		Type: "start_session",
		Session: &bridgeSessionSpec{
			Model:        model,
			SystemPrompt: spec.SystemPrompt,
			MaxTurns:     spec.MaxTurns,
			Tools:        tools,
			Metadata:     spec.Metadata,
		},
	}); err != nil {
		_ = session.Close()
		return nil, err
	}

	select {
	case <-session.ready:
		return session, nil
	case <-session.done:
		err := session.exitError()
		_ = session.Close()
		return nil, fmt.Errorf("claude agent bridge exited before the session started: %w", err)
	case <-ctx.Done():
		_ = session.Close()
		return nil, ctx.Err()
	}
}

// normalizeToolSchema guarantees a schema the bridge can pass through: the
// object shape every backend expects, even when a caller supplied nothing.
func normalizeToolSchema(schema map[string]any) map[string]any {
	if len(schema) == 0 {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	normalized := make(map[string]any, len(schema)+1)
	for key, value := range schema {
		normalized[key] = value
	}
	if _, ok := normalized["type"]; !ok {
		normalized["type"] = "object"
	}
	return normalized
}

// bridgeRequest is one line written to the bridge.
type bridgeRequest struct {
	Type       string             `json:"type"`
	ID         string             `json:"id,omitempty"`
	Session    *bridgeSessionSpec `json:"session,omitempty"`
	Tool       *bridgeTool        `json:"tool,omitempty"`
	Prompt     string             `json:"prompt,omitempty"`
	Structured *bridgeStructured  `json:"structured_output,omitempty"`
	Content    string             `json:"content,omitempty"`
	IsError    bool               `json:"is_error,omitempty"`
}

type bridgeSessionSpec struct {
	Model        string            `json:"model"`
	SystemPrompt string            `json:"system_prompt,omitempty"`
	MaxTurns     int               `json:"max_turns,omitempty"`
	Tools        []bridgeTool      `json:"tools,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type bridgeTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type bridgeStructured struct {
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema,omitempty"`
}

// bridgeEvent is one line read from the bridge.
type bridgeEvent struct {
	Type       string          `json:"type"`
	ID         string          `json:"id,omitempty"`
	SessionID  string          `json:"session_id,omitempty"`
	Text       string          `json:"text,omitempty"`
	Name       string          `json:"name,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	Structured json.RawMessage `json:"structured,omitempty"`
	StopReason string          `json:"stop_reason,omitempty"`
	Usage      *bridgeUsage    `json:"usage,omitempty"`
	Message    string          `json:"message,omitempty"`
}

type bridgeUsage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

func (u bridgeUsage) usage() Usage {
	total := u.TotalTokens
	if total == 0 {
		total = u.InputTokens + u.OutputTokens
	}
	return Usage{PromptTokens: u.InputTokens, CompletionTokens: u.OutputTokens, TotalTokens: total}
}

// claudeSession is one bridge process. The reader goroutine owns the event
// stream and runs tool handlers inline: the bridge blocks until it gets a tool
// result, so nothing else can arrive meanwhile, and inline execution keeps event
// order identical to the backend's.
type claudeSession struct {
	process *exec.Process
	events  chan AgentEvent

	writeMu sync.Mutex
	turnMu  sync.Mutex

	mu      sync.Mutex
	tools   map[string]ToolHandler
	pending chan bridgeEvent
	closed  bool
	readErr error
	turns   int

	ready chan bridgeEvent
	done  chan struct{}

	closeOnce sync.Once
	closeErr  error
}

// Events returns the session's event stream. It is closed by Close.
func (s *claudeSession) Events() <-chan AgentEvent { return s.events }

// RegisterTool is rejected for this backend: the SDK builds its in-process tool
// server when the session starts, so a tool added later would be accepted here
// and then never offered to the model. Declare tools in AgentSessionSpec.Tools.
func (s *claudeSession) RegisterTool(tool ToolDefinition) error {
	if err := tool.Validate(); err != nil {
		return err
	}
	return fmt.Errorf("%w: tool %q must be declared in AgentSessionSpec.Tools", ErrLateToolRegistration, tool.Name)
}

// Send runs one turn. Turns are serialized: a session is a conversation, so a
// second concurrent prompt would interleave into the same transcript.
func (s *claudeSession) Send(ctx context.Context, req PromptRequest) (PromptResult, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return PromptResult{}, fmt.Errorf("agent prompt is required")
	}
	s.turnMu.Lock()
	defer s.turnMu.Unlock()

	prompt := req.Prompt
	var structured *bridgeStructured
	if req.Structured != nil {
		prompt += structuredOutputInstruction(*req.Structured)
		structured = &bridgeStructured{
			Name:        req.Structured.Name,
			Description: req.Structured.Description,
			Schema:      req.Structured.Schema,
		}
	}

	pending := make(chan bridgeEvent, 1)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return PromptResult{}, ErrSessionClosed
	}
	s.turns++
	id := fmt.Sprintf("turn-%d", s.turns)
	s.pending = pending
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.pending = nil
		s.mu.Unlock()
	}()

	if err := s.write(bridgeRequest{Type: "prompt", ID: id, Prompt: prompt, Structured: structured}); err != nil {
		return PromptResult{}, err
	}

	select {
	case event := <-pending:
		if event.Type == "error" {
			return PromptResult{}, fmt.Errorf("claude agent turn failed: %s", event.Message)
		}
		result := PromptResult{Text: event.Text, Structured: event.Structured, StopReason: event.StopReason}
		if event.Usage != nil {
			result.Usage = event.Usage.usage()
		}
		if req.Structured != nil && len(result.Structured) == 0 {
			value, err := extractJSONValue(result.Text)
			if err != nil {
				return PromptResult{}, fmt.Errorf("claude agent returned no structured output: %w", err)
			}
			result.Structured = value
		}
		return result, nil
	case <-s.done:
		return PromptResult{}, fmt.Errorf("claude agent bridge stopped mid-turn: %w", s.exitError())
	case <-ctx.Done():
		return PromptResult{}, ctx.Err()
	}
}

// Close ends the session, stops the bridge, and closes the event stream.
func (s *claudeSession) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		_ = s.write(bridgeRequest{Type: "close"})
		s.closeErr = s.process.Close()
		<-s.done
		close(s.events)
	})
	return s.closeErr
}

func (s *claudeSession) write(req bridgeRequest) error {
	line, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode bridge request: %w", err)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.process.WriteLine(string(line)); err != nil {
		return fmt.Errorf("claude agent bridge: %w", err)
	}
	return nil
}

// exitError describes why the reader stopped, for a caller waiting on a turn.
func (s *claudeSession) exitError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readErr != nil {
		return s.readErr
	}
	return fmt.Errorf("bridge closed its output (stderr: %s)", s.process.StderrTail())
}

func (s *claudeSession) readLoop() {
	defer close(s.done)
	for {
		line, err := s.process.ReadLine()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.setReadErr(err)
			}
			s.failPending(err)
			return
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event bridgeEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			s.emit(AgentEvent{Kind: AgentEventError, Err: fmt.Sprintf("bridge emitted a line that is not JSON: %s", truncate(line, 200))})
			continue
		}
		s.handle(event)
	}
}

func (s *claudeSession) handle(event bridgeEvent) {
	switch event.Type {
	case "ready", "session_started":
		s.emit(AgentEvent{Kind: AgentEventSessionStarted, Text: event.SessionID})
		select {
		case s.ready <- event:
		default:
		}
	case "text":
		s.emit(AgentEvent{Kind: AgentEventText, Text: event.Text})
	case "thinking":
		s.emit(AgentEvent{Kind: AgentEventThinking, Text: event.Text})
	case "tool_call":
		s.runTool(event)
	case "usage":
		if event.Usage != nil {
			usage := event.Usage.usage()
			s.emit(AgentEvent{Kind: AgentEventUsage, Usage: &usage})
		}
	case "turn_complete":
		s.emit(AgentEvent{Kind: AgentEventTurnComplete, Text: event.Text})
		s.deliver(event)
	case "error":
		s.emit(AgentEvent{Kind: AgentEventError, Err: event.Message})
		s.deliver(event)
	default:
		s.emit(AgentEvent{Kind: AgentEventError, Err: fmt.Sprintf("bridge emitted unknown event %q", event.Type)})
	}
}

func (s *claudeSession) runTool(event bridgeEvent) {
	s.emit(AgentEvent{Kind: AgentEventToolCall, ToolName: event.Name, ToolInput: event.Input})
	s.mu.Lock()
	handler, known := s.tools[event.Name]
	s.mu.Unlock()

	result := ToolResult{}
	switch {
	case !known:
		result = ToolResult{Content: fmt.Sprintf("tool %q is not registered with this session", event.Name), IsError: true}
	default:
		value, err := handler(context.Background(), event.Input)
		if err != nil {
			result = ToolResult{Content: fmt.Sprintf("tool %q failed: %v", event.Name, err), IsError: true}
		} else {
			result = value
		}
	}
	s.emit(AgentEvent{Kind: AgentEventToolResult, ToolName: event.Name, Result: &result})
	if err := s.write(bridgeRequest{Type: "tool_result", ID: event.ID, Content: result.Content, IsError: result.IsError}); err != nil {
		s.emit(AgentEvent{Kind: AgentEventError, Err: err.Error()})
	}
}

// deliver hands a turn-ending event to the waiting Send call.
func (s *claudeSession) deliver(event bridgeEvent) {
	s.mu.Lock()
	pending := s.pending
	s.mu.Unlock()
	if pending == nil {
		return
	}
	select {
	case pending <- event:
	default:
	}
}

func (s *claudeSession) setReadErr(err error) {
	s.mu.Lock()
	if s.readErr == nil {
		s.readErr = fmt.Errorf("claude agent bridge: %w", err)
	}
	s.mu.Unlock()
}

// failPending unblocks a waiting turn when the bridge goes away. Send also
// selects on done, so this is belt and braces for the ordinary case.
func (s *claudeSession) failPending(cause error) {
	message := "bridge closed its output"
	if cause != nil && !errors.Is(cause, io.EOF) {
		message = cause.Error()
	}
	s.deliver(bridgeEvent{Type: "error", Message: fmt.Sprintf("%s (stderr: %s)", message, s.process.StderrTail())})
}

// emit publishes an event without blocking: a caller that ignores Events must
// still get a working session, so a full buffer drops the event.
func (s *claudeSession) emit(event AgentEvent) {
	event.At = time.Now().UTC()
	select {
	case s.events <- event:
	default:
	}
}

func truncate(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max] + "..."
}
