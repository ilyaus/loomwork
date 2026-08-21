package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ilyaus/loomwork/internal/model"
)

// Project directory layout. A project is a directory, not a document, so the
// entities of later phases (agent definitions, test suites, executor configs,
// reports) each own a subfolder and never contend for one large JSON file.
const (
	ProjectFileName         = "project.json"
	RequirementsDirName     = "requirements"
	AgentDefinitionsDirName = "agent-definitions"
	TestSuitesDirName       = "test-suites"
	ExecutorConfigDirName   = "executor-config"
	ReportsDirName          = "reports"

	requirementIndexFileName = "index.json"
)

// ProjectSubdirs lists the subfolders created inside every project directory.
func ProjectSubdirs() []string {
	return []string{
		RequirementsDirName,
		AgentDefinitionsDirName,
		TestSuitesDirName,
		ExecutorConfigDirName,
		ReportsDirName,
	}
}

// RequirementStore persists requirement versions inside a project. Every version
// is a discrete retrievable snapshot; nothing is ever overwritten or deleted.
type RequirementStore interface {
	CreateRequirement(projectRef string, spec model.RequirementSpec) (*model.Requirement, error)
	// UpdateRequirement writes the next version and marks the previous one
	// superseded.
	UpdateRequirement(projectRef, requirementID string, spec model.RequirementSpec) (*model.Requirement, error)
	// SetRequirementStatus updates one version's status; version 0 means the
	// current version.
	SetRequirementStatus(projectRef, requirementID string, version int, status model.RequirementStatus) (*model.Requirement, error)
	// LoadRequirement reads one version; version 0 means the current version.
	LoadRequirement(projectRef, requirementID string, version int) (*model.Requirement, error)
	// ListRequirements returns the current version of every requirement, by id.
	ListRequirements(projectRef string) ([]*model.Requirement, error)
	// RequirementHistory returns every retained version of one requirement.
	RequirementHistory(projectRef, requirementID string) ([]*model.Requirement, error)
}

// RequirementIndex is the requirements/index.json document: the current-version
// pointer per requirement id, so a project loads without reading every version.
type RequirementIndex struct {
	Requirements []RequirementIndexEntry `json:"requirements"`
	UpdatedAt    time.Time               `json:"updated_at"`
}

// RequirementIndexEntry points at one requirement's current version and records
// the versions retained for it.
type RequirementIndexEntry struct {
	ID             string                  `json:"id"`
	CurrentVersion int                     `json:"current_version"`
	Versions       []int                   `json:"versions"`
	Status         model.RequirementStatus `json:"status"`
	UpdatedAt      time.Time               `json:"updated_at"`
}

func (i RequirementIndex) find(id string) (RequirementIndexEntry, bool) {
	for _, entry := range i.Requirements {
		if strings.EqualFold(entry.ID, id) {
			return entry, true
		}
	}
	return RequirementIndexEntry{}, false
}

func (i *RequirementIndex) upsert(entry RequirementIndexEntry) {
	for position := range i.Requirements {
		if strings.EqualFold(i.Requirements[position].ID, entry.ID) {
			i.Requirements[position] = entry
			return
		}
	}
	i.Requirements = append(i.Requirements, entry)
	sort.Slice(i.Requirements, func(a, b int) bool { return i.Requirements[a].ID < i.Requirements[b].ID })
}

// DirStore lays every project out as its own directory holding a project.json
// metadata/index document plus one subfolder per entity family. Writes are
// atomic (temp file + rename) and read-modify-write cycles are serialized across
// processes by a lock file in the projects root, matching FileStore's guarantees.
// Projects written by FileStore as flat <id>.json documents are still readable
// and are migrated to a directory the first time they are written.
type DirStore struct {
	mu  sync.RWMutex
	dir string
}

// NewDirStore creates the projects root directory if needed.
func NewDirStore(dir string) (*DirStore, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("store directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create store directory %s: %w", dir, err)
	}
	return &DirStore{dir: dir}, nil
}

// Root returns the projects root directory.
func (d *DirStore) Root() string { return d.dir }

// ProjectDir returns the directory holding one project's entities.
func (d *DirStore) ProjectDir(id string) string { return filepath.Join(d.dir, id) }

// Create persists a new project directory, rejecting duplicate names.
func (d *DirStore) Create(project *model.Project) error {
	if project == nil {
		return fmt.Errorf("project is required")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	release, err := lockDir(d.dir)
	if err != nil {
		return err
	}
	defer release()

	existing, err := d.listLocked()
	if err != nil {
		return err
	}
	for _, candidate := range existing {
		if strings.EqualFold(candidate.Name, project.Name) {
			return fmt.Errorf("project name %q already used by project %s", project.Name, candidate.ID)
		}
	}
	return d.writeLocked(project)
}

// Save overwrites an existing project document.
func (d *DirStore) Save(project *model.Project) error {
	if project == nil {
		return fmt.Errorf("project is required")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	release, err := lockDir(d.dir)
	if err != nil {
		return err
	}
	defer release()
	return d.writeLocked(project)
}

// Update applies mutate to the resolved project while holding the store lock, so
// the load and the save form one atomic cycle even across processes.
func (d *DirStore) Update(ref string, mutate func(project *model.Project) error) (*model.Project, error) {
	if mutate == nil {
		return nil, fmt.Errorf("mutate function is required")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	release, err := lockDir(d.dir)
	if err != nil {
		return nil, err
	}
	defer release()

	project, err := d.resolveLocked(ref)
	if err != nil {
		return nil, err
	}
	if err := mutate(project); err != nil {
		return nil, err
	}
	if err := d.writeLocked(project); err != nil {
		return nil, err
	}
	return project, nil
}

// Load reads a project by id.
func (d *DirStore) Load(id string) (*model.Project, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.readByIDLocked(id)
}

// FindByName reads a project by name (case-insensitive).
func (d *DirStore) FindByName(name string) (*model.Project, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	projects, err := d.listLocked()
	if err != nil {
		return nil, err
	}
	for _, project := range projects {
		if strings.EqualFold(project.Name, name) {
			return project, nil
		}
	}
	return nil, fmt.Errorf("project %q: %w", name, ErrNotFound)
}

// Resolve finds a project by id, then by name.
func (d *DirStore) Resolve(ref string) (*model.Project, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.resolveLocked(ref)
}

// List returns every project, ordered by name.
func (d *DirStore) List() ([]*model.Project, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.listLocked()
}

func (d *DirStore) resolveLocked(ref string) (*model.Project, error) {
	if project, err := d.readByIDLocked(ref); err == nil {
		return project, nil
	}
	projects, err := d.listLocked()
	if err != nil {
		return nil, err
	}
	for _, project := range projects {
		if strings.EqualFold(project.Name, ref) {
			return project, nil
		}
	}
	return nil, fmt.Errorf("project %q: %w", ref, ErrNotFound)
}

// readByIDLocked prefers the directory layout and falls back to a flat document
// left by FileStore.
func (d *DirStore) readByIDLocked(id string) (*model.Project, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("project id is required")
	}
	project, err := readProjectFile(filepath.Join(d.ProjectDir(id), ProjectFileName))
	if err == nil {
		return project, nil
	}
	legacyPath := filepath.Join(d.dir, id+".json")
	if _, statErr := os.Stat(legacyPath); statErr != nil {
		return nil, err
	}
	return readProjectFile(legacyPath)
}

func (d *DirStore) listLocked() ([]*model.Project, error) {
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read store directory %s: %w", d.dir, err)
	}
	projects := make([]*model.Project, 0, len(entries))
	for _, entry := range entries {
		var path string
		switch {
		case entry.IsDir():
			path = filepath.Join(d.dir, entry.Name(), ProjectFileName)
			if _, statErr := os.Stat(path); statErr != nil {
				continue
			}
		case strings.HasSuffix(entry.Name(), ".json"):
			path = filepath.Join(d.dir, entry.Name())
		default:
			continue
		}
		project, err := readProjectFile(path)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	return projects, nil
}

func (d *DirStore) writeLocked(project *model.Project) error {
	if err := d.ensureLayoutLocked(project.ID); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(project, "", "  ")
	if err != nil {
		return fmt.Errorf("encode project %s: %w", project.ID, err)
	}
	if err := writeFileAtomic(filepath.Join(d.ProjectDir(project.ID), ProjectFileName), payload); err != nil {
		return err
	}
	// A project that previously lived as a flat document now lives in its
	// directory; leaving the old file behind would list the project twice.
	legacy := filepath.Join(d.dir, project.ID+".json")
	if _, statErr := os.Stat(legacy); statErr == nil {
		if err := os.Remove(legacy); err != nil {
			return fmt.Errorf("remove migrated project document %s: %w", legacy, err)
		}
	}
	return nil
}

func (d *DirStore) ensureLayoutLocked(id string) error {
	root := d.ProjectDir(id)
	for _, name := range ProjectSubdirs() {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			return fmt.Errorf("create project directory %s: %w", filepath.Join(root, name), err)
		}
	}
	return nil
}

func readProjectFile(path string) (*model.Project, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("project file %s: %w", path, ErrNotFound)
		}
		return nil, fmt.Errorf("read project %s: %w", path, err)
	}
	var project model.Project
	if err := json.Unmarshal(raw, &project); err != nil {
		return nil, fmt.Errorf("parse project %s: %w", path, err)
	}
	return &project, nil
}

// CreateRequirement writes v1 of a new requirement and advances the index.
func (d *DirStore) CreateRequirement(projectRef string, spec model.RequirementSpec) (*model.Requirement, error) {
	var created *model.Requirement
	err := d.withRequirements(projectRef, func(project *model.Project, dir string, index *RequirementIndex) error {
		requirement, err := model.NewRequirement(nextRequirementID(*index), spec)
		if err != nil {
			return err
		}
		if err := writeRequirement(dir, requirement); err != nil {
			return err
		}
		index.upsert(RequirementIndexEntry{
			ID:             requirement.ID,
			CurrentVersion: requirement.Version,
			Versions:       []int{requirement.Version},
			Status:         requirement.Status,
			UpdatedAt:      requirement.CreatedAt,
		})
		created = requirement
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// UpdateRequirement writes the next version and marks the previous one
// superseded, keeping it retrievable.
func (d *DirStore) UpdateRequirement(projectRef, requirementID string, spec model.RequirementSpec) (*model.Requirement, error) {
	var updated *model.Requirement
	err := d.withRequirements(projectRef, func(project *model.Project, dir string, index *RequirementIndex) error {
		entry, ok := index.find(requirementID)
		if !ok {
			return fmt.Errorf("requirement %q in project %s: %w", requirementID, project.Name, ErrNotFound)
		}
		current, err := readRequirement(dir, entry.ID, entry.CurrentVersion)
		if err != nil {
			return err
		}
		next, err := current.NextVersion(spec)
		if err != nil {
			return err
		}
		current.Status = model.RequirementStatusSuperseded
		if err := writeRequirement(dir, current); err != nil {
			return err
		}
		if err := writeRequirement(dir, next); err != nil {
			return err
		}
		entry.CurrentVersion = next.Version
		entry.Versions = append(entry.Versions, next.Version)
		entry.Status = next.Status
		entry.UpdatedAt = next.CreatedAt
		index.upsert(entry)
		updated = next
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// SetRequirementStatus updates the status of one version (0 = current version).
func (d *DirStore) SetRequirementStatus(projectRef, requirementID string, version int, status model.RequirementStatus) (*model.Requirement, error) {
	var changed *model.Requirement
	err := d.withRequirements(projectRef, func(project *model.Project, dir string, index *RequirementIndex) error {
		entry, ok := index.find(requirementID)
		if !ok {
			return fmt.Errorf("requirement %q in project %s: %w", requirementID, project.Name, ErrNotFound)
		}
		target := version
		if target == 0 {
			target = entry.CurrentVersion
		}
		requirement, err := readRequirement(dir, entry.ID, target)
		if err != nil {
			return err
		}
		if err := requirement.SetStatus(status); err != nil {
			return err
		}
		if err := writeRequirement(dir, requirement); err != nil {
			return err
		}
		if requirement.Version == entry.CurrentVersion {
			entry.Status = requirement.Status
			entry.UpdatedAt = time.Now().UTC()
			index.upsert(entry)
		}
		changed = requirement
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changed, nil
}

// LoadRequirement reads one requirement version (0 = current version).
func (d *DirStore) LoadRequirement(projectRef, requirementID string, version int) (*model.Requirement, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return nil, err
	}
	dir := d.requirementsDir(project.ID)
	index, err := readRequirementIndex(dir)
	if err != nil {
		return nil, err
	}
	entry, ok := index.find(requirementID)
	if !ok {
		return nil, fmt.Errorf("requirement %q in project %s: %w", requirementID, project.Name, ErrNotFound)
	}
	if version == 0 {
		version = entry.CurrentVersion
	}
	return readRequirement(dir, entry.ID, version)
}

// ListRequirements returns the current version of every requirement, by id.
func (d *DirStore) ListRequirements(projectRef string) ([]*model.Requirement, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return nil, err
	}
	dir := d.requirementsDir(project.ID)
	index, err := readRequirementIndex(dir)
	if err != nil {
		return nil, err
	}
	requirements := make([]*model.Requirement, 0, len(index.Requirements))
	for _, entry := range index.Requirements {
		requirement, err := readRequirement(dir, entry.ID, entry.CurrentVersion)
		if err != nil {
			return nil, err
		}
		requirements = append(requirements, requirement)
	}
	return requirements, nil
}

// RequirementHistory returns every retained version of one requirement, oldest
// first.
func (d *DirStore) RequirementHistory(projectRef, requirementID string) ([]*model.Requirement, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return nil, err
	}
	dir := d.requirementsDir(project.ID)
	index, err := readRequirementIndex(dir)
	if err != nil {
		return nil, err
	}
	entry, ok := index.find(requirementID)
	if !ok {
		return nil, fmt.Errorf("requirement %q in project %s: %w", requirementID, project.Name, ErrNotFound)
	}
	versions := append([]int(nil), entry.Versions...)
	sort.Ints(versions)
	history := make([]*model.Requirement, 0, len(versions))
	for _, version := range versions {
		requirement, err := readRequirement(dir, entry.ID, version)
		if err != nil {
			return nil, err
		}
		history = append(history, requirement)
	}
	return history, nil
}

// RequirementIndex returns the current-version pointers for a project.
func (d *DirStore) RequirementIndex(projectRef string) (RequirementIndex, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return RequirementIndex{}, err
	}
	return readRequirementIndex(d.requirementsDir(project.ID))
}

// withRequirements runs mutate against a project's requirements folder under the
// store lock, then persists the index and the project's cached counts.
func (d *DirStore) withRequirements(projectRef string, mutate func(project *model.Project, dir string, index *RequirementIndex) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	release, err := lockDir(d.dir)
	if err != nil {
		return err
	}
	defer release()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return err
	}
	if err := d.ensureLayoutLocked(project.ID); err != nil {
		return err
	}
	dir := d.requirementsDir(project.ID)
	index, err := readRequirementIndex(dir)
	if err != nil {
		return err
	}
	if err := mutate(project, dir, &index); err != nil {
		return err
	}
	index.UpdatedAt = time.Now().UTC()
	if err := writeRequirementIndex(dir, index); err != nil {
		return err
	}
	project.Index = &model.ProjectIndex{
		Requirements:       len(index.Requirements),
		ActiveRequirements: countActive(index),
	}
	project.UpdatedAt = time.Now().UTC()
	return d.writeLocked(project)
}

func (d *DirStore) requirementsDir(projectID string) string {
	return filepath.Join(d.ProjectDir(projectID), RequirementsDirName)
}

func countActive(index RequirementIndex) int {
	active := 0
	for _, entry := range index.Requirements {
		if entry.Status == model.RequirementStatusActive {
			active++
		}
	}
	return active
}

// nextRequirementID continues the req-NNN sequence used by the index.
func nextRequirementID(index RequirementIndex) string {
	highest := 0
	for _, entry := range index.Requirements {
		digits := strings.TrimPrefix(entry.ID, "req-")
		if number, err := strconv.Atoi(digits); err == nil && number > highest {
			highest = number
		}
	}
	return fmt.Sprintf("req-%03d", highest+1)
}

func requirementPath(dir, id string, version int) string {
	return filepath.Join(dir, fmt.Sprintf("%s.v%d.json", id, version))
}

func readRequirement(dir, id string, version int) (*model.Requirement, error) {
	path := requirementPath(dir, id, version)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("requirement %s v%d: %w", id, version, ErrNotFound)
		}
		return nil, fmt.Errorf("read requirement %s: %w", path, err)
	}
	var requirement model.Requirement
	if err := json.Unmarshal(raw, &requirement); err != nil {
		return nil, fmt.Errorf("parse requirement %s: %w", path, err)
	}
	return &requirement, nil
}

func writeRequirement(dir string, requirement *model.Requirement) error {
	payload, err := json.MarshalIndent(requirement, "", "  ")
	if err != nil {
		return fmt.Errorf("encode requirement %s v%d: %w", requirement.ID, requirement.Version, err)
	}
	return writeFileAtomic(requirementPath(dir, requirement.ID, requirement.Version), payload)
}

func readRequirementIndex(dir string) (RequirementIndex, error) {
	path := filepath.Join(dir, requirementIndexFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return RequirementIndex{Requirements: []RequirementIndexEntry{}}, nil
		}
		return RequirementIndex{}, fmt.Errorf("read requirement index %s: %w", path, err)
	}
	var index RequirementIndex
	if err := json.Unmarshal(raw, &index); err != nil {
		return RequirementIndex{}, fmt.Errorf("parse requirement index %s: %w", path, err)
	}
	return index, nil
}

func writeRequirementIndex(dir string, index RequirementIndex) error {
	if index.Requirements == nil {
		index.Requirements = []RequirementIndexEntry{}
	}
	payload, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("encode requirement index: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create requirements directory %s: %w", dir, err)
	}
	return writeFileAtomic(filepath.Join(dir, requirementIndexFileName), payload)
}
