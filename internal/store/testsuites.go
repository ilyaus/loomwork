package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ilyaus/loomwork/internal/model"
)

// Test suite layout inside a project:
//
//	test-suites/<suite-id>/
//	  v1/suite.json          # manifest: origin, provenance, incompleteness
//	  v1/tests/tc-001.json   # one file per case
//	  current.json           # current version pointer for the suite
//
// Generated and imported suites share this layout exactly: an imported suite is
// a first-class execution target, so nothing downstream can tell them apart
// except the origin field.
const (
	suiteManifestFileName = "suite.json"
	suiteTestsDirName     = "tests"
	suitePointerFileName  = "current.json"
)

// TestSuiteStore persists test suite versions inside a project.
type TestSuiteStore interface {
	// SaveTestSuite writes the next version of a suite (v1 if it is new) and
	// moves the current pointer. The suite's Version field is assigned by the
	// store, so a caller cannot overwrite a stored version.
	SaveTestSuite(projectRef string, suite *model.TestSuite) (*model.TestSuite, error)
	// LoadTestSuite reads one version with its cases; 0 means the current one.
	LoadTestSuite(projectRef, suiteID string, version int) (*model.TestSuite, error)
	// ListTestSuites returns the current version of every suite, manifest only.
	ListTestSuites(projectRef string) ([]*model.TestSuite, error)
	// TestSuiteHistory returns every retained version, oldest first, manifest
	// only.
	TestSuiteHistory(projectRef, suiteID string) ([]*model.TestSuite, error)
}

// TestSuitePointer is the test-suites/<suite-id>/current.json document.
type TestSuitePointer struct {
	SuiteID        string                `json:"suite_id"`
	CurrentVersion int                   `json:"current_version"`
	Versions       []int                 `json:"versions"`
	Origin         model.TestSuiteOrigin `json:"origin"`
	Incomplete     bool                  `json:"incomplete"`
	UpdatedAt      time.Time             `json:"updated_at"`
}

// SaveTestSuite writes a suite as a new version. Storing an incomplete suite is
// allowed on purpose — including one with no cases at all: the flag and its
// reasons travel with the version so the gap is visible, which is the point of
// flagging rather than rejecting. Normalize is the only gate, so what the store
// accepts and what the model considers well formed never drift apart.
func (d *DirStore) SaveTestSuite(projectRef string, suite *model.TestSuite) (*model.TestSuite, error) {
	if suite == nil {
		return nil, fmt.Errorf("test suite is required")
	}
	if err := suite.Normalize(); err != nil {
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	release, err := lockDir(d.dir)
	if err != nil {
		return nil, err
	}
	defer release()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return nil, err
	}
	if err := d.ensureLayoutLocked(project.ID); err != nil {
		return nil, err
	}
	suiteDir, err := d.testSuiteDir(project.ID, suite.SuiteID)
	if err != nil {
		return nil, err
	}
	pointer, err := readTestSuitePointer(suiteDir, suite.SuiteID)
	if err != nil {
		return nil, err
	}

	stored := *suite
	stored.Cases = append([]model.TestCase(nil), suite.Cases...)
	stored.Version = pointer.CurrentVersion + 1
	if stored.CreatedAt.IsZero() {
		stored.CreatedAt = time.Now().UTC()
	}

	versionDir := filepath.Join(suiteDir, fmt.Sprintf("v%d", stored.Version))
	if err := os.MkdirAll(filepath.Join(versionDir, suiteTestsDirName), 0o755); err != nil {
		return nil, fmt.Errorf("create test suite directory %s: %w", versionDir, err)
	}
	for _, testCase := range stored.Cases {
		payload, err := json.MarshalIndent(testCase, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("encode test case %s: %w", testCase.ID, err)
		}
		if err := writeFileAtomic(filepath.Join(versionDir, suiteTestsDirName, testCase.ID+".json"), payload); err != nil {
			return nil, err
		}
	}
	manifest := stored.Manifest()
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode test suite %s: %w", stored.Ref(), err)
	}
	if err := writeFileAtomic(filepath.Join(versionDir, suiteManifestFileName), payload); err != nil {
		return nil, err
	}

	pointer.SuiteID = stored.SuiteID
	pointer.CurrentVersion = stored.Version
	pointer.Versions = append(pointer.Versions, stored.Version)
	pointer.Origin = stored.Origin
	pointer.Incomplete = stored.Incomplete
	pointer.UpdatedAt = time.Now().UTC()
	if err := writeTestSuitePointer(suiteDir, pointer); err != nil {
		return nil, err
	}

	project.UpdatedAt = time.Now().UTC()
	if err := d.writeLocked(project); err != nil {
		return nil, err
	}
	return &stored, nil
}

// LoadTestSuite reads one version with its cases (0 = current).
func (d *DirStore) LoadTestSuite(projectRef, suiteID string, version int) (*model.TestSuite, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return nil, err
	}
	suiteDir, err := d.testSuiteDir(project.ID, suiteID)
	if err != nil {
		return nil, err
	}
	pointer, err := readTestSuitePointer(suiteDir, suiteID)
	if err != nil {
		return nil, err
	}
	if pointer.CurrentVersion == 0 {
		return nil, fmt.Errorf("test suite %q in project %s: %w", suiteID, project.Name, ErrNotFound)
	}
	if version == 0 {
		version = pointer.CurrentVersion
	}
	return readTestSuite(suiteDir, suiteID, version, true)
}

// ListTestSuites returns the current version of every suite, manifest only.
func (d *DirStore) ListTestSuites(projectRef string) ([]*model.TestSuite, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(d.ProjectDir(project.ID), TestSuitesDirName)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read test suites directory %s: %w", root, err)
	}
	suites := make([]*model.TestSuite, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		suiteDir := filepath.Join(root, entry.Name())
		pointer, err := readTestSuitePointer(suiteDir, entry.Name())
		if err != nil {
			return nil, err
		}
		if pointer.CurrentVersion == 0 {
			continue
		}
		suite, err := readTestSuite(suiteDir, entry.Name(), pointer.CurrentVersion, false)
		if err != nil {
			return nil, err
		}
		suites = append(suites, suite)
	}
	sort.Slice(suites, func(i, j int) bool { return suites[i].SuiteID < suites[j].SuiteID })
	return suites, nil
}

// TestSuiteHistory returns every retained version, oldest first, manifest only.
func (d *DirStore) TestSuiteHistory(projectRef, suiteID string) ([]*model.TestSuite, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return nil, err
	}
	suiteDir, err := d.testSuiteDir(project.ID, suiteID)
	if err != nil {
		return nil, err
	}
	pointer, err := readTestSuitePointer(suiteDir, suiteID)
	if err != nil {
		return nil, err
	}
	if pointer.CurrentVersion == 0 {
		return nil, fmt.Errorf("test suite %q in project %s: %w", suiteID, project.Name, ErrNotFound)
	}
	versions := append([]int(nil), pointer.Versions...)
	sort.Ints(versions)
	history := make([]*model.TestSuite, 0, len(versions))
	for _, version := range versions {
		suite, err := readTestSuite(suiteDir, suiteID, version, false)
		if err != nil {
			return nil, err
		}
		history = append(history, suite)
	}
	return history, nil
}

// TestSuitePointer returns the current-version pointer for one suite.
func (d *DirStore) TestSuitePointer(projectRef, suiteID string) (TestSuitePointer, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return TestSuitePointer{}, err
	}
	suiteDir, err := d.testSuiteDir(project.ID, suiteID)
	if err != nil {
		return TestSuitePointer{}, err
	}
	return readTestSuitePointer(suiteDir, suiteID)
}

// testSuiteDir validates the id before it becomes a path component, so a caller
// cannot walk out of the suites directory with something like "../../other".
func (d *DirStore) testSuiteDir(projectID, suiteID string) (string, error) {
	id, err := model.NormalizeSuiteID(suiteID)
	if err != nil {
		return "", err
	}
	return filepath.Join(d.ProjectDir(projectID), TestSuitesDirName, id), nil
}

func readTestSuite(suiteDir, suiteID string, version int, withCases bool) (*model.TestSuite, error) {
	versionDir := filepath.Join(suiteDir, fmt.Sprintf("v%d", version))
	path := filepath.Join(versionDir, suiteManifestFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("test suite %s v%d: %w", suiteID, version, ErrNotFound)
		}
		return nil, fmt.Errorf("read test suite %s: %w", path, err)
	}
	var suite model.TestSuite
	if err := json.Unmarshal(raw, &suite); err != nil {
		return nil, fmt.Errorf("parse test suite %s: %w", path, err)
	}
	if !withCases {
		return &suite, nil
	}
	cases := make([]model.TestCase, 0, len(suite.CaseIDs))
	for _, id := range suite.CaseIDs {
		casePath := filepath.Join(versionDir, suiteTestsDirName, id+".json")
		rawCase, err := os.ReadFile(casePath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, fmt.Errorf("test case %s of suite %s v%d: %w", id, suiteID, version, ErrNotFound)
			}
			return nil, fmt.Errorf("read test case %s: %w", casePath, err)
		}
		var testCase model.TestCase
		if err := json.Unmarshal(rawCase, &testCase); err != nil {
			return nil, fmt.Errorf("parse test case %s: %w", casePath, err)
		}
		cases = append(cases, testCase)
	}
	suite.Cases = cases
	return &suite, nil
}

func readTestSuitePointer(suiteDir, suiteID string) (TestSuitePointer, error) {
	path := filepath.Join(suiteDir, suitePointerFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TestSuitePointer{SuiteID: suiteID, Versions: []int{}}, nil
		}
		return TestSuitePointer{}, fmt.Errorf("read test suite pointer %s: %w", path, err)
	}
	var pointer TestSuitePointer
	if err := json.Unmarshal(raw, &pointer); err != nil {
		return TestSuitePointer{}, fmt.Errorf("parse test suite pointer %s: %w", path, err)
	}
	return pointer, nil
}

func writeTestSuitePointer(suiteDir string, pointer TestSuitePointer) error {
	if pointer.Versions == nil {
		pointer.Versions = []int{}
	}
	payload, err := json.MarshalIndent(pointer, "", "  ")
	if err != nil {
		return fmt.Errorf("encode test suite pointer: %w", err)
	}
	return writeFileAtomic(filepath.Join(suiteDir, suitePointerFileName), payload)
}
