package model

import "testing"

func TestDocumentSuitesGroupOnlyConfiguredFolderBoundaries(t *testing.T) {
	project := &Project{Import: &ProjectImport{Format: "spec-kit", Features: []string{"008-example"}}, Artifacts: []Artifact{
		{ID: "old", Name: "specs/008-example/sdd-qa/run/test.md", Version: 1},
		{ID: "current", Name: "specs/008-example/sdd-qa/run/test.md", Version: 2},
		{ID: "design", Name: "specs/008-example/sdd-qa/run/design.md", Version: 1},
		{ID: "outside", Name: "specs/008-example/sdd-qa-other/test.md", Version: 1},
	}}
	suites := project.DocumentSuites()
	if len(suites) != 1 || len(suites[0].Documents) != 2 {
		t.Fatalf("suites = %+v", suites)
	}
	for _, doc := range suites[0].Documents {
		if doc.ID == "old" || doc.ID == "outside" {
			t.Fatalf("incorrect document: %+v", doc)
		}
	}
	project.TestDocuments = &TestDocumentConfig{Roots: []string{}}
	if len(project.DocumentSuites()) != 0 {
		t.Fatal("empty configuration must disable grouping")
	}
	for _, roots := range [][]string{{"../outside"}, {"/absolute"}, {"specs/*/sdd-qa"}, {"specs", "specs/008-example"}} {
		if _, err := NormalizeTestDocumentRoots(roots); err == nil {
			t.Fatalf("accepted invalid or overlapping roots %v", roots)
		}
	}
}
