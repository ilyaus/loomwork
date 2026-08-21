package provider

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// DefaultAzureAPIVersion is used when the config omits one.
const DefaultAzureAPIVersion = "2024-10-21"

// Azure is the Azure AI Foundry adapter. Foundry exposes an OpenAI-compatible
// chat API, so the wire mapping is shared with the LM Studio adapter; only the
// URL shape (deployment path plus api-version) and the "api-key" header differ.
//
// TODO(azure): support Entra ID (bearer token) credentials in addition to API
// keys.
type Azure struct {
	endpoint     string
	deployment   string
	apiVersion   string
	defaultModel string
	apiKey       string
	client       *http.Client
}

// NewAzure builds the Azure adapter, validating deployment coordinates and
// resolving the API key from the environment (never from config files).
func NewAzure(cfg Config) (*Azure, error) {
	endpoint := firstNonEmpty(cfg.Azure.Endpoint, cfg.BaseURL)
	if endpoint == "" {
		return nil, fmt.Errorf("azure provider requires azure.endpoint")
	}
	if strings.TrimSpace(cfg.Azure.Deployment) == "" {
		return nil, fmt.Errorf("azure provider requires azure.deployment")
	}
	keyEnv := firstNonEmpty(cfg.Azure.APIKeyEnv, EnvAzureAPIKey)
	apiKey := strings.TrimSpace(os.Getenv(keyEnv))
	if apiKey == "" {
		return nil, fmt.Errorf("azure provider requires an API key in environment variable %s", keyEnv)
	}
	return &Azure{
		endpoint:     strings.TrimRight(endpoint, "/"),
		deployment:   strings.TrimSpace(cfg.Azure.Deployment),
		apiVersion:   firstNonEmpty(cfg.Azure.APIVersion, DefaultAzureAPIVersion),
		defaultModel: cfg.DefaultModel,
		apiKey:       apiKey,
		client:       newHTTPClient(cfg.Timeout()),
	}, nil
}

// Name identifies the adapter.
func (a *Azure) Name() string { return string(KindAzure) }

// ChatCompletionsURL is the deployment-scoped chat completions endpoint.
func (a *Azure) ChatCompletionsURL() string {
	return fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s", a.endpoint, a.deployment, a.apiVersion)
}

// ModelsURL is the model discovery endpoint.
func (a *Azure) ModelsURL() string {
	return fmt.Sprintf("%s/openai/models?api-version=%s", a.endpoint, a.apiVersion)
}

// Generate performs a non-streaming chat completion against the deployment. The
// normalized parameters map exactly as they do for any OpenAI-compatible
// backend: TopK, RepeatPenalty, and ContextWindow have no portable equivalent
// and are dropped rather than guessed at.
func (a *Azure) Generate(ctx context.Context, req Request) (Response, error) {
	req.Model = firstNonEmpty(req.Model, a.defaultModel, a.deployment)
	if err := req.Validate(); err != nil {
		return Response{}, fmt.Errorf("azure: %w", err)
	}

	var decoded openAIChatResponse
	url := a.ChatCompletionsURL()
	if err := postJSON(ctx, a.client, url, a.headers(), lmStudioBody(req), &decoded); err != nil {
		return Response{}, fmt.Errorf("azure generate (deployment %s, model %s): %w", a.deployment, req.Model, err)
	}
	if len(decoded.Choices) == 0 || strings.TrimSpace(decoded.Choices[0].Message.Content) == "" {
		return Response{}, fmt.Errorf("azure generate (deployment %s, model %s): empty completion", a.deployment, req.Model)
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
		Raw: map[string]string{"provider": a.Name(), "deployment": a.deployment},
	}, nil
}

// Models lists the models the resource exposes.
func (a *Azure) Models(ctx context.Context) ([]Model, error) {
	var decoded openAIModelsResponse
	if err := getJSON(ctx, a.client, a.ModelsURL(), a.headers(), &decoded); err != nil {
		return nil, fmt.Errorf("azure models (deployment %s): %w", a.deployment, err)
	}
	models := make([]Model, 0, len(decoded.Data))
	for _, entry := range decoded.Data {
		models = append(models, Model{ID: entry.ID, Description: entry.OwnedBy})
	}
	return models, nil
}

func (a *Azure) headers() map[string]string {
	return map[string]string{"api-key": a.apiKey}
}
