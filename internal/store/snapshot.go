package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ilyaus/loomwork/internal/model"
)

func (d *DirStore) CreateWithRequirements(project *model.Project, specs []model.RequirementSpec) error {
	if project == nil || !filepath.IsLocal(project.ID) || filepath.Base(project.ID) != project.ID || project.ID == "." {
		return fmt.Errorf("a project with a valid id is required")
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
		if candidate.ID == project.ID || strings.EqualFold(candidate.Name, project.Name) {
			return fmt.Errorf("project name %q already used by project %s", project.Name, candidate.ID)
		}
	}
	if _, err := os.Lstat(d.ProjectDir(project.ID)); err == nil || !os.IsNotExist(err) {
		return fmt.Errorf("project directory %q already exists or is inaccessible", project.ID)
	}
	staging, err := os.MkdirTemp(d.dir, ".import-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	temporary := &DirStore{dir: staging}
	if err := temporary.ensureLayoutLocked(project.ID); err != nil {
		return err
	}
	dir := temporary.requirementsDir(project.ID)
	index := RequirementIndex{Requirements: []RequirementIndexEntry{}, UpdatedAt: time.Now().UTC()}
	seen := map[string]bool{}
	for i, spec := range specs {
		id := spec.ID
		if id == "" {
			id = fmt.Sprintf("req-%03d", i+1)
		}
		id, err := model.NormalizeRequirementID(id)
		if err != nil {
			return err
		}
		if seen[id] {
			return fmt.Errorf("duplicate requirement id %s in imported project", id)
		}
		seen[id] = true
		requirement, err := model.NewRequirement(id, spec)
		if err != nil {
			return err
		}
		if err := writeRequirement(dir, requirement); err != nil {
			return err
		}
		index.Requirements = append(index.Requirements, RequirementIndexEntry{
			ID: requirement.ID, CurrentVersion: 1, Versions: []int{1}, Status: requirement.Status, UpdatedAt: requirement.CreatedAt,
		})
	}
	if err := writeRequirementIndex(dir, index); err != nil {
		return err
	}
	project.Index = &model.ProjectIndex{Requirements: len(specs), ActiveRequirements: countActive(index)}
	if err := temporary.writeLocked(project); err != nil {
		return err
	}
	if err := os.Rename(temporary.ProjectDir(project.ID), d.ProjectDir(project.ID)); err != nil {
		return fmt.Errorf("publish imported project: %w", err)
	}
	return nil
}
