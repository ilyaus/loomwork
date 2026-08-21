package testgen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/provider"
)

// Tool names an agent definition may allow. They are read-only: generation gives
// an agent the inputs the vision names — spec, requirements, override rules,
// templates — and nothing that writes.
const (
	ToolReadSwagger       = "read_swagger"
	ToolReadRequirements  = "read_requirements"
	ToolReadOverrideRules = "read_override_rules"
	ToolReadTemplates     = "read_test_templates"
)

// GenerationTools lists every tool generation can offer.
func GenerationTools() []string {
	return []string{ToolReadSwagger, ToolReadRequirements, ToolReadOverrideRules, ToolReadTemplates}
}

type namedDocument struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// agentInputs is the material one generation run hands to an agent. The same
// values are inlined in the prompt and served by the tools: an agent that cannot
// or will not call tools still sees every input, and one that prefers tools can
// re-read a large spec without the prompt carrying it twice in its context.
type agentInputs struct {
	spec         string
	specPath     string
	requirements []*model.Requirement
	rules        []*model.OverrideRule
	templates    []namedDocument
}

// tools returns the tools the definition allows, in normalized form. A definition
// that allows nothing gets no tools; the prompt still carries every input.
func (i agentInputs) tools(definition model.AgentDefinition) []provider.ToolDefinition {
	candidates := []provider.ToolDefinition{
		{
			Name:        ToolReadSwagger,
			Description: fmt.Sprintf("Return the full OpenAPI/Swagger specification under test (%s).", i.specPath),
			Handler:     staticTool(i.spec),
		},
		{
			Name:        ToolReadRequirements,
			Description: "Return the project's current-version requirements as JSON, each with its id and version.",
			Handler:     jsonTool(requirementViews(i.requirements)),
		},
		{
			Name:        ToolReadOverrideRules,
			Description: "Return the project's active override rules as JSON: structured condition and action plus the rationale to reason from.",
			Handler:     jsonTool(ruleViews(i.rules)),
		},
		{
			Name:        ToolReadTemplates,
			Description: "Return the test templates supplied for this run as JSON, each with its file name and content.",
			Handler:     jsonTool(i.templates),
		},
	}
	tools := make([]provider.ToolDefinition, 0, len(candidates))
	for _, tool := range candidates {
		if definition.AllowsTool(tool.Name) {
			tools = append(tools, tool)
		}
	}
	return tools
}

func staticTool(content string) provider.ToolHandler {
	return func(context.Context, json.RawMessage) (provider.ToolResult, error) {
		return provider.ToolResult{Content: content}, nil
	}
}

func jsonTool(value any) provider.ToolHandler {
	return func(context.Context, json.RawMessage) (provider.ToolResult, error) {
		return provider.ToolResult{Content: encodeJSON(value)}, nil
	}
}

// requirementView is the requirement shape an agent sees: the text plus the
// exact version a generated case is traceable to.
type requirementView struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	Text    string `json:"text"`
}

func requirementViews(requirements []*model.Requirement) []requirementView {
	views := make([]requirementView, 0, len(requirements))
	for _, requirement := range requirements {
		views = append(views, requirementView{ID: requirement.ID, Version: requirement.Version, Text: requirement.Text})
	}
	return views
}

// ruleView is the override rule shape an agent sees. The reference is the exact
// string a case must record in overrides_applied, so the agent cannot invent a
// citation format.
type ruleView struct {
	Reference string                  `json:"reference"`
	Title     string                  `json:"title"`
	Condition model.OverrideCondition `json:"condition"`
	Action    model.OverrideAction    `json:"action"`
	Rationale string                  `json:"rationale"`
}

func ruleViews(rules []*model.OverrideRule) []ruleView {
	views := make([]ruleView, 0, len(rules))
	for _, rule := range rules {
		views = append(views, ruleView{
			Reference: rule.Ref(),
			Title:     rule.Title,
			Condition: rule.Condition,
			Action:    rule.Action,
			Rationale: rule.Rationale,
		})
	}
	return views
}

// prompt builds the generation turn. The agent definition body is the system
// prompt, so this states only the task and the inputs; the reasoning rules live
// in the versioned definition where a QA engineer can change them.
func (i agentInputs) prompt(request GenerateRequest) string {
	var builder strings.Builder
	builder.WriteString("Generate a REST API test suite for the specification below.\n\n")
	builder.WriteString("Rules that are not negotiable:\n")
	builder.WriteString("- Every test case must link to at least one requirement id from the requirements below. A suite with an unlinked case is rejected as incomplete.\n")
	builder.WriteString("- Where an override rule applies, follow it instead of the literal specification, and record its exact reference string in that case's overrides_applied.\n")
	builder.WriteString("- Where an override rule forbids a case, do not generate the case at all.\n")
	builder.WriteString("- Leave overrides_applied empty for a case no rule touches. Do not cite a rule you did not follow.\n")
	builder.WriteString("- Use only the scenario values allowed by the schema; they are how the workbench audits which rule shaped which case.\n")

	fmt.Fprintf(&builder, "\n## OpenAPI/Swagger specification (%s)\n\n", i.specPath)
	builder.WriteString(i.spec)

	builder.WriteString("\n\n## Requirements (current versions)\n\n")
	builder.WriteString(encodeJSON(requirementViews(i.requirements)))

	builder.WriteString("\n\n## Override rules (active versions)\n\n")
	if len(i.rules) == 0 {
		builder.WriteString("[]\n\nNo override rules are active: follow the specification, and flag an ambiguity as a note on the affected case rather than guessing.")
	} else {
		builder.WriteString(encodeJSON(ruleViews(i.rules)))
	}

	if len(i.templates) > 0 {
		builder.WriteString("\n\n## Test templates\n\n")
		builder.WriteString(encodeJSON(i.templates))
	}
	if strings.TrimSpace(request.Instructions) != "" {
		builder.WriteString("\n\n## Additional instructions for this run\n\n")
		builder.WriteString(strings.TrimSpace(request.Instructions))
	}
	return builder.String()
}
