package ingest

import (
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
)

const sampleReport = `{
  "outcome": "failure",
  "baseUrl": "http://localhost:9999",
  "summary": {"total": 3, "passed": 1, "failed": 1, "skipped": 1},
  "results": [
    {"file": "a.md", "scenario": 1, "title": "creates a thing", "status": "passed"},
    {"file": "a.md", "scenario": 2, "title": "rejects bad input", "status": "failed"},
    {"file": "b.md", "scenario": 1, "status": "skipped", "skipReason": "dependency failed"}
  ]
}`

func TestParseRunReport(t *testing.T) {
	report, err := ParseRunReport([]byte(sampleReport))
	if err != nil {
		t.Fatalf("ParseRunReport: %v", err)
	}
	if report.Outcome != "failure" {
		t.Fatalf("Outcome = %q", report.Outcome)
	}
	if report.Summary.Total != 3 || report.Summary.Passed != 1 || report.Summary.Failed != 1 || report.Summary.Skipped != 1 {
		t.Fatalf("Summary = %+v", report.Summary)
	}
	if len(report.Results) != 3 {
		t.Fatalf("Results = %d, want 3", len(report.Results))
	}
}

func TestParseRunReportRejectsGarbage(t *testing.T) {
	for name, raw := range map[string]string{
		"empty":      "",
		"whitespace": "  \n",
		"not JSON":   "runner exploded",
		"no outcome": `{"summary": {"total": 0}}`,
	} {
		if _, err := ParseRunReport([]byte(raw)); err == nil {
			t.Fatalf("%s: ParseRunReport should fail", name)
		}
	}
}

func TestArtifactSpec(t *testing.T) {
	report, err := ParseRunReport([]byte(sampleReport))
	if err != nil {
		t.Fatalf("ParseRunReport: %v", err)
	}
	spec := ArtifactSpec("orders.test-result", []byte(sampleReport), report, []string{"workbench"})
	if spec.Type != model.ArtifactTypeTestResult {
		t.Fatalf("Type = %q", spec.Type)
	}
	if spec.Body.Content != sampleReport {
		t.Fatal("Body should preserve the raw report verbatim")
	}
	for key, want := range map[string]string{
		"tool": "api-test-runner", "outcome": "failure",
		"total": "3", "passed": "1", "failed": "1", "skipped": "1",
	} {
		if spec.Metadata[key] != want {
			t.Fatalf("Metadata[%q] = %q, want %q", key, spec.Metadata[key], want)
		}
	}
	if _, ok := spec.Metadata["dryRun"]; ok {
		t.Fatal("dryRun should be absent for non-dry runs")
	}
}

func TestArtifactSpecGlobalErrorsAndDryRun(t *testing.T) {
	raw := `{"outcome":"failure","dryRun":true,"summary":{},"globalErrors":[{"kind":"parse","file":"x.md","detail":"bad table"}]}`
	report, err := ParseRunReport([]byte(raw))
	if err != nil {
		t.Fatalf("ParseRunReport: %v", err)
	}
	spec := ArtifactSpec("r", []byte(raw), report, nil)
	if spec.Metadata["dryRun"] != "true" || spec.Metadata["globalErrors"] != "1" {
		t.Fatalf("Metadata = %v", spec.Metadata)
	}
}

func TestSummarize(t *testing.T) {
	report, err := ParseRunReport([]byte(sampleReport))
	if err != nil {
		t.Fatalf("ParseRunReport: %v", err)
	}
	summary := Summarize(report)
	for _, want := range []string{"outcome: failure", "3 total", "failed: rejects bad input"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("Summarize %q missing %q", summary, want)
		}
	}
	if strings.Contains(summary, "creates a thing") {
		t.Fatal("Summarize should not list passing scenarios")
	}
}
