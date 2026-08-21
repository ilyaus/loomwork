package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
)

func suiteWithCases(names ...string) *model.TestSuite {
	cases := make([]model.TestCase, 0, len(names))
	for _, name := range names {
		cases = append(cases, model.TestCase{
			Name:           name,
			RequirementIDs: []string{"req-001"},
			Scenario:       model.ScenarioHappyPath,
			Request:        model.TestRequest{Method: model.MethodGET, Path: "/orders/42/items"},
			Expected:       model.TestExpectation{Status: 200},
		})
	}
	return &model.TestSuite{SuiteID: "suite-orders-api", Origin: model.TestSuiteOriginGenerated, Cases: cases}
}

func TestSaveTestSuiteWritesVersionedDirectoriesAndACurrentPointer(t *testing.T) {
	dirStore, project := seededStore(t)
	var _ TestSuiteStore = dirStore

	first, err := dirStore.SaveTestSuite(project.ID, suiteWithCases("lists items"))
	if err != nil {
		t.Fatalf("SaveTestSuite: %v", err)
	}
	if first.Version != 1 {
		t.Fatalf("Version = %d, want 1", first.Version)
	}
	second, err := dirStore.SaveTestSuite(project.ID, suiteWithCases("lists items", "rejects an unknown order"))
	if err != nil {
		t.Fatalf("SaveTestSuite: %v", err)
	}
	if second.Version != 2 {
		t.Fatalf("Version = %d, want 2", second.Version)
	}

	root := filepath.Join(dirStore.ProjectDir(project.ID), TestSuitesDirName, "suite-orders-api")
	for _, name := range []string{
		"current.json",
		filepath.Join("v1", "suite.json"),
		filepath.Join("v1", "tests", "tc-001.json"),
		filepath.Join("v2", "suite.json"),
		filepath.Join("v2", "tests", "tc-002.json"),
	} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("%s missing: %v", name, err)
		}
	}

	current, err := dirStore.LoadTestSuite(project.ID, "suite-orders-api", 0)
	if err != nil {
		t.Fatalf("LoadTestSuite(current): %v", err)
	}
	if current.Version != 2 || len(current.Cases) != 2 {
		t.Errorf("current = v%d with %d case(s)", current.Version, len(current.Cases))
	}
	earlier, err := dirStore.LoadTestSuite(project.ID, "suite-orders-api", 1)
	if err != nil {
		t.Fatalf("LoadTestSuite(v1): %v", err)
	}
	if len(earlier.Cases) != 1 {
		t.Errorf("v1 has %d case(s), want 1: earlier versions are immutable", len(earlier.Cases))
	}

	pointer, err := dirStore.TestSuitePointer(project.ID, "suite-orders-api")
	if err != nil {
		t.Fatalf("TestSuitePointer: %v", err)
	}
	if pointer.CurrentVersion != 2 || len(pointer.Versions) != 2 {
		t.Errorf("pointer = %+v", pointer)
	}
}

func TestSaveTestSuiteRetainsIncompletenessWithTheVersion(t *testing.T) {
	dirStore, project := seededStore(t)
	suite := suiteWithCases("lists items")
	suite.Cases[0].RequirementIDs = nil
	audit := suite.ApplyOverrideRules(nil)
	if len(audit.UnlinkedCases) != 1 {
		t.Fatalf("UnlinkedCases = %v", audit.UnlinkedCases)
	}

	if _, err := dirStore.SaveTestSuite(project.ID, suite); err != nil {
		t.Fatalf("SaveTestSuite: %v", err)
	}
	loaded, err := dirStore.LoadTestSuite(project.ID, "suite-orders-api", 0)
	if err != nil {
		t.Fatalf("LoadTestSuite: %v", err)
	}
	if !loaded.Incomplete || len(loaded.IncompleteReasons) != 1 {
		t.Errorf("incompleteness lost: %t %v", loaded.Incomplete, loaded.IncompleteReasons)
	}
	pointer, err := dirStore.TestSuitePointer(project.ID, "suite-orders-api")
	if err != nil {
		t.Fatalf("TestSuitePointer: %v", err)
	}
	if !pointer.Incomplete {
		t.Error("the pointer should surface incompleteness without opening the version")
	}
}

func TestSaveTestSuiteFlagsAnEmptySuiteInsteadOfRejectingIt(t *testing.T) {
	dirStore, project := seededStore(t)
	empty := suiteWithCases()
	empty.ApplyOverrideRules(nil)

	stored, err := dirStore.SaveTestSuite(project.ID, empty)
	if err != nil {
		t.Fatalf("SaveTestSuite: %v", err)
	}
	if !stored.Incomplete {
		t.Fatalf("a suite with no cases must be flagged: %+v", stored.IncompleteReasons)
	}
	pointer, err := dirStore.TestSuitePointer(project.ID, "suite-orders-api")
	if err != nil {
		t.Fatalf("TestSuitePointer: %v", err)
	}
	if pointer.CurrentVersion != 1 || !pointer.Incomplete {
		t.Errorf("pointer = %+v", pointer)
	}
}

func TestListAndHistoryOfTestSuites(t *testing.T) {
	dirStore, project := seededStore(t)
	if _, err := dirStore.SaveTestSuite(project.ID, suiteWithCases("lists items")); err != nil {
		t.Fatalf("SaveTestSuite: %v", err)
	}
	if _, err := dirStore.SaveTestSuite(project.ID, suiteWithCases("lists items again")); err != nil {
		t.Fatalf("SaveTestSuite: %v", err)
	}
	imported := suiteWithCases("checks the legacy contract")
	imported.SuiteID = "suite-legacy"
	imported.Origin = model.TestSuiteOriginImported
	if _, err := dirStore.SaveTestSuite(project.ID, imported); err != nil {
		t.Fatalf("SaveTestSuite(imported): %v", err)
	}

	suites, err := dirStore.ListTestSuites(project.ID)
	if err != nil {
		t.Fatalf("ListTestSuites: %v", err)
	}
	if len(suites) != 2 || suites[0].SuiteID != "suite-legacy" || suites[1].Version != 2 {
		t.Errorf("suites = %+v", suites)
	}
	if suites[0].Origin != model.TestSuiteOriginImported {
		t.Errorf("imported origin lost: %q", suites[0].Origin)
	}
	if len(suites[1].Cases) != 0 || len(suites[1].CaseIDs) != 1 {
		t.Errorf("list should return manifests only: %+v", suites[1])
	}

	history, err := dirStore.TestSuiteHistory(project.ID, "suite-orders-api")
	if err != nil {
		t.Fatalf("TestSuiteHistory: %v", err)
	}
	if len(history) != 2 || history[0].Version != 1 {
		t.Errorf("history = %+v", history)
	}

	if _, err := dirStore.TestSuiteHistory(project.ID, "suite-absent"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	if _, err := dirStore.LoadTestSuite(project.ID, "suite-absent", 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
