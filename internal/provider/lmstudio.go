package provider

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// LMStudio talks to LM Studio's local OpenAI-compatible HTTP API. Any other
// OpenAI-compatible local server works by pointing BaseURL at it.
type LMStudio struct {
	baseURL      string
	defaultModel string
	apiKey       string
	client       *http.Client
}

// NewLMStudio builds an LM Studio adapter. The API key, when present, is read
// from the environment only.
func NewLMStudio(cfg Config) *LMStudio {
	return &LMStudio{
		baseURL:      firstNonEmpty(cfg.BaseURL, DefaultLMStudioBaseURL),
		defaultModel: cfg.DefaultModel,
		apiKey:       strings.TrimSpace(os.Getenv(EnvLMStudioAPIKey)),
		client:       newHTTPClient(cfg.Timeout()),
	}
}

// Name identifies the adapter.
func (l *LMStudio) Name() string { return string(KindLMStudio) }

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

type openAIModelsResponse struct {
	Data []struct {
		ID      string `json:"id"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

// Generate performs a non-streaming chat completion.
func (l *LMStudio) Generate(ctx context.Context, req Request) (Response, error) {
	req.Model = firstNonEmpty(req.Model, l.defaultModel)
	if err := req.Validate(); err != nil {
		return Response{}, fmt.Errorf("lmstudio: %w", err)
	}

	payload := lmStudioBody(req)
	var decoded openAIChatResponse
	url := joinURL(l.baseURL, "/chat/completions")
	if err := postJSON(ctx, l.client, url, l.headers(), payload, &decoded); err != nil {
		return Response{}, fmt.Errorf("lmstudio generate (model %s): %w", req.Model, err)
	}
	if len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return Response{}, fmt.Errorf("lmstudio generate (model %s): empty completion from %s", req.Model, url)
	}

	return Response{
		Text:         decoded.Choices[0].Message.Content,
		Model:        firstNonEmpty(decoded.Model, req.Model),
		FinishReason: decoded.Choices[0].FinishReason,
		Usage: Usage{
			PromptTokens:     decoded.Usage.PromptTokens,
			CompletionTokens: decoded.Usage.CompletionTokens,
			TotalTokens:      decoded.Usage.TotalTokens,
		},
		Raw: map[string]string{"provider": l.Name()},
	}, nil
}

// Models lists models the local server currently exposes.
func (l *LMStudio) Models(ctx context.Context) ([]Model, error) {
	var decoded openAIModelsResponse
	url := joinURL(l.baseURL, "/models")
	if err := getJSON(ctx, l.client, url, l.headers(), &decoded); err != nil {
		return nil, fmt.Errorf("lmstudio models: %w", err)
	}
	models := make([]Model, 0, len(decoded.Data))
	for _, entry := range decoded.Data {
		models = append(models, Model{ID: entry.ID, Description: entry.OwnedBy})
	}
	return models, nil
}

func (l *LMStudio) headers() map[string]string {
	if l.apiKey == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + l.apiKey}
}

// lmStudioBody maps normalized parameters onto the OpenAI chat schema. TopK,
// RepeatPenalty, and ContextWindow have no portable OpenAI equivalent and are
// intentionally dropped; pass them through Params.Extra when a specific server
// supports them.
func lmStudioBody(req Request) map[string]any {
	messages := make([]openAIMessage, 0, 3)
	if strings.TrimSpace(req.SystemPrompt) != "" {
		messages = append(messages, openAIMessage{Role: string(RoleSystem), Content: req.SystemPrompt})
	}
	if rendered := RenderContext(req.Context); rendered != "" {
		messages = append(messages, openAIMessage{Role: string(RoleUser), Content: rendered})
	}
	messages = append(messages, openAIMessage{Role: string(RoleUser), Content: req.Prompt})

	body := map[string]any{
		"model":    req.Model,
		"messages": messages,
		"stream":   false,
	}
	if req.Params.Temperature != nil {
		body["temperature"] = *req.Params.Temperature
	}
	if req.Params.TopP != nil {
		body["top_p"] = *req.Params.TopP
	}
	if req.Params.MaxOutputTokens != nil {
		body["max_tokens"] = *req.Params.MaxOutputTokens
	}
	if req.Params.Seed != nil {
		body["seed"] = *req.Params.Seed
	}
	if len(req.Params.Stop) > 0 {
		body["stop"] = req.Params.Stop
	}
	for key, value := range req.Params.Extra {
		body[key] = value
	}
	return body
}
