package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/orchestrator"
	"github.com/ilyaus/loomwork/internal/provider"
)

func runPrompt(e *env, args []string) error {
	var projectRef, artifactRef, selector, prompt, promptFile, systemPrompt, outputName, outputType, tags string
	var pin, includePinned bool
	var temperature, topP float64
	var maxTokens, seed, topK int
	err := e.parse("run", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&artifactRef, "artifact", "", "artifact id, or name for its latest revision (required)")
		flags.StringVar(&selector, "model", "", "provider/model[#preset], e.g. ollama/qwen3:8b#code-review (required)")
		flags.StringVar(&prompt, "prompt", "", "prompt text")
		flags.StringVar(&promptFile, "prompt-file", "", "read the prompt from this file")
		flags.StringVar(&systemPrompt, "system", "", "override the workspace system prompt")
		flags.StringVar(&outputName, "name", "", "name for the produced artifact")
		flags.StringVar(&outputType, "type", "", "type for the produced artifact (default generated)")
		flags.StringVar(&tags, "tags", "", "comma-separated tags for the produced artifact")
		flags.BoolVar(&pin, "pin", false, "pin the produced artifact")
		flags.BoolVar(&includePinned, "include-pinned", false, "include the project's pinned artifacts as standing context")
		flags.Float64Var(&temperature, "temperature", -1, "override temperature")
		flags.Float64Var(&topP, "top-p", -1, "override top_p")
		flags.IntVar(&topK, "top-k", -1, "override top_k")
		flags.IntVar(&maxTokens, "max-tokens", -1, "override max output tokens")
		flags.IntVar(&seed, "seed", -1, "override seed")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(artifactRef) == "" || strings.TrimSpace(selector) == "" {
		return fmt.Errorf("run: --project, --artifact, and --model are required")
	}
	if (strings.TrimSpace(prompt) == "") == (strings.TrimSpace(promptFile) == "") {
		return fmt.Errorf("run: supply exactly one of --prompt or --prompt-file")
	}
	if strings.TrimSpace(promptFile) != "" {
		raw, readErr := os.ReadFile(promptFile)
		if readErr != nil {
			return fmt.Errorf("read --prompt-file %s: %w", promptFile, readErr)
		}
		prompt = string(raw)
	}

	overrides := provider.Params{}
	if temperature >= 0 {
		overrides.Temperature = provider.Float(temperature)
	}
	if topP >= 0 {
		overrides.TopP = provider.Float(topP)
	}
	if topK >= 0 {
		overrides.TopK = provider.Int(topK)
	}
	if maxTokens >= 0 {
		overrides.MaxOutputTokens = provider.Int(maxTokens)
	}
	if seed >= 0 {
		overrides.Seed = provider.Int(seed)
	}

	var artifactType model.ArtifactType
	if strings.TrimSpace(outputType) != "" {
		parsed, parseErr := model.ParseArtifactType(outputType)
		if parseErr != nil {
			return parseErr
		}
		artifactType = parsed
	}

	engine := orchestrator.New(e.config, e.store, e.presets, nil)
	result, err := engine.RunPrompt(context.Background(), orchestrator.RunRequest{
		ProjectRef:    projectRef,
		ArtifactRef:   artifactRef,
		Selector:      selector,
		Prompt:        prompt,
		SystemPrompt:  systemPrompt,
		OutputName:    outputName,
		OutputType:    artifactType,
		Tags:          splitList(tags),
		Pin:           pin,
		IncludePinned: includePinned,
		Overrides:     overrides,
	})
	if err != nil {
		return err
	}

	if e.asJSON {
		return e.emit(result, "")
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s -> %s (%s v%d)\n", result.Target.Name, result.Generated.Name, result.Generated.ID, result.Generated.Version)
	fmt.Fprintf(&builder, "provider: %s\tmodel: %s", result.Provider, result.Model)
	if result.Preset != "" {
		fmt.Fprintf(&builder, "\tpreset: %s", result.Preset)
	}
	fmt.Fprintf(&builder, "\nduration: %dms\ttokens: %d prompt / %d completion\n\n%s\n",
		result.Duration.Milliseconds(), result.Usage.PromptTokens, result.Usage.CompletionTokens, result.Generated.Body.Content)
	return e.emit(result, strings.TrimRight(builder.String(), "\n"))
}

func providersList(e *env, args []string) error {
	if err := e.parse("providers", args, nil); err != nil {
		return err
	}

	type providerView struct {
		Name         string   `json:"name"`
		Kind         string   `json:"kind"`
		BaseURL      string   `json:"baseUrl,omitempty"`
		DefaultModel string   `json:"defaultModel,omitempty"`
		Status       string   `json:"status"`
		PresetKeys   []string `json:"presetKeys,omitempty"`
	}

	views := make([]providerView, 0, len(e.config.Providers))
	for _, name := range e.config.ProviderNames() {
		declared := e.config.Providers[name]
		status := "configured"
		switch declared.Kind {
		case provider.KindAzure, provider.KindBedrock:
			if _, err := provider.BuildTextGenerator(declared); err != nil {
				status = "scaffold: " + err.Error()
			} else {
				status = "scaffold: generation not implemented yet"
			}
		}
		views = append(views, providerView{
			Name:         name,
			Kind:         string(declared.Kind),
			BaseURL:      providerEndpoint(declared),
			DefaultModel: declared.DefaultModel,
			Status:       status,
			PresetKeys:   presetKeysFor(e, declared.Kind),
		})
	}

	payload := map[string]any{
		"home":         e.home,
		"configFile":   e.paths.ConfigFile,
		"presetsFile":  e.paths.PresetsFile,
		"providers":    views,
		"presetGroups": e.presets.Keys(),
	}
	if e.asJSON {
		return e.emit(payload, "")
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "workspace: %s\n", e.home)
	for _, view := range views {
		fmt.Fprintf(&builder, "\n%s (%s)\n", view.Name, view.Kind)
		if view.BaseURL != "" {
			fmt.Fprintf(&builder, "  endpoint: %s\n", view.BaseURL)
		}
		fmt.Fprintf(&builder, "  status: %s\n", view.Status)
		if view.DefaultModel != "" {
			fmt.Fprintf(&builder, "  default model: %s\n", view.DefaultModel)
		}
		if len(view.PresetKeys) > 0 {
			fmt.Fprintf(&builder, "  preset groups: %s\n", strings.Join(view.PresetKeys, ", "))
		}
	}
	return e.emit(payload, strings.TrimRight(builder.String(), "\n"))
}

// providerEndpoint reports the address a provider talks to, which for the remote
// kinds lives in their kind-specific configuration rather than in BaseURL.
func providerEndpoint(declared provider.Config) string {
	switch declared.Kind {
	case provider.KindAzure:
		return declared.Azure.Endpoint
	case provider.KindBedrock:
		if declared.Bedrock.Region == "" {
			return ""
		}
		return "bedrock " + declared.Bedrock.Region
	default:
		return declared.BaseURL
	}
}

func presetKeysFor(e *env, kind provider.Kind) []string {
	prefix := string(kind) + "/"
	keys := make([]string, 0, 4)
	for _, key := range e.presets.Keys() {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
