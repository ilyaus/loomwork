// Package store persists projects. The workbench ships a filesystem store that
// lays every project out as a directory (see DirStore); the ProjectStore
// interface exists so object storage or a database can replace it without
// touching orchestration.
package store

import (
	"errors"

	"github.com/ilyaus/loomwork/internal/model"
)

// ErrNotFound is returned (possibly wrapped) when a stored entity does not
// exist. Callers name the entity in the wrapping message.
var ErrNotFound = errors.New("store: not found")

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

var (
	_ ProjectStore     = (*DirStore)(nil)
	_ RequirementStore = (*DirStore)(nil)
)
