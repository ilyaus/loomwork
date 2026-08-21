package model

import (
	"strings"
	"testing"
)

func testAgentSpec() AgentDefinitionSpec {
	return AgentDefinitionSpec{
		AgentName:      "rest-api-test-generator",
		TargetProvider: AgentTargetClaudeSDK,
		Model:          "claude-sonnet-4-5",
		ToolsAllowed:   []string{"read_swagger", "read_requirements"},
		Description:    "Generates REST API test suites",
		Tags:           []string{"phase3"},
		Body:           "# Role\n\nYou generate REST API test cases.\n",
	}
}

func TestNewAgentDefinitionStartsAtVersionOne(t *testing.T) {
	definition, err := NewAgentDefinition(testAgentSpec())
	if err != nil {
		t.Fatalf("NewAgentDefinition: %v", err)
	}
	if definition.Version != 1 {
		t.Errorf("Version = %d, want 1", definition.Version)
	}
	if definition.Ref() != "rest-api-test-generator-v1" {
		t.Errorf("Ref = %q", definition.Ref())
	}
	if definition.CreatedAt.IsZero() {
		t.Error("CreatedAt was not set")
	}
}

func TestNewAgentDefinitionRejectsInvalidInput(t *testing.T) {
	cases := map[string]func(*AgentDefinitionSpec){
		"empty name":     func(s *AgentDefinitionSpec) { s.AgentName = "" },
		"spaced name":    func(s *AgentDefinitionSpec) { s.AgentName = "rest generator" },
		"empty body":     func(s *AgentDefinitionSpec) { s.Body = "  " },
		"unknown target": func(s *AgentDefinitionSpec) { s.TargetProvider = "gemini-sdk" },
		"bad tool name":  func(s *AgentDefinitionSpec) { s.ToolsAllowed = []string{"read swagger"} },
	}
	for name, mutate := range cases {
		spec := testAgentSpec()
		mutate(&spec)
		if _, err := NewAgentDefinition(spec); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestAgentDefinitionNextVersionKeepsUnchangedFields(t *testing.T) {
	first, err := NewAgentDefinition(testAgentSpec())
	if err != nil {
		t.Fatalf("NewAgentDefinition: %v", err)
	}
	second, err := first.NextVersion(AgentDefinitionSpec{Body: "# Role\n\nRevised instructions.\n"})
	if err != nil {
		t.Fatalf("NextVersion: %v", err)
	}
	if second.Version != 2 {
		t.Errorf("Version = %d, want 2", second.Version)
	}
	if second.AgentName != first.AgentName || second.TargetProvider != first.TargetProvider || second.Model != first.Model {
		t.Errorf("identity fields changed: %+v", second)
	}
	if strings.Join(second.ToolsAllowed, ",") != strings.Join(first.ToolsAllowed, ",") {
		t.Errorf("ToolsAllowed = %v, want %v", second.ToolsAllowed, first.ToolsAllowed)
	}
	if second.Body == first.Body {
		t.Error("body was not replaced")
	}
	if first.Version != 1 {
		t.Errorf("earlier version mutated: %d", first.Version)
	}
}

func TestAgentDefinitionMarkdownRoundTrip(t *testing.T) {
	definition, err := NewAgentDefinition(testAgentSpec())
	if err != nil {
		t.Fatalf("NewAgentDefinition: %v", err)
	}
	rendered := definition.Markdown()
	if !strings.HasPrefix(rendered, "---\n") {
		t.Fatalf("markdown has no frontmatter:\n%s", rendered)
	}
	parsed, err := ParseAgentDefinitionMarkdown([]byte(rendered))
	if err != nil {
		t.Fatalf("ParseAgentDefinitionMarkdown: %v", err)
	}
	if parsed.AgentName != definition.AgentName || parsed.Version != definition.Version {
		t.Errorf("identity lost: %+v", parsed)
	}
	if parsed.TargetProvider != definition.TargetProvider || parsed.Model != definition.Model {
		t.Errorf("target lost: %+v", parsed)
	}
	if strings.Join(parsed.ToolsAllowed, ",") != strings.Join(definition.ToolsAllowed, ",") {
		t.Errorf("ToolsAllowed = %v, want %v", parsed.ToolsAllowed, definition.ToolsAllowed)
	}
	if strings.TrimSpace(parsed.Body) != strings.TrimSpace(definition.Body) {
		t.Errorf("Body = %q, want %q", parsed.Body, definition.Body)
	}
	if !parsed.CreatedAt.Equal(definition.CreatedAt) {
		t.Errorf("CreatedAt = %s, want %s", parsed.CreatedAt, definition.CreatedAt)
	}
	if !strings.Contains(rendered, "version: 1\n") {
		t.Errorf("version is not the integer the schema declares:\n%s", rendered)
	}

	// A hand-written file may spell the version the way the file name does.
	prefixed := strings.Replace(rendered, "version: 1", "version: v1", 1)
	if _, err := ParseAgentDefinitionMarkdown([]byte(prefixed)); err != nil {
		t.Errorf("ParseAgentDefinitionMarkdown(v-prefixed): %v", err)
	}
}

func TestAgentDefinitionToolsAllowedIsAStrictAllowlist(t *testing.T) {
	spec := testAgentSpec()
	spec.ToolsAllowed = nil
	definition, err := NewAgentDefinition(spec)
	if err != nil {
		t.Fatalf("NewAgentDefinition: %v", err)
	}
	if definition.AllowsTool("read_swagger") {
		t.Error("a definition naming no tools must grant none")
	}

	restricted, err := NewAgentDefinition(testAgentSpec())
	if err != nil {
		t.Fatalf("NewAgentDefinition: %v", err)
	}
	if !restricted.AllowsTool("read_requirements") {
		t.Error("listed tool was denied")
	}
	if restricted.AllowsTool("read_override_rules") {
		t.Error("unlisted tool was allowed")
	}
}
