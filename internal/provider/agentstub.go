package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// StubToolCall makes a scripted turn call a registered tool before it answers,
// so tool plumbing is exercised without a live SDK.
type StubToolCall struct {
	Name  string
	Input json.RawMessage
}

// StubTurn is one scripted reply. Text is returned verbatim; Structured, when
// empty and the caller asked for structured output, is extracted from Text
// exactly as a real adapter's fallback would.
type StubTurn struct {
	ToolCalls  []StubToolCall
	Text       string
	Structured json.RawMessage
	StopReason string
	Usage      Usage
	Err        error
}

// StubAgentAdapter is an in-process AgentAdapter that replays scripted turns. It
// exists so every code path above the provider layer — generation, the CLI,
// tests — can run with no SDK, no network, and no credentials.
type StubAgentAdapter struct {
	// AdapterName overrides the reported name.
	AdapterName string
	// Turns are replayed in order, one per Send.
	Turns []StubTurn
	// Respond, when set, takes precedence over Turns.
	Respond func(ctx context.Context, spec AgentSessionSpec, req PromptRequest) (PromptResult, error)

	mu       sync.Mutex
	sessions []*StubSession
}

// NewStubAgentAdapter builds a stub that replays the given turns.
func NewStubAgentAdapter(turns ...StubTurn) *StubAgentAdapter {
	return &StubAgentAdapter{Turns: turns}
}

// Name identifies the adapter.
func (a *StubAgentAdapter) Name() string {
	if strings.TrimSpace(a.AdapterName) != "" {
		return a.AdapterName
	}
	return string(AgentKindStub)
}

// StartSession opens a scripted session.
func (a *StubAgentAdapter) StartSession(ctx context.Context, spec AgentSessionSpec) (AgentSession, error) {
	for _, tool := range spec.Tools {
		if err := tool.Validate(); err != nil {
			return nil, err
		}
	}
	session := &StubSession{
		adapter: a,
		spec:    spec,
		tools:   map[string]ToolHandler{},
		events:  make(chan AgentEvent, 256),
	}
	for _, tool := range spec.Tools {
		session.tools[tool.Name] = tool.Handler
	}
	session.emit(AgentEvent{Kind: AgentEventSessionStarted, Text: a.Name()})
	a.mu.Lock()
	a.sessions = append(a.sessions, session)
	a.mu.Unlock()
	return session, nil
}

// Sessions returns every session the adapter has opened, for assertions.
func (a *StubAgentAdapter) Sessions() []*StubSession {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*StubSession(nil), a.sessions...)
}

// StubSession is a scripted AgentSession.
type StubSession struct {
	adapter *StubAgentAdapter
	spec    AgentSessionSpec
	events  chan AgentEvent

	mu      sync.Mutex
	tools   map[string]ToolHandler
	prompts []PromptRequest
	turn    int
	closed  bool
}

// Spec returns the spec the session was started with.
func (s *StubSession) Spec() AgentSessionSpec { return s.spec }

// Prompts returns every prompt the session received, for assertions.
func (s *StubSession) Prompts() []PromptRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]PromptRequest(nil), s.prompts...)
}

// RegisterTool records a tool.
func (s *StubSession) RegisterTool(tool ToolDefinition) error {
	if err := tool.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSessionClosed
	}
	s.tools[tool.Name] = tool.Handler
	return nil
}

// Events returns the session's event stream.
func (s *StubSession) Events() <-chan AgentEvent { return s.events }

// Send replays the next scripted turn.
func (s *StubSession) Send(ctx context.Context, req PromptRequest) (PromptResult, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return PromptResult{}, fmt.Errorf("agent prompt is required")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return PromptResult{}, ErrSessionClosed
	}
	s.prompts = append(s.prompts, req)
	index := s.turn
	s.turn++
	s.mu.Unlock()

	if s.adapter.Respond != nil {
		return s.adapter.Respond(ctx, s.spec, req)
	}
	if index >= len(s.adapter.Turns) {
		return PromptResult{}, fmt.Errorf("stub agent has no scripted turn %d", index+1)
	}
	turn := s.adapter.Turns[index]
	for _, call := range turn.ToolCalls {
		s.emit(AgentEvent{Kind: AgentEventToolCall, ToolName: call.Name, ToolInput: call.Input})
		s.mu.Lock()
		handler, known := s.tools[call.Name]
		s.mu.Unlock()
		if !known {
			return PromptResult{}, fmt.Errorf("stub agent called unregistered tool %q", call.Name)
		}
		result, err := handler(ctx, call.Input)
		if err != nil {
			return PromptResult{}, fmt.Errorf("stub agent tool %q failed: %w", call.Name, err)
		}
		s.emit(AgentEvent{Kind: AgentEventToolResult, ToolName: call.Name, Result: &result})
	}
	if turn.Err != nil {
		s.emit(AgentEvent{Kind: AgentEventError, Err: turn.Err.Error()})
		return PromptResult{}, turn.Err
	}
	s.emit(AgentEvent{Kind: AgentEventText, Text: turn.Text})
	s.emit(AgentEvent{Kind: AgentEventTurnComplete, Text: turn.Text})

	result := PromptResult{Text: turn.Text, Structured: turn.Structured, StopReason: turn.StopReason, Usage: turn.Usage}
	if req.Structured != nil && len(result.Structured) == 0 {
		value, err := extractJSONValue(result.Text)
		if err != nil {
			return PromptResult{}, fmt.Errorf("stub agent returned no structured output: %w", err)
		}
		result.Structured = value
	}
	return result, nil
}

// Close ends the scripted session.
func (s *StubSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.events)
	return nil
}

func (s *StubSession) emit(event AgentEvent) {
	select {
	case s.events <- event:
	default:
	}
}
