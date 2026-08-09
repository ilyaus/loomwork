// Package orchestrator wires the domain, providers, presets, and storage
// together. It is the only package allowed to combine those concerns, and it is
// transport agnostic so a CLI, an HTTP server, or a future workbench can share
// it unchanged.
package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ilyaus/loomwork/internal/config"
	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/preset"
	"github.com/ilyaus/loomwork/internal/provider"
	"github.com/ilyaus/loomwork/internal/store"
)

// TextGeneratorFactory builds a text provider from a declaration. It is
// injectable so tests can substitute a fake without any network.
type TextGeneratorFactory func(cfg provider.Config) (provider.TextGenerator, error)

// Orchestrator executes prompt runs against project artifacts.
type Orchestrator struct {
	config   config.Config
	store    store.ProjectStore
	presets  *preset.Registry
	newModel TextGeneratorFactory
	now      func() time.Time
}

// New builds an orchestrator. A nil factory uses provider.BuildTextGenerator.
func New(cfg config.Config, projects store.ProjectStore, presets *preset.Registry, factory TextGeneratorFactory) *Orchestrator {
	if factory == nil {
		factory = provider.BuildTextGenerator
	}
	return &Orchestrator{
		config:   cfg.WithDefaults(),
		store:    projects,
		presets:  presets,
		newModel: factory,
		now:      time.Now,
	}
}

// RunRequest describes one prompt run.
type RunRequest struct {
	// ProjectRef is a project id or name.
	ProjectRef string
	// ArtifactRef is an artifact id, or a name whose latest revision is used.
	ArtifactRef string
	// Selector is `provider/model[#preset]`.
	Selector string
	// Prompt is the instruction applied to the artifact.
	Prompt string
	// SystemPrompt overrides the workspace default.
	SystemPrompt string
	// OutputName names the produced artifact; defaults to "<artifact>.<preset|model>".
	OutputName string
	// OutputType defaults to model.ArtifactTypeGenerated.
	OutputType model.ArtifactType
	// Tags are applied to the produced artifact.
	Tags []string
	// Pin pins the produced artifact.
	Pin bool
	// IncludePinned adds the project's pinned artifacts as standing context.
	IncludePinned bool
	// Overrides win over every preset and default.
	Overrides provider.Params
}

// RunResult reports what a prompt run produced.
type RunResult struct {
	Project   *model.Project `json:"-"`
	ProjectID string         `json:"projectId"`
	Target    model.Artifact `json:"target"`
	Generated model.Artifact `json:"generated"`
	Provider  string         `json:"provider"`
	Model     string         `json:"model"`
	Preset    string         `json:"preset,omitempty"`
	Usage     provider.Usage `json:"usage,omitempty"`
	// Duration is not serialized directly: a time.Duration marshals as
	// nanoseconds, so DurationMs carries the value the field name promises.
	Duration   time.Duration     `json:"-"`
	DurationMs int64             `json:"durationMs"`
	Params     provider.Params   `json:"params"`
	Raw        map[string]string `json:"raw,omitempty"`
}

// RunPrompt resolves the project and artifact, assembles the request, calls the
// provider, and appends the response as a new artifact derived from the target.
// A failure leaves the stored project untouched.
func (o *Orchestrator) RunPrompt(ctx context.Context, request RunRequest) (RunResult, error) {
	if strings.TrimSpace(request.Prompt) == "" {
		return RunResult{}, fmt.Errorf("prompt is required")
	}

	project, err := o.store.Resolve(request.ProjectRef)
	if err != nil {
		return RunResult{}, fmt.Errorf("resolve project %q: %w", request.ProjectRef, err)
	}
	target, ok := project.ResolveArtifact(request.ArtifactRef)
	if !ok {
		return RunResult{}, fmt.Errorf("artifact %q not found in project %q", request.ArtifactRef, project.Name)
	}

	selector, err := preset.ParseSelector(request.Selector)
	if err != nil {
		return RunResult{}, err
	}
	params, err := o.presets.Resolve(selector, request.Overrides)
	if err != nil {
		return RunResult{}, err
	}
	providerConfig, err := o.config.ProviderConfig(selector.Provider)
	if err != nil {
		return RunResult{}, err
	}
	generator, err := o.newModel(providerConfig)
	if err != nil {
		return RunResult{}, fmt.Errorf("build provider %q: %w", selector.Provider, err)
	}

	blocks, err := o.contextBlocks(project, target, request.IncludePinned)
	if err != nil {
		return RunResult{}, err
	}

	generateRequest := provider.Request{
		Model:        selector.Model,
		SystemPrompt: firstNonEmpty(request.SystemPrompt, o.config.SystemPrompt),
		Prompt:       request.Prompt,
		Context:      blocks,
		Params:       params,
	}

	started := o.now()
	response, err := generator.Generate(ctx, generateRequest)
	if err != nil {
		return RunResult{}, fmt.Errorf("run prompt on artifact %q with %s: %w", target.Name, selector, err)
	}
	duration := o.now().Sub(started)

	spec := model.ArtifactSpec{
		Name:   firstNonEmpty(request.OutputName, defaultOutputName(target, selector)),
		Type:   defaultOutputType(request.OutputType),
		Body:   model.Body{Content: response.Text, MediaType: "text/markdown"},
		Tags:   request.Tags,
		Pinned: request.Pin,
		Metadata: map[string]string{
			"provider":       generator.Name(),
			"model":          firstNonEmpty(response.Model, selector.Model),
			"sourceArtifact": target.ID,
			"promptSha256":   promptDigest(request.Prompt),
			"durationMs":     strconv.FormatInt(duration.Milliseconds(), 10),
		},
	}
	if selector.Preset != "" {
		spec.Metadata["preset"] = selector.Preset
	}
	if response.FinishReason != "" {
		spec.Metadata["finishReason"] = response.FinishReason
	}

	// Persist in one serialized read-modify-write cycle rather than saving the
	// copy loaded before generation, so a concurrent run cannot be lost.
	var generated model.Artifact
	stored, err := o.store.Update(request.ProjectRef, func(current *model.Project) error {
		derived, err := current.DeriveArtifact(target.ID, spec)
		generated = derived
		return err
	})
	if err != nil {
		return RunResult{}, fmt.Errorf("store prompt result: %w", err)
	}
	project = stored

	return RunResult{
		Project:    project,
		ProjectID:  project.ID,
		Target:     target,
		Generated:  generated,
		Provider:   generator.Name(),
		Model:      firstNonEmpty(response.Model, selector.Model),
		Preset:     selector.Preset,
		Usage:      response.Usage,
		Duration:   duration,
		DurationMs: duration.Milliseconds(),
		Params:     params,
		Raw:        response.Raw,
	}, nil
}

// contextBlocks assembles the target artifact plus, optionally, the project's
// pinned artifacts as standing context. The target always comes last so it is
// closest to the instruction.
func (o *Orchestrator) contextBlocks(project *model.Project, target model.Artifact, includePinned bool) ([]provider.ContextBlock, error) {
	blocks := make([]provider.ContextBlock, 0, 4)
	if includePinned {
		for _, pinned := range project.PinnedArtifacts() {
			if pinned.ID == target.ID {
				continue
			}
			content, err := ArtifactContent(pinned)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, provider.ContextBlock{Label: blockLabel(pinned) + " (pinned)", Content: content})
		}
	}
	content, err := ArtifactContent(target)
	if err != nil {
		return nil, err
	}
	blocks = append(blocks, provider.ContextBlock{Label: blockLabel(target), Content: content})
	return blocks, nil
}

func blockLabel(artifact model.Artifact) string {
	return fmt.Sprintf("%s [%s v%d]", artifact.Name, artifact.Type, artifact.Version)
}

func defaultOutputName(target model.Artifact, selector preset.Selector) string {
	suffix := selector.Preset
	if suffix == "" {
		suffix = strings.ReplaceAll(selector.Model, "/", "-")
	}
	return fmt.Sprintf("%s.%s", target.Name, suffix)
}

func defaultOutputType(requested model.ArtifactType) model.ArtifactType {
	if strings.TrimSpace(string(requested)) == "" {
		return model.ArtifactTypeGenerated
	}
	return requested
}

func promptDigest(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
