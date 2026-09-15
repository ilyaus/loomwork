package httpapi

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ilyaus/loomwork/internal/model"
)

// Testability is the per-project health rollup the landing view shows. It is
// derived on read from the current test suites and the reports folder, never
// stored. `available` is false when the project has no suites and no reports,
// so a client can tell "nothing tested yet" from "0% covered".
type Testability struct {
	Available       bool       `json:"available"`
	LastTestedAt    *time.Time `json:"lastTestedAt"`
	CoveragePercent *float64   `json:"coveragePercent"`
	OpenGaps        *int       `json:"openGaps"`
}

// RunSummary is the outcome of the most recent report, when one can be read.
type RunSummary struct {
	Report  string `json:"report"`
	Outcome string `json:"outcome,omitempty"`
	Total   int    `json:"total"`
	Passed  int    `json:"passed"`
	Failed  int    `json:"failed"`
	Skipped int    `json:"skipped"`
}

// TestabilityReport is the detailed view behind the rollup: which active
// requirements have at least one linked test case and which have none.
type TestabilityReport struct {
	Testability
	ActiveRequirements    int         `json:"activeRequirements"`
	CoveredRequirements   []string    `json:"coveredRequirements"`
	UncoveredRequirements []string    `json:"uncoveredRequirements"`
	Suites                int         `json:"suites"`
	IncompleteSuites      int         `json:"incompleteSuites"`
	Cases                 int         `json:"cases"`
	UnlinkedCases         int         `json:"unlinkedCases"`
	Reports               int         `json:"reports"`
	LastRun               *RunSummary `json:"lastRun"`
}

func (s *Server) getTestability(w http.ResponseWriter, _ *http.Request, projectRef string) {
	report, err := s.testability(projectRef)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// testability computes the rollup from the store. Coverage counts an active
// requirement as covered when any case in any suite's current version links it.
func (s *Server) testability(projectRef string) (TestabilityReport, error) {
	requirements, err := s.store.ListRequirements(projectRef)
	if err != nil {
		return TestabilityReport{}, err
	}
	suites, err := s.store.ListTestSuites(projectRef)
	if err != nil {
		return TestabilityReport{}, err
	}
	reports, err := s.store.ListReports(projectRef)
	if err != nil {
		return TestabilityReport{}, err
	}

	report := TestabilityReport{
		CoveredRequirements:   []string{},
		UncoveredRequirements: []string{},
		Suites:                len(suites),
		Reports:               len(reports),
	}
	linked := map[string]bool{}
	for _, summary := range suites {
		suite, err := s.store.LoadTestSuite(projectRef, summary.SuiteID, summary.Version)
		if err != nil {
			return TestabilityReport{}, err
		}
		if suite.Incomplete {
			report.IncompleteSuites++
		}
		for _, testCase := range suite.Cases {
			report.Cases++
			if len(testCase.RequirementIDs) == 0 {
				report.UnlinkedCases++
			}
			for _, id := range testCase.RequirementIDs {
				linked[strings.ToLower(id)] = true
			}
		}
	}
	for _, requirement := range requirements {
		if requirement.Status != model.RequirementStatusActive {
			continue
		}
		report.ActiveRequirements++
		if linked[strings.ToLower(requirement.ID)] || linked[strings.ToLower(requirement.ReferenceID())] {
			report.CoveredRequirements = append(report.CoveredRequirements, requirement.ID)
		} else {
			report.UncoveredRequirements = append(report.UncoveredRequirements, requirement.ID)
		}
	}
	sort.Strings(report.CoveredRequirements)
	sort.Strings(report.UncoveredRequirements)

	report.Available = len(suites) > 0 || len(reports) > 0
	if report.Available && report.ActiveRequirements > 0 {
		coverage := float64(len(report.CoveredRequirements)) / float64(report.ActiveRequirements) * 100
		gaps := len(report.UncoveredRequirements)
		report.CoveragePercent = &coverage
		report.OpenGaps = &gaps
	}
	// The newest run is the report with the latest embedded run timestamp,
	// falling back to file modification time for reports that carry none.
	var latestAt time.Time
	for _, file := range reports {
		testedAt := file.UpdatedAt
		var run *RunSummary
		if strings.HasPrefix(file.MediaType, "application/json") {
			if _, raw, err := s.store.LoadReport(projectRef, file.Name); err == nil {
				var runAt *time.Time
				run, runAt = summarizeRunReport(file.Name, raw)
				if runAt != nil {
					testedAt = *runAt
				}
			}
		}
		if report.LastTestedAt == nil || testedAt.After(latestAt) {
			latestAt = testedAt
			report.LastTestedAt = &latestAt
			report.LastRun = run
		}
	}
	return report, nil
}

// summarizeRunReport reads the aggregate counts and run timestamp out of a JSON
// report without committing to one runner's schema: api-test-runner nests
// counts under summary, the vision's report layout puts them at the top level,
// and either may list per-test results instead. A report that is not JSON yields
// no summary and no timestamp.
func summarizeRunReport(name string, raw []byte) (*RunSummary, *time.Time) {
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, nil
	}
	run := &RunSummary{Report: name}
	if outcome, ok := document["outcome"].(string); ok {
		run.Outcome = outcome
	}
	counts := document
	if summary, ok := document["summary"].(map[string]any); ok {
		counts = summary
	}
	run.Total = intField(counts, "total")
	run.Passed = intField(counts, "passed")
	run.Failed = intField(counts, "failed")
	run.Skipped = intField(counts, "skipped")
	if run.Total == 0 && run.Passed == 0 && run.Failed == 0 {
		for _, key := range []string{"results", "tests"} {
			rows, ok := document[key].([]any)
			if !ok {
				continue
			}
			for _, row := range rows {
				fields, ok := row.(map[string]any)
				if !ok {
					continue
				}
				run.Total++
				switch status, _ := fields["status"].(string); {
				case status == "failed" || fields["passed"] == false || fields["error"] != nil:
					run.Failed++
				case status == "skipped":
					run.Skipped++
				default:
					run.Passed++
				}
			}
		}
	}
	if run.Total == 0 {
		run.Total = run.Passed + run.Failed + run.Skipped
	}
	if run.Outcome == "" && run.Total > 0 {
		run.Outcome = "passed"
		if run.Failed > 0 {
			run.Outcome = "failed"
		}
	}
	var runAt *time.Time
	for _, key := range []string{"run_timestamp", "runTimestamp", "timestamp", "startedAt", "started_at", "finishedAt", "completedAt"} {
		value, ok := document[key].(string)
		if !ok {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			utc := parsed.UTC()
			runAt = &utc
			break
		}
	}
	return run, runAt
}

func intField(fields map[string]any, key string) int {
	if value, ok := fields[key].(float64); ok {
		return int(value)
	}
	return 0
}
