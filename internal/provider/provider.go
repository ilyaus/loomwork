// Package provider defines Loomwork's model-provider abstraction: one interface
// for text generation and one for image generation, with pluggable adapters.
// Adapters know nothing about projects or artifacts; they translate a normalized
// Request into a backend's wire format and back.
package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Kind identifies a provider adapter.
type Kind string

const (
	KindOllama   Kind = "ollama"
	KindLMStudio Kind = "lmstudio"
	KindAzure    Kind = "azure"
	KindBedrock  Kind = "bedrock"
	KindImGen    Kind = "imgen"
)

// TextKinds lists the provider kinds that implement TextGenerator.
func TextKinds() []Kind {
	return []Kind{KindOllama, KindLMStudio, KindAzure, KindBedrock}
}

// ParseKind validates a raw provider kind, including image providers.
func ParseKind(raw string) (Kind, error) {
	candidate := Kind(strings.TrimSpace(strings.ToLower(raw)))
	for _, known := range append(TextKinds(), KindImGen) {
		if candidate == known {
			return known, nil
		}
	}
	return "", fmt.Errorf("unknown provider kind %q: supported kinds are ollama, lmstudio, azure, bedrock, imgen", raw)
}

// ErrNotImplemented is the sentinel for an adapter that exists behind the
// interface but whose request mapping is not finished yet. Callers detect the
// condition with errors.Is rather than string matching. No adapter returns it
// today; it stays the contract for the next one to land incrementally.
var ErrNotImplemented = errors.New("provider adapter not implemented yet")

// Role identifies the author of a message in a chat-shaped request.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// ContextBlock is a labeled piece of material (typically an artifact) supplied
// as grounding context for a prompt.
type ContextBlock struct {
	Label   string
	Content string
}

// Params is the normalized parameter set. Adapters map the fields they support
// and drop the rest; see docs/architecture.md for the mapping table. A nil
// pointer means "unset — let the backend decide".
type Params struct {
	Temperature     *float64       `json:"temperature,omitempty"`
	TopP            *float64       `json:"top_p,omitempty"`
	TopK            *int           `json:"top_k,omitempty"`
	MaxOutputTokens *int           `json:"max_output_tokens,omitempty"`
	RepeatPenalty   *float64       `json:"repeat_penalty,omitempty"`
	ContextWindow   *int           `json:"num_ctx,omitempty"`
	Seed            *int           `json:"seed,omitempty"`
	Stop            []string       `json:"stop,omitempty"`
	Extra           map[string]any `json:"extra,omitempty"`
}

// Merge returns a copy of p with every value set in override applied on top.
func (p Params) Merge(override Params) Params {
	merged := p.Clone()
	if override.Temperature != nil {
		merged.Temperature = override.Temperature
	}
	if override.TopP != nil {
		merged.TopP = override.TopP
	}
	if override.TopK != nil {
		merged.TopK = override.TopK
	}
	if override.MaxOutputTokens != nil {
		merged.MaxOutputTokens = override.MaxOutputTokens
	}
	if override.RepeatPenalty != nil {
		merged.RepeatPenalty = override.RepeatPenalty
	}
	if override.ContextWindow != nil {
		merged.ContextWindow = override.ContextWindow
	}
	if override.Seed != nil {
		merged.Seed = override.Seed
	}
	if len(override.Stop) > 0 {
		merged.Stop = append([]string(nil), override.Stop...)
	}
	for key, value := range override.Extra {
		if merged.Extra == nil {
			merged.Extra = map[string]any{}
		}
		merged.Extra[key] = value
	}
	return merged
}

// Clone deep-copies the parameter set.
func (p Params) Clone() Params {
	clone := Params{
		Temperature:     copyFloat(p.Temperature),
		TopP:            copyFloat(p.TopP),
		TopK:            copyInt(p.TopK),
		MaxOutputTokens: copyInt(p.MaxOutputTokens),
		RepeatPenalty:   copyFloat(p.RepeatPenalty),
		ContextWindow:   copyInt(p.ContextWindow),
		Seed:            copyInt(p.Seed),
	}
	if len(p.Stop) > 0 {
		clone.Stop = append([]string(nil), p.Stop...)
	}
	if len(p.Extra) > 0 {
		clone.Extra = make(map[string]any, len(p.Extra))
		for key, value := range p.Extra {
			clone.Extra[key] = value
		}
	}
	return clone
}

// Request is a provider-agnostic text generation request.
type Request struct {
	Model        string
	SystemPrompt string
	Prompt       string
	Context      []ContextBlock
	Params       Params
}

// Validate checks the request is minimally well formed.
func (r Request) Validate() error {
	if strings.TrimSpace(r.Model) == "" {
		return errors.New("request model is required")
	}
	if strings.TrimSpace(r.Prompt) == "" {
		return errors.New("request prompt is required")
	}
	return nil
}

// Usage reports token accounting when the backend provides it.
type Usage struct {
	PromptTokens     int `json:"promptTokens,omitempty"`
	CompletionTokens int `json:"completionTokens,omitempty"`
	TotalTokens      int `json:"totalTokens,omitempty"`
}

// Response is a provider-agnostic text generation response.
type Response struct {
	Text         string            `json:"text"`
	Model        string            `json:"model,omitempty"`
	FinishReason string            `json:"finishReason,omitempty"`
	Usage        Usage             `json:"usage,omitempty"`
	Raw          map[string]string `json:"raw,omitempty"`
}

// Model describes a model exposed by a provider.
type Model struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
}

// TextGenerator is the single text-generation interface every text adapter
// implements. Adding a backend means adding an adapter, never changing callers.
type TextGenerator interface {
	Name() string
	Models(ctx context.Context) ([]Model, error)
	Generate(ctx context.Context, req Request) (Response, error)
}

// ImageRequest is a provider-agnostic image generation request. It is separate
// from Request because image generation is asynchronous and multi-artifact.
type ImageRequest struct {
	Model          string
	Prompt         string
	NegativePrompt string
	Width          int
	Height         int
	Count          int
	Steps          int
	GuidanceScale  *float64
	Seed           *int
	Extra          map[string]any
}

// Validate checks the request is minimally well formed.
func (r ImageRequest) Validate() error {
	if strings.TrimSpace(r.Model) == "" {
		return errors.New("image request model is required")
	}
	if strings.TrimSpace(r.Prompt) == "" {
		return errors.New("image request prompt is required")
	}
	return nil
}

// ImageArtifact describes one generated image.
type ImageArtifact struct {
	Filename    string `json:"filename"`
	Path        string `json:"path,omitempty"`
	DownloadURL string `json:"downloadUrl,omitempty"`
	MediaType   string `json:"mediaType,omitempty"`
	SizeBytes   int64  `json:"sizeBytes,omitempty"`
	Seed        *int   `json:"seed,omitempty"`
}

// ImageResult is the outcome of an image generation job.
type ImageResult struct {
	JobID     string          `json:"jobId,omitempty"`
	Model     string          `json:"model,omitempty"`
	Artifacts []ImageArtifact `json:"artifacts"`
}

// ImageGenerator is the single image-generation interface.
type ImageGenerator interface {
	Name() string
	Models(ctx context.Context) ([]Model, error)
	GenerateImages(ctx context.Context, req ImageRequest) (ImageResult, error)
}

func copyFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyInt(value *int) *int {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

// Float returns a pointer to v, for building Params literals.
func Float(v float64) *float64 { return &v }

// Int returns a pointer to v, for building Params literals.
func Int(v int) *int { return &v }
