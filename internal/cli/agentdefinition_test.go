package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
)

func TestAgentDefinitionLifecycleVerticalSlice(t *testing.T) {
	home := t.TempDir()
	exec(t, home, "project", "create", "--name", "orders")

	bodyPath := filepath.Join(t.TempDir(), "agent.md")
	if err := os.WriteFile(bodyPath, []byte("# Role\n\nYou generate REST API test cases.\n"), 0o644); err != nil {
		t.Fatalf("write body: %v", err)
	}
	var created model.AgentDefinition
	decodeJSON(t, exec(t, home, "agent-definition", "create", "--project", "orders",
		"--name", "rest-api-test-generator", "--model", "claude-sonnet-4-5",
		"--tools", "read_swagger,read_requirements", "--body-file", bodyPath,
		"--tags", "phase3", "--json"), &created)
	if created.Version != 1 || created.TargetProvider != model.AgentTargetClaudeSDK {
		t.Fatalf("created = %+v, want v1 targeting the Claude SDK by default", created)
	}
	if !created.AllowsTool("read_swagger") || created.AllowsTool("write_files") {
		t.Errorf("tools_allowed = %v, want a strict allowlist", created.ToolsAllowed)
	}

	var updated model.AgentDefinition
	decodeJSON(t, exec(t, home, "agent-definition", "update", "--project", "orders",
		"--name", "rest-api-test-generator", "--body", "# Role\n\nRevised instructions.", "--json"), &updated)
	if updated.Version != 2 || updated.Model != "claude-sonnet-4-5" {
		t.Fatalf("updated = %+v, want v2 inheriting the model", updated)
	}

	var first model.AgentDefinition
	decodeJSON(t, exec(t, home, "agent-definition", "show", "--project", "orders",
		"--name", "rest-api-test-generator", "--version", "1", "--json"), &first)
	if !strings.Contains(first.Body, "You generate REST API test cases") {
		t.Errorf("v1 body = %q, want the retained snapshot", first.Body)
	}

	markdown := exec(t, home, "agent-definition", "show", "--project", "orders", "--name", "rest-api-test-generator")
	for _, want := range []string{"agent_name: rest-api-test-generator", "version: 2", "target_provider: claude-agent-sdk", "Revised instructions."} {
		if !strings.Contains(markdown, want) {
			t.Errorf("markdown is missing %q:\n%s", want, markdown)
		}
	}

	var history []model.AgentDefinition
	decodeJSON(t, exec(t, home, "agent-definition", "show", "--project", "orders",
		"--name", "rest-api-test-generator", "--history", "--json"), &history)
	if len(history) != 2 || history[0].Version != 1 {
		t.Fatalf("history = %+v, want both versions oldest first", history)
	}
	if listed := exec(t, home, "agent-definition", "list", "--project", "orders"); !strings.Contains(listed, "v2") {
		t.Errorf("list output = %q", listed)
	}
}

func TestOverrideRuleLifecycleVerticalSlice(t *testing.T) {
	home := t.TempDir()
	exec(t, home, "project", "create", "--name", "orders")

	var created model.OverrideRule
	decodeJSON(t, exec(t, home, "agent-definition", "rule-create", "--project", "orders",
		"--rule", "empty-list-on-missing", "--title", "Missing collections return an empty list",
		"--methods", "GET", "--path", "/orders/*", "--scenario", "missing-item", "--spec-status", "404",
		"--action", "expect-empty-collection",
		"--rationale", "The storefront treats an absent order as an empty basket", "--json"), &created)
	if created.Version != 1 || created.Status != model.OverrideRuleStatusActive {
		t.Fatalf("created = %+v, want an active v1", created)
	}
	if created.Action.Kind != model.OverrideActionExpectEmptyCollection || created.Rationale == "" {
		t.Errorf("created = %+v, want the structured action alongside the rationale", created)
	}

	text := exec(t, home, "agent-definition", "rule-show", "--project", "orders", "--rule", "empty-list-on-missing")
	for _, want := range []string{"empty-list-on-missing-v1", "method GET", "path /orders/*", "spec says 404", "empty collection", "because:"} {
		if !strings.Contains(text, want) {
			t.Errorf("rule-show is missing %q:\n%s", want, text)
		}
	}

	var updated model.OverrideRule
	decodeJSON(t, exec(t, home, "agent-definition", "rule-update", "--project", "orders",
		"--rule", "empty-list-on-missing",
		"--rationale", "Checkout was rewritten; the empty basket is still the contract", "--json"), &updated)
	if updated.Version != 2 || updated.Action.Kind != model.OverrideActionExpectEmptyCollection {
		t.Fatalf("updated = %+v, want v2 inheriting the action", updated)
	}

	var active []model.OverrideRule
	decodeJSON(t, exec(t, home, "agent-definition", "rule-list", "--project", "orders", "--active", "--json"), &active)
	if len(active) != 1 || active[0].Version != 2 {
		t.Fatalf("active = %+v, want only the current version", active)
	}

	exec(t, home, "agent-definition", "rule-set-status", "--project", "orders",
		"--rule", "empty-list-on-missing", "--status", "obsolete")
	decodeJSON(t, exec(t, home, "agent-definition", "rule-list", "--project", "orders", "--active", "--json"), &active)
	if len(active) != 0 {
		t.Errorf("active = %+v, want none once the rule is obsolete", active)
	}
	if out := exec(t, home, "agent-definition", "rule-list", "--project", "orders"); !strings.Contains(out, "obsolete") {
		t.Errorf("rule-list output = %q, want the retained rule", out)
	}
}

func TestAgentDefinitionCommandsReportBadInput(t *testing.T) {
	home := t.TempDir()
	exec(t, home, "project", "create", "--name", "orders")

	if got := execErr(t, home, "agent-definition", "create", "--project", "orders"); !strings.Contains(got, "are required") {
		t.Errorf("error = %q", got)
	}
	if got := execErr(t, home, "agent-definition", "create", "--project", "orders", "--name", "a"); !strings.Contains(got, "--body") {
		t.Errorf("error = %q", got)
	}
	if got := execErr(t, home, "agent-definition", "create", "--project", "orders", "--name", "a",
		"--body", "x", "--body-file", "/nope.md"); !strings.Contains(got, "not both") {
		t.Errorf("error = %q", got)
	}
	if got := execErr(t, home, "agent-definition", "create", "--project", "orders", "--name", "agent",
		"--target", "gemini-sdk", "--body", "# Role"); !strings.Contains(got, "unknown") {
		t.Errorf("error = %q", got)
	}
	if got := execErr(t, home, "agent-definition", "show", "--project", "orders", "--name", "absent"); !strings.Contains(got, "not found") {
		t.Errorf("error = %q", got)
	}
	if got := execErr(t, home, "agent-definition", "rule-create", "--project", "orders",
		"--rule", "no-rationale", "--title", "Missing rationale"); !strings.Contains(got, "rationale") {
		t.Errorf("error = %q, want the rationale enforced", got)
	}
	if out := exec(t, home, "agent-definition", "list", "--project", "orders"); !strings.Contains(out, "no agent definitions") {
		t.Errorf("output = %q", out)
	}
	if out := exec(t, home, "agent-definition", "rule-list", "--project", "orders"); !strings.Contains(out, "no override rules") {
		t.Errorf("output = %q", out)
	}
}

func TestUsageDocumentsTheNewGroups(t *testing.T) {
	out := exec(t, t.TempDir(), "help")
	for _, want := range []string{"agent-definition", "test-suite", "generate", "rule-create", "import"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage is missing %q", want)
		}
	}
}
