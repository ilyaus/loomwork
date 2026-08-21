package model

import (
	"bufio"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// sortedMetadataKeys orders metadata keys so rendered files are stable.
func sortedMetadataKeys(metadata map[string]string) []string {
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// AgentTarget names the agent adapter backend that runs a definition. It names
// an adapter, not a model: the adapter normalizes tool registration and
// structured output so a definition can move between backends unchanged.
type AgentTarget string

const (
	// AgentTargetClaudeSDK runs the definition through the Claude Agent SDK.
	AgentTargetClaudeSDK AgentTarget = "claude-agent-sdk"
	// AgentTargetCopilotSDK runs the definition through the Copilot SDK. No
	// adapter implements it yet; the value exists so definitions can already
	// declare it.
	AgentTargetCopilotSDK AgentTarget = "copilot-sdk"
)

// AgentTargets lists every supported agent backend.
func AgentTargets() []AgentTarget {
	return []AgentTarget{AgentTargetClaudeSDK, AgentTargetCopilotSDK}
}

// ParseAgentTarget validates a raw target provider string.
func ParseAgentTarget(raw string) (AgentTarget, error) {
	candidate := AgentTarget(strings.TrimSpace(strings.ToLower(raw)))
	for _, known := range AgentTargets() {
		if candidate == known {
			return known, nil
		}
	}
	return "", fmt.Errorf("unknown agent target %q: supported targets are %s", raw, joinStrings(AgentTargets()))
}

// agentNamePattern constrains names that are also file-name stems.
var agentNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// toolNamePattern constrains normalized tool names.
var toolNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]*$`)

// AgentDefinition is one immutable version of an agent definition: markdown
// instructions plus the frontmatter that declares how they are executed. Its
// JSON shape is fixed by docs/schemas/agent-definition.schema.json; its on-disk
// form is agent-definitions/<agent_name>.v<version>.md (see Markdown).
type AgentDefinition struct {
	AgentName      string            `json:"agent_name"`
	Version        int               `json:"version"`
	TargetProvider AgentTarget       `json:"target_provider"`
	Model          string            `json:"model,omitempty"`
	ToolsAllowed   []string          `json:"tools_allowed,omitempty"`
	Body           string            `json:"body"`
	Description    string            `json:"description,omitempty"`
	Tags           []string          `json:"tags,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
}

// AgentDefinitionSpec describes a version to be created. The store assigns the
// version number and timestamp.
type AgentDefinitionSpec struct {
	AgentName      string
	TargetProvider AgentTarget
	Model          string
	ToolsAllowed   []string
	Body           string
	Description    string
	Tags           []string
	Metadata       map[string]string
}

func (s AgentDefinitionSpec) normalize() (AgentDefinitionSpec, error) {
	s.AgentName = strings.TrimSpace(strings.ToLower(s.AgentName))
	if !agentNamePattern.MatchString(s.AgentName) {
		return AgentDefinitionSpec{}, fmt.Errorf("agent name %q must be lowercase letters, digits, and dashes", s.AgentName)
	}
	target, err := ParseAgentTarget(string(s.TargetProvider))
	if err != nil {
		return AgentDefinitionSpec{}, err
	}
	s.TargetProvider = target
	s.Model = strings.TrimSpace(s.Model)
	s.Body = strings.TrimSpace(s.Body)
	if s.Body == "" {
		return AgentDefinitionSpec{}, fmt.Errorf("agent definition %q needs a markdown body", s.AgentName)
	}
	s.Description = strings.TrimSpace(s.Description)
	tools := make([]string, 0, len(s.ToolsAllowed))
	seen := map[string]bool{}
	for _, tool := range s.ToolsAllowed {
		normalized := strings.TrimSpace(strings.ToLower(tool))
		if normalized == "" {
			continue
		}
		if !toolNamePattern.MatchString(normalized) {
			return AgentDefinitionSpec{}, fmt.Errorf("tool name %q must be lowercase letters, digits, and underscores", tool)
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		tools = append(tools, normalized)
	}
	s.ToolsAllowed = tools
	return s, nil
}

// NewAgentDefinition builds the first version of an agent definition.
func NewAgentDefinition(spec AgentDefinitionSpec) (*AgentDefinition, error) {
	normalized, err := spec.normalize()
	if err != nil {
		return nil, err
	}
	return &AgentDefinition{
		AgentName:      normalized.AgentName,
		Version:        1,
		TargetProvider: normalized.TargetProvider,
		Model:          normalized.Model,
		ToolsAllowed:   normalized.ToolsAllowed,
		Body:           normalized.Body,
		Description:    normalized.Description,
		Tags:           normalizeTags(normalized.Tags),
		Metadata:       copyMetadata(normalized.Metadata),
		CreatedAt:      nowFunc().UTC(),
	}, nil
}

// NextVersion returns the next version of a definition. Fields the spec leaves
// empty are inherited, so a body-only edit keeps the target and tool allowlist.
// The receiver is not modified.
func (a *AgentDefinition) NextVersion(spec AgentDefinitionSpec) (*AgentDefinition, error) {
	spec.AgentName = a.AgentName
	if strings.TrimSpace(string(spec.TargetProvider)) == "" {
		spec.TargetProvider = a.TargetProvider
	}
	if strings.TrimSpace(spec.Model) == "" {
		spec.Model = a.Model
	}
	if strings.TrimSpace(spec.Body) == "" {
		spec.Body = a.Body
	}
	if strings.TrimSpace(spec.Description) == "" {
		spec.Description = a.Description
	}
	if len(spec.ToolsAllowed) == 0 {
		spec.ToolsAllowed = a.ToolsAllowed
	}
	if len(spec.Tags) == 0 {
		spec.Tags = a.Tags
	}
	if len(spec.Metadata) == 0 {
		spec.Metadata = a.Metadata
	}
	next, err := NewAgentDefinition(spec)
	if err != nil {
		return nil, err
	}
	next.Version = a.Version + 1
	return next, nil
}

// Ref is the citation form of one definition version, as used by a generated
// suite's agent_definition field.
func (a AgentDefinition) Ref() string {
	return fmt.Sprintf("%s-v%d", a.AgentName, a.Version)
}

// AllowsTool reports whether the definition permits a tool name. The list is a
// strict allowlist: a definition that names no tools gets none, because a tool
// an agent may call is a capability a QA engineer has to grant deliberately.
func (a AgentDefinition) AllowsTool(name string) bool {
	wanted := strings.TrimSpace(strings.ToLower(name))
	for _, tool := range a.ToolsAllowed {
		if tool == wanted {
			return true
		}
	}
	return false
}

// Markdown renders the definition in its on-disk form: a frontmatter block
// followed by the markdown body. The frontmatter is a deliberately small
// key/value dialect (scalars, [bracketed, lists], and metadata.<key> entries)
// rather than general YAML, so the file stays hand-editable without pulling in a
// YAML dependency to read it back.
func (a AgentDefinition) Markdown() string {
	var builder strings.Builder
	builder.WriteString("---\n")
	fmt.Fprintf(&builder, "agent_name: %s\n", a.AgentName)
	fmt.Fprintf(&builder, "version: %d\n", a.Version)
	fmt.Fprintf(&builder, "target_provider: %s\n", a.TargetProvider)
	if a.Model != "" {
		fmt.Fprintf(&builder, "model: %s\n", a.Model)
	}
	if len(a.ToolsAllowed) > 0 {
		fmt.Fprintf(&builder, "tools_allowed: [%s]\n", strings.Join(a.ToolsAllowed, ", "))
	}
	if a.Description != "" {
		fmt.Fprintf(&builder, "description: %s\n", a.Description)
	}
	if len(a.Tags) > 0 {
		fmt.Fprintf(&builder, "tags: [%s]\n", strings.Join(a.Tags, ", "))
	}
	for _, key := range sortedMetadataKeys(a.Metadata) {
		fmt.Fprintf(&builder, "metadata.%s: %s\n", key, a.Metadata[key])
	}
	fmt.Fprintf(&builder, "created_at: %s\n", a.CreatedAt.UTC().Format(time.RFC3339Nano))
	builder.WriteString("---\n\n")
	builder.WriteString(a.Body)
	builder.WriteString("\n")
	return builder.String()
}

// ParseAgentDefinitionMarkdown reads the on-disk form produced by Markdown. An
// unknown frontmatter key is an error rather than a silent drop, because a typo
// in target_provider or tools_allowed would otherwise change how an agent runs.
func ParseAgentDefinitionMarkdown(raw []byte) (*AgentDefinition, error) {
	frontmatter, body, err := splitFrontmatter(string(raw))
	if err != nil {
		return nil, err
	}

	definition := AgentDefinition{Body: strings.TrimSpace(body)}
	scanner := bufio.NewScanner(strings.NewReader(frontmatter))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, ":")
		if !found {
			return nil, fmt.Errorf("agent definition frontmatter line %q is not key: value", line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if metadataKey, ok := strings.CutPrefix(key, "metadata."); ok {
			if definition.Metadata == nil {
				definition.Metadata = map[string]string{}
			}
			definition.Metadata[metadataKey] = value
			continue
		}
		switch key {
		case "agent_name":
			definition.AgentName = value
		case "version":
			version, err := strconv.Atoi(strings.TrimPrefix(value, "v"))
			if err != nil {
				return nil, fmt.Errorf("agent definition version %q is not a number", value)
			}
			definition.Version = version
		case "target_provider", "target":
			target, err := ParseAgentTarget(value)
			if err != nil {
				return nil, err
			}
			definition.TargetProvider = target
		case "model":
			definition.Model = value
		case "tools_allowed":
			definition.ToolsAllowed = parseBracketList(value)
		case "description":
			definition.Description = value
		case "tags":
			definition.Tags = parseBracketList(value)
		case "created_at":
			created, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return nil, fmt.Errorf("agent definition created_at %q is not RFC 3339: %w", value, err)
			}
			definition.CreatedAt = created.UTC()
		default:
			return nil, fmt.Errorf("unknown agent definition frontmatter key %q", key)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read agent definition frontmatter: %w", err)
	}
	if definition.Version < 1 {
		return nil, fmt.Errorf("agent definition %q needs a version of 1 or greater", definition.AgentName)
	}

	// Reuse the constructor's validation, then restore the parsed identity.
	validated, err := NewAgentDefinition(AgentDefinitionSpec{
		AgentName:      definition.AgentName,
		TargetProvider: definition.TargetProvider,
		Model:          definition.Model,
		ToolsAllowed:   definition.ToolsAllowed,
		Body:           definition.Body,
		Description:    definition.Description,
		Tags:           definition.Tags,
		Metadata:       definition.Metadata,
	})
	if err != nil {
		return nil, err
	}
	validated.Version = definition.Version
	if !definition.CreatedAt.IsZero() {
		validated.CreatedAt = definition.CreatedAt
	}
	return validated, nil
}

// splitFrontmatter separates a leading --- delimited block from the body.
func splitFrontmatter(text string) (string, string, error) {
	trimmed := strings.TrimLeft(text, "\ufeff \t\r\n")
	if !strings.HasPrefix(trimmed, "---") {
		return "", "", fmt.Errorf("agent definition must start with a --- frontmatter block")
	}
	rest := strings.TrimPrefix(trimmed, "---")
	rest = strings.TrimLeft(rest, "\r\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", fmt.Errorf("agent definition frontmatter block is not closed with ---")
	}
	frontmatter := rest[:end]
	body := rest[end+len("\n---"):]
	if newline := strings.Index(body, "\n"); newline >= 0 {
		body = body[newline+1:]
	} else {
		body = ""
	}
	return frontmatter, body, nil
}

// parseBracketList reads `[a, b]` or a bare comma-separated list.
func parseBracketList(value string) []string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.TrimPrefix(trimmed, "[")
	trimmed = strings.TrimSuffix(trimmed, "]")
	parts := strings.Split(trimmed, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if cleaned := strings.Trim(strings.TrimSpace(part), `"'`); cleaned != "" {
			values = append(values, cleaned)
		}
	}
	return values
}
