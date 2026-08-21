package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/provider"
	"github.com/ilyaus/loomwork/internal/testgen"
)

const generatedSuiteJSON = `{"title":"Orders API","cases":[` +
	`{"name":"lists the items of an order","requirement_ids":["req-001"],"scenario":"happy-path",` +
	`"request":{"method":"GET","path":"/orders/42/items"},"expected":{"status":200}},` +
	`{"name":"a missing order lists no items","requirement_ids":["req-002"],"scenario":"missing-item",` +
	`"request":{"method":"GET","path":"/orders/999/items"},"expected":{"status":200,"empty_collection":true}}]}`

// stubBridge installs a bridge script that answers the generation prompt with a
// fixed suite, so the CLI slice runs with no SDK and no credential.
func stubBridge(t *testing.T, suiteJSON string) {
	t.Helper()
	script := `
import { createInterface } from "node:readline";
const send = (event) => process.stdout.write(JSON.stringify(event) + "\n");
const rl = createInterface({ input: process.stdin });
rl.on("line", (line) => {
  if (!line.trim()) return;
  const request = JSON.parse(line);
  if (request.type === "start_session") { send({ type: "ready", session_id: "stub" }); return; }
  if (request.type === "prompt") {
    send({ type: "turn_complete", id: request.id, text: ` + "`" + suiteJSON + "`" + `, stop_reason: "end_turn" });
    return;
  }
  if (request.type === "close") { rl.close(); process.exit(0); }
});
`
	path := filepath.Join(t.TempDir(), "stub-bridge.mjs")
	if err := os.WriteFile(path, []byte(script), 0o644); err != nil {
		t.Fatalf("write stub bridge: %v", err)
	}
	t.Setenv(provider.BridgeEnvVar, path)
}

// seedGenerationInputs creates the project, agent definition, requirements, and
// override rule a generation run consumes, and returns the spec path.
func seedGenerationInputs(t *testing.T, home string) string {
	t.Helper()
	exec(t, home, "project", "create", "--name", "orders")
	exec(t, home, "agent-definition", "create", "--project", "orders",
		"--name", "rest-api-test-generator", "--target", string(model.AgentTargetClaudeSDK),
		"--model", "claude-sonnet-4-5", "--tools", strings.Join(testgen.GenerationTools(), ","),
		"--body", "# Role\n\nYou generate REST API test cases.")
	exec(t, home, "requirement", "create", "--project", "orders", "--text", "Listing the items of an order returns them")
	exec(t, home, "requirement", "create", "--project", "orders", "--text", "A missing order lists no items")
	exec(t, home, "agent-definition", "rule-create", "--project", "orders",
		"--rule", "empty-list-on-missing", "--title", "Missing collections return an empty list",
		"--methods", "GET", "--scenario", "missing-item", "--spec-status", "404",
		"--action", "expect-empty-collection",
		"--rationale", "The storefront treats an absent order as an empty basket")

	specPath := filepath.Join(t.TempDir(), "openapi.json")
	if err := os.WriteFile(specPath, []byte(`{"openapi":"3.1.0","paths":{}}`), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return specPath
}

func TestTestSuiteGenerateVerticalSlice(t *testing.T) {
	home := t.TempDir()
	specPath := seedGenerationInputs(t, home)
	stubBridge(t, generatedSuiteJSON)

	var result testgen.Result
	decodeJSON(t, exec(t, home, "test-suite", "generate", "--project", "orders",
		"--suite", "suite-orders-api", "--agent", "rest-api-test-generator",
		"--spec", specPath, "--tags", "phase3", "--json"), &result)

	suite := result.Suite
	if suite.Version != 1 || suite.Origin != model.TestSuiteOriginGenerated {
		t.Fatalf("suite = v%d %s", suite.Version, suite.Origin)
	}
	if suite.Incomplete {
		t.Errorf("suite is incomplete: %v", suite.IncompleteReasons)
	}
	if suite.AgentDefinition != "rest-api-test-generator-v1" {
		t.Errorf("AgentDefinition = %q", suite.AgentDefinition)
	}
	if got := suite.Cases[1].OverridesApplied; len(got) != 1 || got[0] != "empty-list-on-missing-v1" {
		t.Errorf("overrides_applied = %v, want the rule the case followed", got)
	}

	text := exec(t, home, "test-suite", "show", "--project", "orders", "--suite", "suite-orders-api")
	for _, want := range []string{"suite-orders-api-v1", "complete", "empty-list-on-missing-v1", "tc-001", "requirements: req-001"} {
		if !strings.Contains(text, want) {
			t.Errorf("show output is missing %q:\n%s", want, text)
		}
	}
	if listed := exec(t, home, "test-suite", "list", "--project", "orders"); !strings.Contains(listed, "2 case(s)") {
		t.Errorf("list output = %q", listed)
	}

	// A second run publishes v2 and keeps v1 retrievable.
	exec(t, home, "test-suite", "generate", "--project", "orders", "--suite", "suite-orders-api",
		"--agent", "rest-api-test-generator", "--spec", specPath)
	var history []model.TestSuite
	decodeJSON(t, exec(t, home, "test-suite", "show", "--project", "orders",
		"--suite", "suite-orders-api", "--history", "--json"), &history)
	if len(history) != 2 || history[0].Version != 1 {
		t.Fatalf("history = %+v, want both versions oldest first", history)
	}
}

func TestTestSuiteGenerateReportsAnIncompleteSuite(t *testing.T) {
	home := t.TempDir()
	specPath := seedGenerationInputs(t, home)
	stubBridge(t, `{"cases":[{"name":"rejects a malformed order id","requirement_ids":[],`+
		`"scenario":"invalid-input","request":{"method":"GET","path":"/orders/x/items"},"expected":{"status":400}}]}`)

	out := exec(t, home, "test-suite", "generate", "--project", "orders", "--suite", "suite-orders-api",
		"--agent", "rest-api-test-generator", "--spec", specPath)
	if !strings.Contains(out, "INCOMPLETE") || !strings.Contains(out, "tc-001") {
		t.Errorf("output = %q, want the unlinked case reported", out)
	}
	if listed := exec(t, home, "test-suite", "list", "--project", "orders"); !strings.Contains(listed, "INCOMPLETE") {
		t.Errorf("list output = %q, want the suite flagged", listed)
	}
}

func TestTestSuiteImportVerticalSlice(t *testing.T) {
	home := t.TempDir()
	seedGenerationInputs(t, home)

	path := filepath.Join(t.TempDir(), "legacy-suite.json")
	if err := os.WriteFile(path, []byte(generatedSuiteJSON), 0o644); err != nil {
		t.Fatalf("write suite: %v", err)
	}
	var result testgen.Result
	decodeJSON(t, exec(t, home, "test-suite", "import", "--project", "orders",
		"--file", path, "--suite", "suite-legacy", "--title", "Legacy suite", "--json"), &result)
	if result.Suite.Origin != model.TestSuiteOriginImported || result.Suite.Version != 1 {
		t.Fatalf("suite = v%d %s", result.Suite.Version, result.Suite.Origin)
	}
	if got := result.Suite.Cases[1].OverridesApplied; len(got) != 1 {
		t.Errorf("an imported case following a rule must be annotated: %v", got)
	}
	if out := exec(t, home, "test-suite", "show", "--project", "orders", "--suite", "suite-legacy"); !strings.Contains(out, "imported") {
		t.Errorf("show output = %q", out)
	}
}

func TestTestSuiteCommandsReportBadInput(t *testing.T) {
	home := t.TempDir()
	seedGenerationInputs(t, home)

	if got := execErr(t, home, "test-suite", "generate", "--project", "orders"); !strings.Contains(got, "are required") {
		t.Errorf("error = %q", got)
	}
	if got := execErr(t, home, "test-suite", "import", "--project", "orders"); !strings.Contains(got, "are required") {
		t.Errorf("error = %q", got)
	}
	if got := execErr(t, home, "test-suite", "import", "--project", "orders", "--file", "/nope.json"); !strings.Contains(got, "read --file") {
		t.Errorf("error = %q", got)
	}
	if got := execErr(t, home, "test-suite", "show", "--project", "orders", "--suite", "suite-absent"); !strings.Contains(got, "not found") {
		t.Errorf("error = %q", got)
	}
	if out := exec(t, home, "test-suite", "list", "--project", "orders"); !strings.Contains(out, "no test suites") {
		t.Errorf("output = %q", out)
	}
}
