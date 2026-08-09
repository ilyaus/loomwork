// Package store persists projects. The foundation ships a JSON file store; the
// ProjectStore interface exists so object storage or a database can replace it
// without touching orchestration.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/ilyaus/loomwork/internal/model"
)

// ErrNotFound is returned (possibly wrapped) when a project does not exist.
var ErrNotFound = errors.New("store: project not found")

// ProjectStore persists and retrieves projects.
type ProjectStore interface {
	Create(project *model.Project) error
	Save(project *model.Project) error
	// Update resolves a project, applies mutate, and persists the result as one
	// serialized read-modify-write cycle. Callers that change a project must use
	// Update rather than Load+Save so concurrent writers cannot lose each other's
	// changes. A mutate error leaves the stored project untouched.
	Update(ref string, mutate func(project *model.Project) error) (*model.Project, error)
	Load(id string) (*model.Project, error)
	FindByName(name string) (*model.Project, error)
	// Resolve finds a project by id, then by name.
	Resolve(ref string) (*model.Project, error)
	List() ([]*model.Project, error)
}

// FileStore keeps one JSON document per project in a directory. Writes are
// atomic (temp file + rename) so an interrupted run never truncates a project,
// and read-modify-write cycles are serialized across processes by a lock file so
// concurrent CLI invocations cannot lose each other's changes.
type FileStore struct {
	mu  sync.RWMutex
	dir string
}

// NewFileStore creates the projects directory if needed.
func NewFileStore(dir string) (*FileStore, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("store directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create store directory %s: %w", dir, err)
	}
	return &FileStore{dir: dir}, nil
}

// Create persists a new project, rejecting duplicate names.
func (f *FileStore) Create(project *model.Project) error {
	if project == nil {
		return fmt.Errorf("project is required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	release, err := f.lock()
	if err != nil {
		return err
	}
	defer release()

	existing, err := f.listLocked()
	if err != nil {
		return err
	}
	for _, candidate := range existing {
		if strings.EqualFold(candidate.Name, project.Name) {
			return fmt.Errorf("project name %q already used by project %s", project.Name, candidate.ID)
		}
	}
	return f.writeLocked(project)
}

// Save overwrites an existing project document.
func (f *FileStore) Save(project *model.Project) error {
	if project == nil {
		return fmt.Errorf("project is required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	release, err := f.lock()
	if err != nil {
		return err
	}
	defer release()
	return f.writeLocked(project)
}

// Update applies mutate to the resolved project while holding the store lock, so
// the load and the save form one atomic cycle even across processes.
func (f *FileStore) Update(ref string, mutate func(project *model.Project) error) (*model.Project, error) {
	if mutate == nil {
		return nil, fmt.Errorf("mutate function is required")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	release, err := f.lock()
	if err != nil {
		return nil, err
	}
	defer release()

	project, err := f.resolveLocked(ref)
	if err != nil {
		return nil, err
	}
	if err := mutate(project); err != nil {
		return nil, err
	}
	if err := f.writeLocked(project); err != nil {
		return nil, err
	}
	return project, nil
}

// Load reads a project by id.
func (f *FileStore) Load(id string) (*model.Project, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.readLocked(f.path(id))
}

// FindByName reads a project by name (case-insensitive).
func (f *FileStore) FindByName(name string) (*model.Project, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	projects, err := f.listLocked()
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
func (f *FileStore) Resolve(ref string) (*model.Project, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.resolveLocked(ref)
}

func (f *FileStore) resolveLocked(ref string) (*model.Project, error) {
	if project, err := f.readLocked(f.path(ref)); err == nil {
		return project, nil
	}
	projects, err := f.listLocked()
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

// List returns every project, ordered by name.
func (f *FileStore) List() ([]*model.Project, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.listLocked()
}

func (f *FileStore) listLocked() ([]*model.Project, error) {
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read store directory %s: %w", f.dir, err)
	}
	projects := make([]*model.Project, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		project, err := f.readLocked(filepath.Join(f.dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
	return projects, nil
}

func (f *FileStore) readLocked(path string) (*model.Project, error) {
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

func (f *FileStore) writeLocked(project *model.Project) error {
	payload, err := json.MarshalIndent(project, "", "  ")
	if err != nil {
		return fmt.Errorf("encode project %s: %w", project.ID, err)
	}
	target := f.path(project.ID)
	temp, err := os.CreateTemp(f.dir, ".project-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", f.dir, err)
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()

	if _, err := temp.Write(payload); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temp file %s: %w", tempName, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temp file %s: %w", tempName, err)
	}
	if err := os.Rename(tempName, target); err != nil {
		return fmt.Errorf("persist project %s: %w", target, err)
	}
	return nil
}

func (f *FileStore) path(id string) string {
	return filepath.Join(f.dir, id+".json")
}
