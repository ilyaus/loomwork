package traceability

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/ilyaus/loomwork/internal/model"
)

type TestLink struct {
	Family     string `json:"family"`
	Ref        string `json:"ref"`
	Name       string `json:"name"`
	Version    int    `json:"version"`
	SuiteID    string `json:"suiteId,omitempty"`
	SuiteTitle string `json:"suiteTitle,omitempty"`
}

type Store interface {
	Resolve(string) (*model.Project, error)
	ListRequirements(string) ([]*model.Requirement, error)
	ListTestSuites(string) ([]*model.TestSuite, error)
	LoadTestSuite(string, string, int) (*model.TestSuite, error)
}

func RequirementTests(store Store, projectRef string) (map[string][]TestLink, error) {
	project, err := store.Resolve(projectRef)
	if err != nil {
		return nil, err
	}
	requirements, err := store.ListRequirements(project.ID)
	if err != nil {
		return nil, err
	}
	summaries, err := store.ListTestSuites(project.ID)
	if err != nil {
		return nil, err
	}
	suites := make([]*model.TestSuite, 0, len(summaries))
	for _, summary := range summaries {
		suite, err := store.LoadTestSuite(project.ID, summary.SuiteID, summary.Version)
		if err != nil {
			return nil, err
		}
		suites = append(suites, suite)
	}
	return buildLinks(requirements, suites, project.LatestArtifacts()), nil
}

func buildLinks(requirements []*model.Requirement, suites []*model.TestSuite, artifacts []model.Artifact) map[string][]TestLink {
	links := make(map[string][]TestLink, len(requirements))
	type sourceKey struct{ format, feature, id string }
	sources := map[sourceKey]string{}
	aliases := map[string]string{}
	for _, requirement := range requirements {
		links[requirement.ID] = []TestLink{}
		aliases[strings.ToLower(requirement.ID)] = requirement.ID
		aliases[strings.ToLower(requirement.ReferenceID())] = requirement.ID
		metadata := requirement.Metadata
		if metadata["import_format"] != "" && metadata["feature"] != "" && metadata["source_id"] != "" {
			sources[sourceKey{metadata["import_format"], metadata["feature"], metadata["source_id"]}] = requirement.ID
		}
	}
	add := func(id string, link TestLink) {
		id = aliases[strings.ToLower(id)]
		current, ok := links[id]
		if !ok {
			return
		}
		for _, existing := range current {
			if existing.Family == link.Family && existing.Ref == link.Ref {
				return
			}
		}
		links[id] = append(current, link)
	}
	for _, suite := range suites {
		for _, testCase := range suite.Cases {
			link := TestLink{Family: "test-cases", Ref: suite.SuiteID + "~" + testCase.ID, Name: testCase.Name, Version: suite.Version, SuiteID: suite.SuiteID, SuiteTitle: suite.Title}
			for _, id := range testCase.RequirementIDs {
				add(id, link)
			}
		}
	}
	for _, artifact := range artifacts {
		if !isScenario(artifact) {
			continue
		}
		link := TestLink{Family: "artifacts", Ref: artifact.ID, Name: artifact.Name, Version: artifact.Version}
		for _, id := range referenceIDs(artifact.Metadata["requirement_ids"]) {
			add(id, link)
		}
		for _, id := range scenarioReferences(artifact) {
			if _, err := model.NormalizeRequirementID(id); err == nil {
				add(id, link)
			} else if artifact.Metadata["import_format"] == "spec-kit" {
				key := sourceKey{"spec-kit", artifact.Metadata["feature"], id}
				add(sources[key], link)
			}
		}
	}
	for _, tests := range links {
		sort.Slice(tests, func(i, j int) bool {
			if tests[i].Family != tests[j].Family {
				return tests[i].Family == "test-cases"
			}
			if tests[i].Name != tests[j].Name {
				return tests[i].Name < tests[j].Name
			}
			return tests[i].Ref < tests[j].Ref
		})
	}
	return links
}

func isScenario(artifact model.Artifact) bool {
	if artifact.Metadata["document_kind"] == "test-scenario" {
		return true
	}
	for _, tag := range artifact.Tags {
		if tag == "test-scenario" {
			return true
		}
	}
	return false
}

var referencePattern = regexp.MustCompile(`\b(?:req-[0-9]{3,}|(?:[0-9]{3,}-)?(?:FR|NFR|SC)-[0-9]+[A-Za-z]?)\b`)
var scenarioFile = regexp.MustCompile(`^((?:FR|NFR|SC)-[0-9]+[A-Za-z]?)(?:\.(?:api|nonapi))?\.md$`)
var referenceField = regexp.MustCompile(`(?i)^(?:\*\*)?(?:purpose|requirements?|requirement_ids|covers)(?:\*\*)?\s*:(?:\*\*)?\s*(.*)$`)
var frontmatterFR = regexp.MustCompile(`(?i)^fr:\s*(.+)$`)

func referenceIDs(text string) []string {
	ids := []string{}
	for _, span := range referencePattern.FindAllStringIndex(text, -1) {
		if (span[0] > 0 && strings.ContainsRune("-_", rune(text[span[0]-1]))) || (span[1] < len(text) && strings.ContainsRune("-_", rune(text[span[1]]))) {
			continue
		}
		ids = append(ids, text[span[0]:span[1]])
	}
	return ids
}

func scenarioReferences(artifact model.Artifact) []string {
	ids := []string{}
	if artifact.Metadata["import_format"] == "spec-kit" {
		if match := scenarioFile.FindStringSubmatch(path.Base(artifact.Name)); match != nil {
			ids = append(ids, match[1])
		}
	}
	lines := strings.Split(strings.ReplaceAll(artifact.Body.Content, "\r\n", "\n"), "\n")
	frontmatter := false
	fence := ""
	for i, line := range lines {
		if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
			continue
		}
		line = strings.TrimSpace(line)
		if i == 0 && line == "---" {
			frontmatter = true
			continue
		}
		if frontmatter {
			if line == "---" {
				frontmatter = false
				continue
			}
			if match := frontmatterFR.FindStringSubmatch(line); match != nil {
				ids = append(ids, referenceIDs(match[1])...)
			}
			continue
		}
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			marker := line[:3]
			if fence == "" {
				fence = marker
			} else if fence == marker {
				fence = ""
			}
			continue
		}
		if fence == "" {
			if match := referenceField.FindStringSubmatch(line); match != nil {
				ids = append(ids, referenceIDs(match[1])...)
			}
		}
	}
	return ids
}
