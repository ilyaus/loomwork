package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/testgen"
)

func testSuiteGenerate(e *env, args []string) error {
	var projectRef, suiteID, agentName, specPath, templates, modelID, title, description, tags, instructions string
	err := e.parse("test-suite generate", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&suiteID, "suite", "", "suite id, e.g. suite-orders-api (required)")
		flags.StringVar(&agentName, "agent", "", "agent definition name (required)")
		flags.StringVar(&specPath, "spec", "", "OpenAPI/Swagger file (required)")
		flags.StringVar(&templates, "templates", "", "comma-separated test template files")
		flags.StringVar(&modelID, "model", "", "override the model named by the agent definition")
		flags.StringVar(&title, "title", "", "title for the suite version")
		flags.StringVar(&description, "description", "", "description for the suite version")
		flags.StringVar(&tags, "tags", "", "comma-separated tags")
		flags.StringVar(&instructions, "instructions", "", "extra instructions appended to the generation prompt")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(suiteID) == "" || strings.TrimSpace(agentName) == "" {
		return fmt.Errorf("test-suite generate: --project, --suite, and --agent are required")
	}

	generator := testgen.New(e.store, nil)
	result, err := generator.Generate(context.Background(), testgen.GenerateRequest{
		ProjectRef:    projectRef,
		SuiteID:       suiteID,
		AgentName:     agentName,
		SpecPath:      specPath,
		TemplatePaths: splitList(templates),
		Model:         modelID,
		Title:         title,
		Description:   description,
		Tags:          splitList(tags),
		Instructions:  instructions,
	})
	if err != nil {
		return err
	}
	return emitTestSuiteResult(e, result)
}

func testSuiteImport(e *env, args []string) error {
	var projectRef, suiteID, file, title, description, tags string
	err := e.parse("test-suite import", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&file, "file", "", "JSON file matching test-case.schema.json (required)")
		flags.StringVar(&suiteID, "suite", "", "override the suite id in the file")
		flags.StringVar(&title, "title", "", "title for the suite version")
		flags.StringVar(&description, "description", "", "description for the suite version")
		flags.StringVar(&tags, "tags", "", "comma-separated tags")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(file) == "" {
		return fmt.Errorf("test-suite import: --project and --file are required")
	}
	payload, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read --file %s: %w", file, err)
	}

	generator := testgen.New(e.store, nil)
	result, err := generator.Import(testgen.ImportRequest{
		ProjectRef:  projectRef,
		SuiteID:     suiteID,
		Payload:     payload,
		SourcePath:  file,
		Title:       title,
		Description: description,
		Tags:        splitList(tags),
	})
	if err != nil {
		return err
	}
	return emitTestSuiteResult(e, result)
}

func testSuiteList(e *env, args []string) error {
	var projectRef string
	err := e.parse("test-suite list", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" {
		return fmt.Errorf("test-suite list: --project is required")
	}
	suites, err := e.store.ListTestSuites(projectRef)
	if err != nil {
		return err
	}
	if len(suites) == 0 {
		return e.emit(suites, "no test suites")
	}
	var builder strings.Builder
	for _, suite := range suites {
		fmt.Fprintf(&builder, "%s\t%s\t%d case(s)\t%s\n", suite.Ref(), suite.Origin, len(suite.CaseIDs), completeness(suite))
	}
	return e.emit(suites, strings.TrimRight(builder.String(), "\n"))
}

func testSuiteShow(e *env, args []string) error {
	var projectRef, suiteID string
	var version int
	var history bool
	err := e.parse("test-suite show", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&suiteID, "suite", "", "suite id (required)")
		flags.IntVar(&version, "version", 0, "version to show (default current)")
		flags.BoolVar(&history, "history", false, "list every retained version instead")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(suiteID) == "" {
		return fmt.Errorf("test-suite show: --project and --suite are required")
	}
	if version < 0 {
		return fmt.Errorf("test-suite show: --version must be 1 or greater (0 or omitted shows the current version)")
	}
	if history {
		versions, err := e.store.TestSuiteHistory(projectRef, suiteID)
		if err != nil {
			return err
		}
		var builder strings.Builder
		for _, suite := range versions {
			fmt.Fprintf(&builder, "%s\t%s\t%d case(s)\t%s\n", suite.Ref(), suite.Origin, len(suite.CaseIDs), completeness(suite))
		}
		return e.emit(versions, strings.TrimRight(builder.String(), "\n"))
	}
	suite, err := e.store.LoadTestSuite(projectRef, suiteID, version)
	if err != nil {
		return err
	}
	return e.emit(suite, renderSuite(suite))
}

func emitTestSuiteResult(e *env, result testgen.Result) error {
	if e.asJSON {
		return e.emit(result, "")
	}
	var builder strings.Builder
	builder.WriteString(renderSuite(result.Suite))
	if result.Agent != "" {
		fmt.Fprintf(&builder, "\nagent: %s\tadapter: %s", result.Agent, result.Adapter)
		if result.Model != "" {
			fmt.Fprintf(&builder, "\tmodel: %s", result.Model)
		}
		fmt.Fprintf(&builder, "\tduration: %dms", result.DurationMs)
	}
	if annotated := result.Audit.Annotated(); len(annotated) > 0 {
		builder.WriteString("\n\noverride citations added:\n")
		for _, finding := range annotated {
			fmt.Fprintf(&builder, "  %s\t%s\n", finding.CaseID, finding.Detail)
		}
	}
	return e.emit(result, strings.TrimRight(builder.String(), "\n"))
}

func renderSuite(suite *model.TestSuite) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s\t%s\t%s\n", suite.Ref(), suite.Origin, completeness(suite))
	if suite.Title != "" {
		fmt.Fprintf(&builder, "%s\n", suite.Title)
	}
	if suite.AgentDefinition != "" {
		fmt.Fprintf(&builder, "agent definition: %s\n", suite.AgentDefinition)
	}
	if len(suite.OverrideRules) > 0 {
		fmt.Fprintf(&builder, "override rules: %s\n", strings.Join(suite.OverrideRules, ", "))
	}
	for _, testCase := range suite.Cases {
		fmt.Fprintf(&builder, "  %s\t%s %s\t-> %d\t%s\n", testCase.ID, testCase.Request.Method, testCase.Request.Path, testCase.Expected.Status, testCase.Name)
		fmt.Fprintf(&builder, "      requirements: %s", strings.Join(orNone(testCase.RequirementIDs), ", "))
		if len(testCase.OverridesApplied) > 0 {
			fmt.Fprintf(&builder, "\toverrides: %s", strings.Join(testCase.OverridesApplied, ", "))
		}
		builder.WriteString("\n")
	}
	if suite.Incomplete {
		builder.WriteString("\nincomplete because:\n")
		for _, reason := range suite.IncompleteReasons {
			fmt.Fprintf(&builder, "  - %s\n", reason)
		}
	}
	return strings.TrimRight(builder.String(), "\n")
}

func completeness(suite *model.TestSuite) string {
	if suite.Incomplete {
		return "INCOMPLETE"
	}
	return "complete"
}

func orNone(values []string) []string {
	if len(values) == 0 {
		return []string{"none"}
	}
	return values
}
