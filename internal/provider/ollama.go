package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// Ollama talks to a local Ollama server over its native HTTP API.
type Ollama struct {
	baseURL      string
	defaultModel string
	client       *http.Client
}

// NewOllama builds an Ollama adapter. With an empty BaseURL it targets the
// standard local endpoint.
func NewOllama(cfg Config) *Ollama {
	return &Ollama{
		baseURL:      firstNonEmpty(cfg.BaseURL, DefaultOllamaBaseURL),
		defaultModel: cfg.DefaultModel,
		client:       newHTTPClient(cfg.Timeout()),
	}
}

// Name identifies the adapter.
func (o *Ollama) Name() string { return string(KindOllama) }

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  map[string]any  `json:"options,omitempty"`
}

type ollamaChatResponse struct {
	Model              string        `json:"model"`
	Message            ollamaMessage `json:"message"`
	Done               bool          `json:"done"`
	DoneReason         string        `json:"done_reason"`
	PromptEvalCount    int           `json:"prompt_eval_count"`
	EvalCount          int           `json:"eval_count"`
	TotalDurationNanos int64         `json:"total_duration"`
}

type ollamaTagsResponse struct {
	Models []struct {
		Name  string `json:"name"`
		Model string `json:"model"`
	} `json:"models"`
}

// Generate performs a non-streaming chat completion.
func (o *Ollama) Generate(ctx context.Context, req Request) (Response, error) {
	req.Model = firstNonEmpty(req.Model, o.defaultModel)
	if err := req.Validate(); err != nil {
		return Response{}, fmt.Errorf("ollama: %w", err)
	}

	payload := ollamaChatRequest{
		Model:    req.Model,
		Messages: buildMessages(req),
		Stream:   false,
		Options:  ollamaOptions(req.Params),
	}

	var decoded ollamaChatResponse
	url := joinURL(o.baseURL, "/api/chat")
	if err := postJSON(ctx, o.client, url, nil, payload, &decoded); err != nil {
		return Response{}, fmt.Errorf("ollama generate (model %s): %w", req.Model, err)
	}
	if strings.TrimSpace(decoded.Message.Content) == "" {
		return Response{}, fmt.Errorf("ollama generate (model %s): empty completion from %s", req.Model, url)
	}

	return Response{
		Text:         decoded.Message.Content,
		Model:        firstNonEmpty(decoded.Model, req.Model),
		FinishReason: decoded.DoneReason,
		Usage: Usage{
			PromptTokens:     decoded.PromptEvalCount,
			CompletionTokens: decoded.EvalCount,
			TotalTokens:      decoded.PromptEvalCount + decoded.EvalCount,
		},
		Raw: map[string]string{"provider": o.Name()},
	}, nil
}

// Models lists locally installed models.
func (o *Ollama) Models(ctx context.Context) ([]Model, error) {
	var decoded ollamaTagsResponse
	url := joinURL(o.baseURL, "/api/tags")
	if err := getJSON(ctx, o.client, url, nil, &decoded); err != nil {
		return nil, fmt.Errorf("ollama models: %w", err)
	}
	models := make([]Model, 0, len(decoded.Models))
	for _, entry := range decoded.Models {
		models = append(models, Model{ID: firstNonEmpty(entry.Name, entry.Model)})
	}
	return models, nil
}

// ollamaOptions maps normalized parameters onto Ollama's options object. See the
// mapping table in docs/architecture.md.
func ollamaOptions(params Params) map[string]any {
	options := map[string]any{}
	if params.Temperature != nil {
		options["temperature"] = *params.Temperature
	}
	if params.TopP != nil {
		options["top_p"] = *params.TopP
	}
	if params.TopK != nil {
		options["top_k"] = *params.TopK
	}
	if params.MaxOutputTokens != nil {
		options["num_predict"] = *params.MaxOutputTokens
	}
	if params.RepeatPenalty != nil {
		options["repeat_penalty"] = *params.RepeatPenalty
	}
	if params.ContextWindow != nil {
		options["num_ctx"] = *params.ContextWindow
	}
	if params.Seed != nil {
		options["seed"] = *params.Seed
	}
	if len(params.Stop) > 0 {
		options["stop"] = params.Stop
	}
	for key, value := range params.Extra {
		options[key] = value
	}
	if len(options) == 0 {
		return nil
	}
	return options
}

// buildMessages renders a Request into chat messages: system prompt first, then
// labeled context blocks, then the user prompt.
func buildMessages(req Request) []ollamaMessage {
	messages := make([]ollamaMessage, 0, 3)
	if strings.TrimSpace(req.SystemPrompt) != "" {
		messages = append(messages, ollamaMessage{Role: string(RoleSystem), Content: req.SystemPrompt})
	}
	if context := RenderContext(req.Context); context != "" {
		messages = append(messages, ollamaMessage{Role: string(RoleUser), Content: context})
	}
	messages = append(messages, ollamaMessage{Role: string(RoleUser), Content: req.Prompt})
	return messages
}

// RenderContext formats context blocks into a single deterministic user message.
// It is exported because every chat-shaped adapter renders context identically.
func RenderContext(blocks []ContextBlock) string {
	if len(blocks) == 0 {
		return ""
	}
	var builder strings.Builder
	for i, block := range blocks {
		if i > 0 {
			builder.WriteString("\n\n")
		}
		label := strings.TrimSpace(block.Label)
		if label == "" {
			label = fmt.Sprintf("context-%d", i+1)
		}
		builder.WriteString("### " + label + "\n")
		builder.WriteString(block.Content)
	}
	return builder.String()
}
