// Package ingest maps sibling tools' report files into Loomwork artifacts.
// The first mapper understands the api-test-runner structured JSON run report
// (its stdout, mirrored by reports/<run-id>/<run-id>_run.json). Ingestion keeps
// the full report as the artifact body — the source of truth for later prompt
// runs — and lifts only the aggregate counts into metadata, where list views
// and orchestration decisions can read them without re-parsing.
package ingest

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/ilyaus/loomwork/internal/model"
)

// RunReport is the subset of api-test-runner's report Loomwork reads. The full
// document is preserved verbatim in the artifact body; these fields only feed
// summary metadata, so unknown fields are deliberately ignored.
type RunReport struct {
	Outcome string `json:"outcome"`
	DryRun  bool   `json:"dryRun"`
	BaseURL string `json:"baseUrl"`
	Summary struct {
		Total   int `json:"total"`
		Passed  int `json:"passed"`
		Failed  int `json:"failed"`
		Skipped int `json:"skipped"`
	} `json:"summary"`
	Results []struct {
		File   string `json:"file"`
		Title  string `json:"title"`
		Status string `json:"status"`
	} `json:"results"`
	GlobalErrors []struct {
		Kind   string `json:"kind"`
		File   string `json:"file"`
		Detail string `json:"detail"`
	} `json:"globalErrors"`
}

// ParseRunReport decodes an api-test-runner JSON report.
func ParseRunReport(raw []byte) (RunReport, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return RunReport{}, fmt.Errorf("ingest: report is empty")
	}
	var report RunReport
	if err := json.Unmarshal([]byte(trimmed), &report); err != nil {
		return RunReport{}, fmt.Errorf("ingest: parse report: %w", err)
	}
	if report.Outcome == "" {
		return RunReport{}, fmt.Errorf("ingest: report has no outcome; not an api-test-runner report?")
	}
	return report, nil
}

// ArtifactSpec builds the test-result artifact for a report. The body is the
// raw report exactly as the runner emitted it; name and tags come from the
// caller.
func ArtifactSpec(name string, raw []byte, report RunReport, tags []string) model.ArtifactSpec {
	metadata := map[string]string{
		"tool":    "api-test-runner",
		"outcome": report.Outcome,
		"total":   strconv.Itoa(report.Summary.Total),
		"passed":  strconv.Itoa(report.Summary.Passed),
		"failed":  strconv.Itoa(report.Summary.Failed),
		"skipped": strconv.Itoa(report.Summary.Skipped),
	}
	if report.DryRun {
		metadata["dryRun"] = "true"
	}
	if len(report.GlobalErrors) > 0 {
		metadata["globalErrors"] = strconv.Itoa(len(report.GlobalErrors))
	}
	return model.ArtifactSpec{
		Name:     name,
		Type:     model.ArtifactTypeTestResult,
		Body:     model.Body{Content: string(raw)},
		Tags:     tags,
		Metadata: metadata,
	}
}

// Summarize renders a short human-readable line for CLI output.
func Summarize(report RunReport) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "outcome: %s\t%d total / %d passed / %d failed / %d skipped",
		report.Outcome, report.Summary.Total, report.Summary.Passed,
		report.Summary.Failed, report.Summary.Skipped)
	if report.DryRun {
		builder.WriteString("\t(dry run)")
	}
	for _, failure := range report.GlobalErrors {
		fmt.Fprintf(&builder, "\nglobal error (%s): %s", failure.Kind, failure.Detail)
	}
	for _, result := range report.Results {
		if result.Status == "failed" {
			fmt.Fprintf(&builder, "\nfailed: %s", firstNonEmpty(result.Title, result.File))
		}
	}
	return builder.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
