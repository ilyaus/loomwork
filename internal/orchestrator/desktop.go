package orchestrator

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/ilyaus/loomwork/internal/preset"
	"github.com/ilyaus/loomwork/internal/provider"
)

// ProviderModels describes one configured text provider and the models the
// desktop can address through it.
type ProviderModels struct {
	Provider string        `json:"provider"`
	Kind     provider.Kind `json:"kind"`
	Status   string        `json:"status"`
	Models   []ModelChoice `json:"models"`
}

// ModelChoice is one selectable provider/model pair and its named presets.
type ModelChoice struct {
	ID          string   `json:"id"`
	Description string   `json:"description,omitempty"`
	Selector    string   `json:"selector"`
	Presets     []string `json:"presets"`
}

// ListModels returns every configured text provider. Discovery failures stay on
// their provider row so one unavailable backend does not hide the others.
func (o *Orchestrator) ListModels(ctx context.Context) []ProviderModels {
	groups := make([]ProviderModels, 0, len(o.config.Providers))
	seenKinds := map[provider.Kind]bool{}
	for _, name := range o.config.ProviderNames() {
		declared := o.config.Providers[name]
		if !isTextKind(declared.Kind) || seenKinds[declared.Kind] {
			continue
		}
		seenKinds[declared.Kind] = true
		group := ProviderModels{
			Provider: name,
			Kind:     declared.Kind,
			Status:   "available",
			Models:   o.configuredModels(declared),
		}
		generator, err := o.newModel(declared)
		if err != nil {
			group.Status = "unavailable: " + err.Error()
			groups = append(groups, group)
			continue
		}
		discovered, err := generator.Models(ctx)
		if err != nil {
			group.Status = "unavailable: " + err.Error()
			groups = append(groups, group)
			continue
		}
		choices := make(map[string]ModelChoice, len(group.Models)+len(discovered))
		for _, choice := range group.Models {
			choices[choice.ID] = choice
		}
		for _, model := range discovered {
			id := strings.TrimSpace(model.ID)
			if id == "" {
				continue
			}
			choices[id] = o.modelChoice(declared.Kind, id, model.Description)
		}
		group.Models = sortedModelChoices(choices)
		groups = append(groups, group)
	}
	return groups
}

func (o *Orchestrator) configuredModels(declared provider.Config) []ModelChoice {
	models := map[string]ModelChoice{}
	if model := strings.TrimSpace(declared.DefaultModel); model != "" {
		models[model] = o.modelChoice(declared.Kind, model, "")
	}
	prefix := string(declared.Kind) + "/"
	for _, key := range o.presets.Keys() {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		model := strings.TrimPrefix(key, prefix)
		if model == preset.WildcardModel {
			continue
		}
		models[model] = o.modelChoice(declared.Kind, model, "")
	}
	return sortedModelChoices(models)
}

func (o *Orchestrator) modelChoice(kind provider.Kind, model, description string) ModelChoice {
	return ModelChoice{
		ID:          model,
		Description: description,
		Selector:    string(kind) + "/" + model,
		Presets:     o.presets.PresetNames(kind, model),
	}
}

func sortedModelChoices(models map[string]ModelChoice) []ModelChoice {
	choices := make([]ModelChoice, 0, len(models))
	for _, model := range models {
		choices = append(choices, model)
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].ID < choices[j].ID })
	return choices
}

func isTextKind(kind provider.Kind) bool {
	for _, candidate := range provider.TextKinds() {
		if kind == candidate {
			return true
		}
	}
	return false
}

// ChatMessage is one turn in the desktop transcript.
type ChatMessage struct {
	Role    provider.Role `json:"role"`
	Content string        `json:"content"`
}

// ChatRequest describes one provider-backed desktop chat turn.
type ChatRequest struct {
	ProjectRef   string
	Selector     string
	Messages     []ChatMessage
	ArtifactRefs []string
}

// ChatResponse is provider-neutral so a future AgentAdapter can replace the
// current TextGenerator implementation without changing the SSE wire format.
type ChatResponse struct {
	Text         string         `json:"text"`
	Model        string         `json:"model,omitempty"`
	FinishReason string         `json:"finishReason,omitempty"`
	Usage        provider.Usage `json:"usage,omitempty"`
}

// Chat runs a transcript through the selected TextGenerator. Artifact refs are
// optional grounding context and are never mutated.
func (o *Orchestrator) Chat(ctx context.Context, request ChatRequest) (ChatResponse, error) {
	selector, err := preset.ParseSelector(request.Selector)
	if err != nil {
		return ChatResponse{}, err
	}
	params, err := o.presets.Resolve(selector, provider.Params{})
	if err != nil {
		return ChatResponse{}, err
	}
	declared, err := o.config.ProviderConfig(selector.Provider)
	if err != nil {
		return ChatResponse{}, err
	}
	generator, err := o.newModel(declared)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("build provider %q: %w", selector.Provider, err)
	}
	prompt, err := chatPrompt(request.Messages)
	if err != nil {
		return ChatResponse{}, err
	}
	contextBlocks, err := o.chatContext(request.ProjectRef, request.ArtifactRefs)
	if err != nil {
		return ChatResponse{}, err
	}
	response, err := generator.Generate(ctx, provider.Request{
		Model:        selector.Model,
		SystemPrompt: o.config.SystemPrompt,
		Prompt:       prompt,
		Context:      contextBlocks,
		Params:       params,
	})
	if err != nil {
		return ChatResponse{}, fmt.Errorf("chat with %s: %w", selector.String(), err)
	}
	return ChatResponse{
		Text:         response.Text,
		Model:        response.Model,
		FinishReason: response.FinishReason,
		Usage:        response.Usage,
	}, nil
}

func chatPrompt(messages []ChatMessage) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("chat needs at least one message")
	}
	var prompt strings.Builder
	for _, message := range messages {
		content := strings.TrimSpace(message.Content)
		if content == "" {
			return "", fmt.Errorf("chat messages cannot be empty")
		}
		switch message.Role {
		case provider.RoleUser, provider.RoleAssistant:
		default:
			return "", fmt.Errorf("chat role %q must be user or assistant", message.Role)
		}
		fmt.Fprintf(&prompt, "%s: %s\n\n", message.Role, content)
	}
	prompt.WriteString("assistant:")
	return prompt.String(), nil
}

func (o *Orchestrator) chatContext(projectRef string, refs []string) ([]provider.ContextBlock, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	project, err := o.store.Resolve(projectRef)
	if err != nil {
		return nil, fmt.Errorf("resolve project %q: %w", projectRef, err)
	}
	blocks := make([]provider.ContextBlock, 0, len(refs))
	for _, ref := range refs {
		artifact, ok := project.ResolveArtifact(ref)
		if !ok {
			return nil, fmt.Errorf("artifact %q not found in project %q", ref, project.Name)
		}
		content, err := ArtifactContent(artifact)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, provider.ContextBlock{
			Label:   blockLabel(artifact),
			Content: content,
		})
	}
	return blocks, nil
}
