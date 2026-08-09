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

// Azure is the Azure AI Foundry adapter.
//
// Status: SCAFFOLD. Construction, configuration, and credential resolution are
// implemented and testable; the request/response mapping is deferred and
// Generate returns an error wrapping ErrNotImplemented.
//
// TODO(azure): implement Generate against
// POST {endpoint}/openai/deployments/{deployment}/chat/completions?api-version={apiVersion}
// with the "api-key" header, reusing lmStudioBody's OpenAI-shaped payload and
// openAIChatResponse for decoding. TODO(azure): implement Models via
// GET {endpoint}/openai/models?api-version={apiVersion}. TODO(azure): support
// Entra ID (bearer token) credentials in addition to API keys.
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

// ChatCompletionsURL is the endpoint Generate will target once implemented. It
// exists so wiring and configuration can be verified today.
func (a *Azure) ChatCompletionsURL() string {
	return fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s", a.endpoint, a.deployment, a.apiVersion)
}

// Generate is not implemented yet.
func (a *Azure) Generate(_ context.Context, req Request) (Response, error) {
	model := firstNonEmpty(req.Model, a.defaultModel, a.deployment)
	return Response{}, fmt.Errorf("azure generate (deployment %s, model %s): %w", a.deployment, model, ErrNotImplemented)
}

// Models is not implemented yet.
func (a *Azure) Models(_ context.Context) ([]Model, error) {
	return nil, fmt.Errorf("azure models (deployment %s): %w", a.deployment, ErrNotImplemented)
}
