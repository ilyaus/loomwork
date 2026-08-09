// Package config resolves the Loomwork workspace layout and the non-secret
// provider declarations stored inside it. Credentials are never read from files;
// adapters resolve them from the environment.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ilyaus/loomwork/internal/cuenote"
	"github.com/ilyaus/loomwork/internal/provider"
)

// EnvHome names the environment variable overriding the workspace directory.
const EnvHome = "LOOMWORK_HOME"

// DefaultHomeDirName is the workspace directory created under $HOME.
const DefaultHomeDirName = ".loomwork"

// File names inside the workspace.
const (
	ConfigFileName  = "config.json"
	PresetsFileName = "presets.json"
	ProjectsDirName = "projects"
)

// Config is the workspace configuration document ($LOOMWORK_HOME/config.json).
type Config struct {
	// Providers maps a provider name (the selector's provider segment) to its
	// declaration. The name normally equals the provider kind.
	Providers map[string]provider.Config `json:"providers,omitempty"`
	// CueNote configures the cue-note client.
	CueNote cuenote.Config `json:"cuenote,omitempty"`
	// SystemPrompt is prepended to every prompt run unless overridden.
	SystemPrompt string `json:"systemPrompt,omitempty"`
}

// Home resolves the workspace directory: the override, then $LOOMWORK_HOME, then
// $HOME/.loomwork.
func Home(override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		return filepath.Clean(override), nil
	}
	if fromEnv := strings.TrimSpace(os.Getenv(EnvHome)); fromEnv != "" {
		return filepath.Clean(fromEnv), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home directory: %w", err)
	}
	return filepath.Join(home, DefaultHomeDirName), nil
}

// Paths describes resolved workspace locations.
type Paths struct {
	Home        string
	ConfigFile  string
	PresetsFile string
	ProjectsDir string
}

// ResolvePaths computes workspace paths for a home directory.
func ResolvePaths(home string) Paths {
	return Paths{
		Home:        home,
		ConfigFile:  filepath.Join(home, ConfigFileName),
		PresetsFile: filepath.Join(home, PresetsFileName),
		ProjectsDir: filepath.Join(home, ProjectsDirName),
	}
}

// Load reads the workspace config. A missing file yields defaults, so the tool
// works against local providers with no configuration at all.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Default(), nil
		}
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg = cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// Default returns the zero-configuration setup: the two local providers.
func Default() Config {
	return Config{
		Providers: map[string]provider.Config{
			string(provider.KindOllama):   {Kind: provider.KindOllama, BaseURL: provider.DefaultOllamaBaseURL},
			string(provider.KindLMStudio): {Kind: provider.KindLMStudio, BaseURL: provider.DefaultLMStudioBaseURL},
		},
		SystemPrompt: DefaultSystemPrompt,
	}
}

// DefaultSystemPrompt keeps generated artifacts terse and on-task.
const DefaultSystemPrompt = "You are Loomwork, an assistant that analyzes and transforms project artifacts. " +
	"Answer using only the supplied artifacts. Be precise and concise, and state explicitly when the artifacts do not contain the answer."

// WithDefaults fills in omitted sections.
func (c Config) WithDefaults() Config {
	if len(c.Providers) == 0 {
		c.Providers = Default().Providers
	}
	for name, declared := range c.Providers {
		if strings.TrimSpace(string(declared.Kind)) == "" {
			declared.Kind = provider.Kind(name)
			c.Providers[name] = declared
		}
	}
	if strings.TrimSpace(c.SystemPrompt) == "" {
		c.SystemPrompt = DefaultSystemPrompt
	}
	return c
}

// Validate checks every provider declaration.
func (c Config) Validate() error {
	for name, declared := range c.Providers {
		if err := declared.Validate(); err != nil {
			return fmt.Errorf("provider %q: %w", name, err)
		}
	}
	return nil
}

// ProviderNames lists configured provider names, sorted.
func (c Config) ProviderNames() []string {
	names := make([]string, 0, len(c.Providers))
	for name := range c.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ProviderConfig looks up a provider declaration by kind, falling back to a
// built-in default for local providers so no configuration is required.
func (c Config) ProviderConfig(kind provider.Kind) (provider.Config, error) {
	if declared, ok := c.Providers[string(kind)]; ok {
		return declared, nil
	}
	for _, declared := range c.Providers {
		if declared.Kind == kind {
			return declared, nil
		}
	}
	switch kind {
	case provider.KindOllama:
		return provider.Config{Kind: kind, BaseURL: provider.DefaultOllamaBaseURL}, nil
	case provider.KindLMStudio:
		return provider.Config{Kind: kind, BaseURL: provider.DefaultLMStudioBaseURL}, nil
	case provider.KindImGen:
		return provider.Config{Kind: kind, BaseURL: provider.DefaultImGenBaseURL}, nil
	default:
		return provider.Config{}, fmt.Errorf("provider %q is not configured in %s", kind, ConfigFileName)
	}
}
