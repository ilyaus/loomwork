// Package preset implements the config-driven, per-model parameter registry.
// Different models expose different useful parameter ranges, so parameters live
// in data (a JSON config) rather than in code, keyed by provider + model.
package preset

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/ilyaus/loomwork/internal/provider"
)

// WildcardModel matches any model of a provider, supplying provider-wide
// fallbacks.
const WildcardModel = "*"

// Entry declares defaults and named presets for one provider+model key.
type Entry struct {
	Provider provider.Kind              `json:"provider"`
	Model    string                     `json:"model"`
	Defaults provider.Params            `json:"defaults,omitempty"`
	Presets  map[string]provider.Params `json:"presets,omitempty"`
}

// File is the on-disk registry document.
type File struct {
	Entries []Entry `json:"entries"`
}

// Registry resolves parameters for a provider+model[#preset] selector.
type Registry struct {
	entries map[string]Entry
}

// Selector addresses a provider, model, and optional preset.
type Selector struct {
	Provider provider.Kind
	Model    string
	Preset   string
}

// String renders the selector in its canonical `provider/model[#preset]` form.
func (s Selector) String() string {
	rendered := string(s.Provider) + "/" + s.Model
	if s.Preset != "" {
		rendered += "#" + s.Preset
	}
	return rendered
}

// ParseSelector parses `provider/model[#preset]`. The model segment may itself
// contain `/`; only the first `/` and the last `#` are structural.
func ParseSelector(raw string) (Selector, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Selector{}, fmt.Errorf("selector is required in the form provider/model[#preset]")
	}
	slash := strings.Index(trimmed, "/")
	if slash <= 0 || slash == len(trimmed)-1 {
		return Selector{}, fmt.Errorf("invalid selector %q: expected provider/model[#preset]", raw)
	}
	kind, err := provider.ParseKind(trimmed[:slash])
	if err != nil {
		return Selector{}, fmt.Errorf("invalid selector %q: %w", raw, err)
	}
	remainder := trimmed[slash+1:]
	presetName := ""
	if hash := strings.LastIndex(remainder, "#"); hash >= 0 {
		presetName = strings.TrimSpace(remainder[hash+1:])
		remainder = remainder[:hash]
		if presetName == "" {
			return Selector{}, fmt.Errorf("invalid selector %q: preset name after '#' is empty", raw)
		}
	}
	model := strings.TrimSpace(remainder)
	if model == "" {
		return Selector{}, fmt.Errorf("invalid selector %q: model is empty", raw)
	}
	return Selector{Provider: kind, Model: model, Preset: presetName}, nil
}

// New builds a validated registry from entries.
func New(entries []Entry) (*Registry, error) {
	registry := &Registry{entries: make(map[string]Entry, len(entries))}
	for index, entry := range entries {
		kind, err := provider.ParseKind(string(entry.Provider))
		if err != nil {
			return nil, fmt.Errorf("preset entry %d: %w", index, err)
		}
		model := strings.TrimSpace(entry.Model)
		if model == "" {
			return nil, fmt.Errorf("preset entry %d (provider %s): model is required (use %q for provider-wide defaults)", index, kind, WildcardModel)
		}
		entry.Provider = kind
		entry.Model = model

		key := entryKey(kind, model)
		if _, duplicate := registry.entries[key]; duplicate {
			return nil, fmt.Errorf("duplicate preset entry for %s", key)
		}
		if err := validateParams(entry.Defaults, key+" defaults"); err != nil {
			return nil, err
		}
		seen := map[string]struct{}{}
		for name, params := range entry.Presets {
			normalized := strings.TrimSpace(name)
			if normalized == "" {
				return nil, fmt.Errorf("%s: preset name is empty", key)
			}
			lowered := strings.ToLower(normalized)
			if _, duplicate := seen[lowered]; duplicate {
				return nil, fmt.Errorf("%s: duplicate preset name %q", key, normalized)
			}
			seen[lowered] = struct{}{}
			if err := validateParams(params, key+"#"+normalized); err != nil {
				return nil, err
			}
		}
		registry.entries[key] = entry
	}
	return registry, nil
}

// Load reads and validates a registry document from JSON.
func Load(reader io.Reader) (*Registry, error) {
	var file File
	if err := json.NewDecoder(reader).Decode(&file); err != nil {
		return nil, fmt.Errorf("decode preset registry: %w", err)
	}
	return New(file.Entries)
}

// LoadFile reads and validates a registry document from disk. A missing file
// yields an empty registry so the tool works with no configuration at all.
func LoadFile(path string) (*Registry, error) {
	handle, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return New(nil)
		}
		return nil, fmt.Errorf("open preset registry %s: %w", path, err)
	}
	defer func() { _ = handle.Close() }()
	registry, err := Load(handle)
	if err != nil {
		return nil, fmt.Errorf("preset registry %s: %w", path, err)
	}
	return registry, nil
}

// Resolve produces the effective parameters for a selector. Later sources win:
// built-in provider defaults, the provider's wildcard entry defaults, the
// model entry defaults, the named preset, then the caller's overrides.
func (r *Registry) Resolve(selector Selector, overrides provider.Params) (provider.Params, error) {
	params := builtinDefaults(selector.Provider)

	if wildcard, ok := r.entries[entryKey(selector.Provider, WildcardModel)]; ok {
		params = params.Merge(wildcard.Defaults)
	}
	entry, hasEntry := r.entries[entryKey(selector.Provider, selector.Model)]
	if hasEntry {
		params = params.Merge(entry.Defaults)
	}

	if selector.Preset != "" {
		presetParams, ok := r.lookupPreset(selector)
		if !ok {
			return provider.Params{}, fmt.Errorf("unknown preset %q for %s/%s: available presets are %s",
				selector.Preset, selector.Provider, selector.Model, describeAvailable(r.PresetNames(selector.Provider, selector.Model)))
		}
		params = params.Merge(presetParams)
	}

	return params.Merge(overrides), nil
}

func (r *Registry) lookupPreset(selector Selector) (provider.Params, bool) {
	for _, model := range []string{selector.Model, WildcardModel} {
		entry, ok := r.entries[entryKey(selector.Provider, model)]
		if !ok {
			continue
		}
		for name, params := range entry.Presets {
			if strings.EqualFold(strings.TrimSpace(name), selector.Preset) {
				return params, true
			}
		}
	}
	return provider.Params{}, false
}

// PresetNames lists preset names available for a provider+model, including
// wildcard presets, sorted.
func (r *Registry) PresetNames(kind provider.Kind, model string) []string {
	names := map[string]struct{}{}
	for _, candidate := range []string{model, WildcardModel} {
		entry, ok := r.entries[entryKey(kind, candidate)]
		if !ok {
			continue
		}
		for name := range entry.Presets {
			names[strings.TrimSpace(name)] = struct{}{}
		}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	return sorted
}

// Keys lists every provider/model key in the registry, sorted.
func (r *Registry) Keys() []string {
	keys := make([]string, 0, len(r.entries))
	for key := range r.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func entryKey(kind provider.Kind, model string) string {
	return string(kind) + "/" + model
}

func describeAvailable(names []string) string {
	if len(names) == 0 {
		return "(none configured)"
	}
	return strings.Join(names, ", ")
}

// builtinDefaults are conservative, provider-appropriate starting points used
// when a config supplies nothing.
func builtinDefaults(kind provider.Kind) provider.Params {
	switch kind {
	case provider.KindOllama:
		return provider.Params{Temperature: provider.Float(0.2), TopP: provider.Float(0.9)}
	case provider.KindLMStudio:
		return provider.Params{Temperature: provider.Float(0.2), TopP: provider.Float(0.9)}
	default:
		return provider.Params{Temperature: provider.Float(0.2)}
	}
}
