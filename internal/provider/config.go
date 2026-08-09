package provider

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Environment variables that carry provider credentials. Credentials are never
// read from config files and never logged.
const (
	EnvLMStudioAPIKey     = "LMSTUDIO_API_KEY"
	EnvAzureAPIKey        = "AZURE_AI_API_KEY"
	EnvAWSAccessKeyID     = "AWS_ACCESS_KEY_ID"
	EnvAWSSecretAccessKey = "AWS_SECRET_ACCESS_KEY"
	EnvAWSSessionToken    = "AWS_SESSION_TOKEN"
	EnvAWSRegion          = "AWS_REGION"
)

// Default endpoints for local providers, chosen so a developer workstation needs
// no configuration at all.
const (
	DefaultOllamaBaseURL   = "http://localhost:11434"
	DefaultLMStudioBaseURL = "http://localhost:1234/v1"
	DefaultImGenBaseURL    = "http://localhost:8000"
)

// Default timeouts.
const (
	DefaultGenerateTimeout  = 120 * time.Second
	DefaultDiscoveryTimeout = 10 * time.Second
	DefaultPollInterval     = 2 * time.Second
)

// Config declares one provider instance. It is deserialized from
// $LOOMWORK_HOME/config.json; credential fields are intentionally absent.
type Config struct {
	Kind Kind `json:"kind"`
	// BaseURL overrides the adapter default endpoint.
	BaseURL string `json:"baseUrl,omitempty"`
	// DefaultModel is used when a selector omits the model.
	DefaultModel string `json:"defaultModel,omitempty"`
	// TimeoutSeconds bounds a single generation call.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
	// Azure AI Foundry wiring.
	Azure AzureConfig `json:"azure,omitempty"`
	// AWS Bedrock wiring.
	Bedrock BedrockConfig `json:"bedrock,omitempty"`
	// ImGen wiring for the image adapter.
	ImGen ImGenConfig `json:"imgen,omitempty"`
}

// AzureConfig carries non-secret Azure AI Foundry deployment coordinates.
type AzureConfig struct {
	Endpoint   string `json:"endpoint,omitempty"`
	Deployment string `json:"deployment,omitempty"`
	APIVersion string `json:"apiVersion,omitempty"`
	// APIKeyEnv names the environment variable holding the key.
	APIKeyEnv string `json:"apiKeyEnv,omitempty"`
}

// BedrockConfig carries non-secret AWS Bedrock coordinates.
type BedrockConfig struct {
	Region  string `json:"region,omitempty"`
	ModelID string `json:"modelId,omitempty"`
	Profile string `json:"profile,omitempty"`
}

// ImGenConfig carries im-gen polling behavior.
type ImGenConfig struct {
	PollIntervalSeconds int `json:"pollIntervalSeconds,omitempty"`
}

// Timeout returns the configured generation timeout or the default.
func (c Config) Timeout() time.Duration {
	if c.TimeoutSeconds > 0 {
		return time.Duration(c.TimeoutSeconds) * time.Second
	}
	return DefaultGenerateTimeout
}

// Validate checks the declaration is internally consistent, without contacting
// any backend.
func (c Config) Validate() error {
	kind, err := ParseKind(string(c.Kind))
	if err != nil {
		return err
	}
	switch kind {
	case KindAzure:
		if strings.TrimSpace(c.Azure.Endpoint) == "" {
			return fmt.Errorf("azure provider requires azure.endpoint")
		}
		if strings.TrimSpace(c.Azure.Deployment) == "" {
			return fmt.Errorf("azure provider requires azure.deployment")
		}
	case KindBedrock:
		if strings.TrimSpace(c.Bedrock.Region) == "" && strings.TrimSpace(os.Getenv(EnvAWSRegion)) == "" {
			return fmt.Errorf("bedrock provider requires bedrock.region or %s", EnvAWSRegion)
		}
	}
	return nil
}

// BuildTextGenerator constructs the text adapter declared by cfg.
func BuildTextGenerator(cfg Config) (TextGenerator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	switch cfg.Kind {
	case KindOllama:
		return NewOllama(cfg), nil
	case KindLMStudio:
		return NewLMStudio(cfg), nil
	case KindAzure:
		return NewAzure(cfg)
	case KindBedrock:
		return NewBedrock(cfg)
	default:
		return nil, fmt.Errorf("provider kind %q does not implement text generation", cfg.Kind)
	}
}

// BuildImageGenerator constructs the image adapter declared by cfg.
func BuildImageGenerator(cfg Config) (ImageGenerator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	switch cfg.Kind {
	case KindImGen:
		return NewImGen(cfg), nil
	default:
		return nil, fmt.Errorf("provider kind %q does not implement image generation", cfg.Kind)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func newHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultGenerateTimeout
	}
	return &http.Client{Timeout: timeout}
}
