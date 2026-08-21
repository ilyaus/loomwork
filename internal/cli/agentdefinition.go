package cli

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ilyaus/loomwork/internal/model"
)

func agentDefinitionCreate(e *env, args []string) error {
	var projectRef, name, target, modelID, body, bodyFile, tools, description, tags string
	err := e.parse("agent-definition create", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&name, "name", "", "agent name, e.g. rest-api-test-generator (required)")
		flags.StringVar(&target, "target", string(model.AgentTargetClaudeSDK), "target agent adapter: claude-agent-sdk|copilot-sdk")
		flags.StringVar(&modelID, "model", "", "model the agent should run on")
		flags.StringVar(&body, "body", "", "markdown body of the definition")
		flags.StringVar(&bodyFile, "body-file", "", "read the markdown body from a file")
		flags.StringVar(&tools, "tools", "", "comma-separated allowed tools, e.g. read_swagger,read_requirements")
		flags.StringVar(&description, "description", "", "one-line description")
		flags.StringVar(&tags, "tags", "", "comma-separated tags")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(name) == "" {
		return fmt.Errorf("agent-definition create: --project and --name are required")
	}
	content, err := readBody(body, bodyFile, "agent-definition create")
	if err != nil {
		return err
	}
	definition, err := e.store.CreateAgentDefinition(projectRef, model.AgentDefinitionSpec{
		AgentName:      name,
		TargetProvider: model.AgentTarget(target),
		Model:          modelID,
		ToolsAllowed:   splitList(tools),
		Body:           content,
		Description:    description,
		Tags:           splitList(tags),
	})
	if err != nil {
		return err
	}
	return emitAgentDefinition(e, definition)
}

func agentDefinitionUpdate(e *env, args []string) error {
	var projectRef, name, target, modelID, body, bodyFile, tools, description, tags string
	err := e.parse("agent-definition update", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&name, "name", "", "agent name (required)")
		flags.StringVar(&target, "target", "", "change the target agent adapter")
		flags.StringVar(&modelID, "model", "", "change the model")
		flags.StringVar(&body, "body", "", "new markdown body")
		flags.StringVar(&bodyFile, "body-file", "", "read the new markdown body from a file")
		flags.StringVar(&tools, "tools", "", "replace the allowed tools")
		flags.StringVar(&description, "description", "", "change the description")
		flags.StringVar(&tags, "tags", "", "replace the tags")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(name) == "" {
		return fmt.Errorf("agent-definition update: --project and --name are required")
	}
	content := body
	if strings.TrimSpace(bodyFile) != "" {
		content, err = readBody(body, bodyFile, "agent-definition update")
		if err != nil {
			return err
		}
	}
	definition, err := e.store.UpdateAgentDefinition(projectRef, name, model.AgentDefinitionSpec{
		TargetProvider: model.AgentTarget(target),
		Model:          modelID,
		ToolsAllowed:   splitList(tools),
		Body:           content,
		Description:    description,
		Tags:           splitList(tags),
	})
	if err != nil {
		return err
	}
	return emitAgentDefinition(e, definition)
}

func agentDefinitionList(e *env, args []string) error {
	var projectRef string
	err := e.parse("agent-definition list", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" {
		return fmt.Errorf("agent-definition list: --project is required")
	}
	definitions, err := e.store.ListAgentDefinitions(projectRef)
	if err != nil {
		return err
	}
	if len(definitions) == 0 {
		return e.emit(definitions, "no agent definitions")
	}
	var builder strings.Builder
	for _, definition := range definitions {
		fmt.Fprintf(&builder, "%s\tv%d\t%s", definition.AgentName, definition.Version, definition.TargetProvider)
		if len(definition.ToolsAllowed) > 0 {
			fmt.Fprintf(&builder, "\ttools: %s", strings.Join(definition.ToolsAllowed, ","))
		}
		builder.WriteString("\n")
	}
	return e.emit(definitions, strings.TrimRight(builder.String(), "\n"))
}

func agentDefinitionShow(e *env, args []string) error {
	var projectRef, name string
	var version int
	var history bool
	err := e.parse("agent-definition show", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&name, "name", "", "agent name (required)")
		flags.IntVar(&version, "version", 0, "version to show (default current)")
		flags.BoolVar(&history, "history", false, "list every retained version instead")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(name) == "" {
		return fmt.Errorf("agent-definition show: --project and --name are required")
	}
	if version < 0 {
		return fmt.Errorf("agent-definition show: --version must be 1 or greater (0 or omitted shows the current version)")
	}
	if history {
		versions, err := e.store.AgentDefinitionHistory(projectRef, name)
		if err != nil {
			return err
		}
		var builder strings.Builder
		for _, definition := range versions {
			fmt.Fprintf(&builder, "v%d\t%s\t%s\n", definition.Version, definition.TargetProvider, definition.CreatedAt.Format("2006-01-02T15:04:05Z"))
		}
		return e.emit(versions, strings.TrimRight(builder.String(), "\n"))
	}
	definition, err := e.store.LoadAgentDefinition(projectRef, name, version)
	if err != nil {
		return err
	}
	return emitAgentDefinition(e, definition)
}

func emitAgentDefinition(e *env, definition *model.AgentDefinition) error {
	return e.emit(definition, definition.Markdown())
}

func overrideRuleCreate(e *env, args []string) error {
	var projectRef, id, title, methods, pathPattern, scenario, action, rationale, tags string
	var expectStatus, specStatus int
	err := e.parse("agent-definition rule-create", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&id, "rule", "", "rule id, e.g. empty-list-on-missing (required)")
		flags.StringVar(&title, "title", "", "short title (required)")
		flags.StringVar(&methods, "methods", "", "comma-separated HTTP methods the rule applies to")
		flags.StringVar(&pathPattern, "path", "", "path glob the rule applies to, e.g. /orders/*")
		flags.StringVar(&scenario, "scenario", "", "scenario the rule applies to: "+scenarioNames())
		flags.IntVar(&specStatus, "spec-status", 0, "status the spec literally states, for the audit trail")
		flags.StringVar(&action, "action", string(model.OverrideActionExpectStatus), "action: expect-status|expect-empty-collection|skip-test")
		flags.IntVar(&expectStatus, "expect-status", 0, "status the rule requires instead")
		flags.StringVar(&rationale, "rationale", "", "business reasoning the agent must apply (required)")
		flags.StringVar(&tags, "tags", "", "comma-separated tags")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(id) == "" {
		return fmt.Errorf("agent-definition rule-create: --project and --rule are required")
	}
	rule, err := e.store.CreateOverrideRule(projectRef, model.OverrideRuleSpec{
		ID:        id,
		Title:     title,
		Condition: buildCondition(methods, pathPattern, scenario, specStatus),
		Action:    model.OverrideAction{Kind: model.OverrideActionKind(action), ExpectStatus: expectStatus},
		Rationale: rationale,
		Tags:      splitList(tags),
	})
	if err != nil {
		return err
	}
	return emitOverrideRule(e, rule)
}

func overrideRuleUpdate(e *env, args []string) error {
	var projectRef, id, title, methods, pathPattern, scenario, action, rationale, tags string
	var expectStatus, specStatus int
	err := e.parse("agent-definition rule-update", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&id, "rule", "", "rule id (required)")
		flags.StringVar(&title, "title", "", "change the title")
		flags.StringVar(&methods, "methods", "", "replace the HTTP methods")
		flags.StringVar(&pathPattern, "path", "", "replace the path glob")
		flags.StringVar(&scenario, "scenario", "", "replace the scenario")
		flags.IntVar(&specStatus, "spec-status", 0, "replace the literal spec status")
		flags.StringVar(&action, "action", "", "replace the action kind")
		flags.IntVar(&expectStatus, "expect-status", 0, "replace the required status")
		flags.StringVar(&rationale, "rationale", "", "replace the rationale")
		flags.StringVar(&tags, "tags", "", "replace the tags")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(id) == "" {
		return fmt.Errorf("agent-definition rule-update: --project and --rule are required")
	}
	rule, err := e.store.UpdateOverrideRule(projectRef, id, model.OverrideRuleSpec{
		Title:     title,
		Condition: buildCondition(methods, pathPattern, scenario, specStatus),
		Action:    model.OverrideAction{Kind: model.OverrideActionKind(action), ExpectStatus: expectStatus},
		Rationale: rationale,
		Tags:      splitList(tags),
	})
	if err != nil {
		return err
	}
	return emitOverrideRule(e, rule)
}

func overrideRuleSetStatus(e *env, args []string) error {
	var projectRef, id, status string
	var version int
	err := e.parse("agent-definition rule-set-status", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&id, "rule", "", "rule id (required)")
		flags.StringVar(&status, "status", "", "active|obsolete (required)")
		flags.IntVar(&version, "version", 0, "version to change (default current)")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(id) == "" || strings.TrimSpace(status) == "" {
		return fmt.Errorf("agent-definition rule-set-status: --project, --rule, and --status are required")
	}
	if version < 0 {
		return fmt.Errorf("agent-definition rule-set-status: --version must be 1 or greater (0 or omitted changes the current version)")
	}
	parsed, err := model.ParseOverrideRuleStatus(status)
	if err != nil {
		return err
	}
	rule, err := e.store.SetOverrideRuleStatus(projectRef, id, version, parsed)
	if err != nil {
		return err
	}
	return emitOverrideRule(e, rule)
}

func overrideRuleList(e *env, args []string) error {
	var projectRef string
	var activeOnly bool
	err := e.parse("agent-definition rule-list", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.BoolVar(&activeOnly, "active", false, "list only the rules that shape a new generation")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" {
		return fmt.Errorf("agent-definition rule-list: --project is required")
	}
	var rules []*model.OverrideRule
	if activeOnly {
		rules, err = e.store.ActiveOverrideRules(projectRef)
	} else {
		rules, err = e.store.ListOverrideRules(projectRef)
	}
	if err != nil {
		return err
	}
	if len(rules) == 0 {
		return e.emit(rules, "no override rules")
	}
	var builder strings.Builder
	for _, rule := range rules {
		fmt.Fprintf(&builder, "%s\t%s\t%s\t%s\n", rule.Ref(), rule.Status, describeRuleAction(*rule), rule.Title)
	}
	return e.emit(rules, strings.TrimRight(builder.String(), "\n"))
}

func overrideRuleShow(e *env, args []string) error {
	var projectRef, id string
	var version int
	var history bool
	err := e.parse("agent-definition rule-show", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&id, "rule", "", "rule id (required)")
		flags.IntVar(&version, "version", 0, "version to show (default current)")
		flags.BoolVar(&history, "history", false, "list every retained version instead")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(id) == "" {
		return fmt.Errorf("agent-definition rule-show: --project and --rule are required")
	}
	if version < 0 {
		return fmt.Errorf("agent-definition rule-show: --version must be 1 or greater (0 or omitted shows the current version)")
	}
	if history {
		versions, err := e.store.OverrideRuleHistory(projectRef, id)
		if err != nil {
			return err
		}
		var builder strings.Builder
		for _, rule := range versions {
			fmt.Fprintf(&builder, "%s\t%s\t%s\n", rule.Ref(), rule.Status, rule.Title)
		}
		return e.emit(versions, strings.TrimRight(builder.String(), "\n"))
	}
	rule, err := e.store.LoadOverrideRule(projectRef, id, version)
	if err != nil {
		return err
	}
	return emitOverrideRule(e, rule)
}

func emitOverrideRule(e *env, rule *model.OverrideRule) error {
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s\t%s\n%s\n", rule.Ref(), rule.Status, rule.Title)
	fmt.Fprintf(&builder, "when: %s\n", describeCondition(rule.Condition))
	fmt.Fprintf(&builder, "then: %s\n", describeRuleAction(*rule))
	fmt.Fprintf(&builder, "because: %s", rule.Rationale)
	return e.emit(rule, builder.String())
}

func buildCondition(methods, pathPattern, scenario string, specStatus int) model.OverrideCondition {
	condition := model.OverrideCondition{
		PathPattern: pathPattern,
		Scenario:    model.TestScenario(scenario),
		SpecStatus:  specStatus,
	}
	for _, raw := range splitList(methods) {
		condition.Methods = append(condition.Methods, model.HTTPMethod(raw))
	}
	return condition
}

func describeCondition(condition model.OverrideCondition) string {
	if condition.IsEmpty() {
		return "every test case"
	}
	parts := make([]string, 0, 4)
	if len(condition.Methods) > 0 {
		names := make([]string, 0, len(condition.Methods))
		for _, method := range condition.Methods {
			names = append(names, string(method))
		}
		parts = append(parts, "method "+strings.Join(names, "|"))
	}
	if condition.PathPattern != "" {
		parts = append(parts, "path "+condition.PathPattern)
	}
	if condition.Scenario != "" {
		parts = append(parts, "scenario "+string(condition.Scenario))
	}
	if condition.SpecStatus != 0 {
		parts = append(parts, "spec says "+strconv.Itoa(condition.SpecStatus))
	}
	return strings.Join(parts, ", ")
}

func describeRuleAction(rule model.OverrideRule) string {
	switch rule.Action.Kind {
	case model.OverrideActionSkipTest:
		return "do not test"
	case model.OverrideActionExpectEmptyCollection:
		return fmt.Sprintf("expect %d with an empty collection", rule.Action.Status())
	default:
		return fmt.Sprintf("expect %d", rule.Action.Status())
	}
}

func scenarioNames() string {
	names := make([]string, 0, len(model.TestScenarios()))
	for _, scenario := range model.TestScenarios() {
		names = append(names, string(scenario))
	}
	return strings.Join(names, "|")
}

// readBody resolves a markdown body from either an inline flag or a file.
func readBody(body, file, command string) (string, error) {
	if strings.TrimSpace(file) != "" {
		if strings.TrimSpace(body) != "" {
			return "", fmt.Errorf("%s: pass --body or --body-file, not both", command)
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read --body-file %s: %w", file, err)
		}
		return string(raw), nil
	}
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("%s: --body or --body-file is required", command)
	}
	return body, nil
}
