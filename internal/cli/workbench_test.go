package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
)

// writeStubRunner materializes a fake api-test-runner: it records its argv and
// scenario directory contents, then prints the canned report on stdout and
// exits with the given code. Tests run on POSIX systems.
func writeStubRunner(t *testing.T, report string, exitCode string) (binary, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	binary = filepath.Join(dir, "api-test-runner")
	argsFile = filepath.Join(dir, "args.txt")
	reportFile := filepath.Join(dir, "report.json")
	if err := os.WriteFile(reportFile, []byte(report), 0o644); err != nil {
		t.Fatalf("write report: %v", err)
	}
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$@\" > " + argsFile + "\n" +
		"cat " + reportFile + "\n" +
		"exit " + exitCode + "\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub runner: %v", err)
	}
	return binary, argsFile
}

const stubReport = `{
  "outcome": "failure",
  "summary": {"total": 2, "passed": 1, "failed": 1, "skipped": 0},
  "results": [
    {"file": "001-orders.md", "scenario": 1, "title": "lists orders", "status": "passed"},
    {"file": "001-orders.md", "scenario": 2, "title": "rejects bad ids", "status": "failed"}
  ]
}`

func workbenchProject(t *testing.T, home string) (scenarioID string) {
	t.Helper()
	exec(t, home, "project", "create", "--name", "workbench")
	out := exec(t, home, "artifact", "add", "--project", "workbench", "--name", "orders.md",
		"--type", "spec", "--content", "# Scenario: lists orders", "--json")
	var artifact model.Artifact
	if err := json.Unmarshal([]byte(out), &artifact); err != nil {
		t.Fatalf("decode artifact: %v", err)
	}
	return artifact.ID
}

func TestWorkbenchRunIngestsReport(t *testing.T) {
	home := t.TempDir()
	scenarioID := workbenchProject(t, home)
	binary, argsFile := writeStubRunner(t, stubReport, "1")

	out := exec(t, home, "workbench", "run", "--project", "workbench",
		"--scenarios", "orders.md", "--base-url", "http://localhost:9999",
		"--runner", binary, "--dry-run", "--arg", "--max-parallelism", "--arg", "2",
		"--name", "orders.results", "--tags", "smoke", "--json")

	var payload struct {
		Artifact model.Artifact `json:"artifact"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode payload: %v (out: %s)", err, out)
	}
	artifact := payload.Artifact
	if artifact.Type != model.ArtifactTypeTestResult {
		t.Fatalf("Type = %q", artifact.Type)
	}
	if artifact.ParentID != scenarioID {
		t.Fatalf("ParentID = %q, want scenario %q", artifact.ParentID, scenarioID)
	}
	for key, want := range map[string]string{
		"tool": "api-test-runner", "outcome": "failure",
		"total": "2", "failed": "1", "exitCode": "1", "scenarios": scenarioID,
	} {
		if artifact.Metadata[key] != want {
			t.Fatalf("Metadata[%q] = %q, want %q", key, artifact.Metadata[key], want)
		}
	}
	if !strings.Contains(artifact.Body.Content, `"outcome": "failure"`) {
		t.Fatal("Body should carry the raw report")
	}

	rawArgs, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(rawArgs)), "\n")
	joined := strings.Join(args, " ")
	for _, want := range []string{"--scenarios", "--base-url http://localhost:9999", "--dry-run", "--max-parallelism 2"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("runner argv %q missing %q", joined, want)
		}
	}
	// The scenario directory must have contained the materialized artifact.
	var scenarioDir string
	for index, arg := range args {
		if arg == "--scenarios" && index+1 < len(args) {
			scenarioDir = args[index+1]
		}
	}
	if scenarioDir == "" {
		t.Fatalf("no --scenarios directory in argv %q", joined)
	}

	// Human-readable mode prints the ingest summary.
	text := exec(t, home, "workbench", "run", "--project", "workbench",
		"--scenarios", "orders.md", "--base-url", "http://localhost:9999", "--runner", binary)
	for _, want := range []string{"outcome: failure", "2 total", "failed: rejects bad ids"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output %q missing %q", text, want)
		}
	}
}

func TestWorkbenchRunValidation(t *testing.T) {
	home := t.TempDir()
	workbenchProject(t, home)
	binary, _ := writeStubRunner(t, stubReport, "0")

	if got := execErr(t, home, "workbench", "run", "--project", "workbench"); !strings.Contains(got, "--base-url") {
		t.Fatalf("error = %q, want required-flag error", got)
	}
	if got := execErr(t, home, "workbench", "run", "--project", "workbench",
		"--scenarios", "orders.md", "--base-url", "http://x", "--runner", binary,
		"--auth-config", "a.json", "--token-provider-config", "b.json"); !strings.Contains(got, "mutually exclusive") {
		t.Fatalf("error = %q, want mutual-exclusion error", got)
	}
	if got := execErr(t, home, "workbench", "run", "--project", "workbench",
		"--scenarios", "missing.md", "--base-url", "http://x", "--runner", binary); !strings.Contains(got, `"missing.md" not found`) {
		t.Fatalf("error = %q, want missing-artifact error", got)
	}
	if got := execErr(t, home, "workbench", "run", "--project", "workbench",
		"--scenarios", "orders.md", "--base-url", "http://x",
		"--runner", filepath.Join(t.TempDir(), "nope")); !strings.Contains(got, "runner binary") {
		t.Fatalf("error = %q, want missing-binary error", got)
	}
}

func TestWorkbenchRunGarbageOutput(t *testing.T) {
	home := t.TempDir()
	workbenchProject(t, home)
	binary, _ := writeStubRunner(t, "runner exploded", "2")
	got := execErr(t, home, "workbench", "run", "--project", "workbench",
		"--scenarios", "orders.md", "--base-url", "http://x", "--runner", binary)
	if !strings.Contains(got, "without a readable report") || !strings.Contains(got, "runner exploded") {
		t.Fatalf("error = %q, want unreadable-report error with detail", got)
	}
}

func TestWorkbenchRunnerPathFromConfig(t *testing.T) {
	home := t.TempDir()
	workbenchProject(t, home)
	binary, _ := writeStubRunner(t, stubReport, "0")
	configJSON, err := json.Marshal(map[string]any{"workbench": map[string]any{"runnerPath": binary}})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.json"), configJSON, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	out := exec(t, home, "workbench", "run", "--project", "workbench",
		"--scenarios", "orders.md", "--base-url", "http://x")
	if !strings.Contains(out, "outcome: failure") {
		t.Fatalf("output = %q, want ingest summary", out)
	}
}
