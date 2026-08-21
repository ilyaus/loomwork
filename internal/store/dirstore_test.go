package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
)

// seededStore returns a store holding one project, ready for requirement work.
func seededStore(t *testing.T) (*DirStore, *model.Project) {
	t.Helper()
	dirStore := newStore(t)
	project := newProject(t, "workbench")
	if err := dirStore.Create(project); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return dirStore, project
}

func TestDirStoreCreatesProjectDirectoryLayout(t *testing.T) {
	dirStore, project := seededStore(t)

	root := dirStore.ProjectDir(project.ID)
	if _, err := os.Stat(filepath.Join(root, ProjectFileName)); err != nil {
		t.Fatalf("project.json missing: %v", err)
	}
	for _, name := range ProjectSubdirs() {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Fatalf("subfolder %s missing: %v", name, err)
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", name)
		}
	}
	// Every entity family of the vision has a home before any entity exists.
	if want := []string{"requirements", "agent-definitions", "test-suites", "executor-config", "reports"}; strings.Join(ProjectSubdirs(), ",") != strings.Join(want, ",") {
		t.Errorf("subdirs = %v, want %v", ProjectSubdirs(), want)
	}
}

func TestDirStoreMigratesFlatProjectDocument(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "projects")
	dirStore, err := NewDirStore(dir)
	if err != nil {
		t.Fatalf("NewDirStore: %v", err)
	}
	project := newProject(t, "legacy")
	payload, err := json.Marshal(project)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, project.ID+".json"), payload, 0o644); err != nil {
		t.Fatalf("write legacy project: %v", err)
	}

	loaded, err := dirStore.Resolve("legacy")
	if err != nil {
		t.Fatalf("Resolve a legacy flat project: %v", err)
	}
	if loaded.ID != project.ID {
		t.Fatalf("resolved = %q, want %q", loaded.ID, project.ID)
	}
	if _, err := dirStore.CreateRequirement("legacy", model.RequirementSpec{Text: "still testable"}); err != nil {
		t.Fatalf("CreateRequirement: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, project.ID+".json")); !os.IsNotExist(err) {
		t.Errorf("stat legacy document = %v, want it migrated away", err)
	}
	projects, err := dirStore.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("projects = %d, want the migrated project listed once", len(projects))
	}
}

func TestRequirementVersioningRetainsEveryVersion(t *testing.T) {
	dirStore, project := seededStore(t)

	first, err := dirStore.CreateRequirement("workbench", model.RequirementSpec{
		Text:       "Login rejects an expired password",
		SourceType: model.SourceTypeADO,
		SourceRef:  "AB#1234",
	})
	if err != nil {
		t.Fatalf("CreateRequirement: %v", err)
	}
	if first.ID != "req-001" || first.Version != 1 {
		t.Fatalf("created = %s v%d, want req-001 v1", first.ID, first.Version)
	}

	second, err := dirStore.UpdateRequirement("workbench", first.ID, model.RequirementSpec{Text: "Login rejects an expired password with a clear message"})
	if err != nil {
		t.Fatalf("UpdateRequirement: %v", err)
	}
	third, err := dirStore.UpdateRequirement("workbench", strings.ToUpper(first.ID), model.RequirementSpec{Text: "Login rejects an expired password and offers a reset link"})
	if err != nil {
		t.Fatalf("UpdateRequirement: %v", err)
	}
	if second.Version != 2 || third.Version != 3 {
		t.Fatalf("versions = %d/%d, want 2 and 3", second.Version, third.Version)
	}

	// Each version stayed a discrete retrievable file.
	requirementsDir := filepath.Join(dirStore.ProjectDir(project.ID), RequirementsDirName)
	for version, wantStatus := range map[int]model.RequirementStatus{
		1: model.RequirementStatusSuperseded,
		2: model.RequirementStatusSuperseded,
		3: model.RequirementStatusActive,
	} {
		if _, err := os.Stat(filepath.Join(requirementsDir, "req-001.v"+strconv.Itoa(version)+".json")); err != nil {
			t.Errorf("version %d file missing: %v", version, err)
		}
		loaded, err := dirStore.LoadRequirement("workbench", "req-001", version)
		if err != nil {
			t.Fatalf("LoadRequirement v%d: %v", version, err)
		}
		if loaded.Status != wantStatus {
			t.Errorf("v%d status = %q, want %q", version, loaded.Status, wantStatus)
		}
	}
	if loaded, err := dirStore.LoadRequirement("workbench", "req-001", 1); err != nil || loaded.Text != first.Text {
		t.Errorf("v1 = %+v (err %v), want the original text preserved", loaded, err)
	}

	// Version 0 means the current version, per the index pointer.
	current, err := dirStore.LoadRequirement("workbench", "req-001", 0)
	if err != nil {
		t.Fatalf("LoadRequirement current: %v", err)
	}
	if current.Version != 3 {
		t.Errorf("current = v%d, want v3", current.Version)
	}

	history, err := dirStore.RequirementHistory("workbench", "req-001")
	if err != nil {
		t.Fatalf("RequirementHistory: %v", err)
	}
	if len(history) != 3 || history[0].Version != 1 || history[2].Version != 3 {
		t.Fatalf("history = %d versions, want v1..v3 oldest first", len(history))
	}

	index, err := dirStore.RequirementIndex("workbench")
	if err != nil {
		t.Fatalf("RequirementIndex: %v", err)
	}
	entry, ok := index.find("req-001")
	if !ok {
		t.Fatal("index entry missing")
	}
	if entry.CurrentVersion != 3 || len(entry.Versions) != 3 {
		t.Errorf("entry = %+v, want a v3 pointer and three retained versions", entry)
	}
	if _, err := os.Stat(filepath.Join(requirementsDir, requirementIndexFileName)); err != nil {
		t.Errorf("requirements index missing: %v", err)
	}
}

func TestRequirementStatusTransitionsThroughTheStore(t *testing.T) {
	dirStore, _ := seededStore(t)
	created, err := dirStore.CreateRequirement("workbench", model.RequirementSpec{Text: "Password reset emails arrive within a minute"})
	if err != nil {
		t.Fatalf("CreateRequirement: %v", err)
	}

	// An obsolete requirement is retained and still readable.
	obsolete, err := dirStore.SetRequirementStatus("workbench", created.ID, 0, model.RequirementStatusObsolete)
	if err != nil {
		t.Fatalf("SetRequirementStatus: %v", err)
	}
	if obsolete.Status != model.RequirementStatusObsolete {
		t.Fatalf("status = %q, want obsolete", obsolete.Status)
	}
	listed, err := dirStore.ListRequirements("workbench")
	if err != nil {
		t.Fatalf("ListRequirements: %v", err)
	}
	if len(listed) != 1 || listed[0].Status != model.RequirementStatusObsolete {
		t.Fatalf("listed = %+v, want the obsolete requirement retained", listed)
	}
	index, err := dirStore.RequirementIndex("workbench")
	if err != nil {
		t.Fatalf("RequirementIndex: %v", err)
	}
	if entry, _ := index.find(created.ID); entry.Status != model.RequirementStatusObsolete {
		t.Errorf("index status = %q, want obsolete", entry.Status)
	}

	// Reactivating and then updating supersedes only the older version.
	if _, err := dirStore.SetRequirementStatus("workbench", created.ID, 1, model.RequirementStatusActive); err != nil {
		t.Fatalf("SetRequirementStatus back to active: %v", err)
	}
	if _, err := dirStore.UpdateRequirement("workbench", created.ID, model.RequirementSpec{Text: "Password reset emails arrive within 30 seconds"}); err != nil {
		t.Fatalf("UpdateRequirement: %v", err)
	}
	if _, err := dirStore.SetRequirementStatus("workbench", created.ID, 1, model.RequirementStatusActive); err == nil {
		t.Error("reactivating a superseded version = nil, want an error")
	}
	if _, err := dirStore.SetRequirementStatus("workbench", created.ID, 0, model.RequirementStatus("draft")); err == nil {
		t.Error("an unknown status = nil, want an error")
	}
}

func TestRequirementErrorsForMissingReferences(t *testing.T) {
	dirStore, _ := seededStore(t)

	tests := []struct {
		name string
		call func() error
	}{
		{name: "load a missing requirement", call: func() error {
			_, err := dirStore.LoadRequirement("workbench", "req-404", 0)
			return err
		}},
		{name: "load a missing version", call: func() error {
			if _, err := dirStore.CreateRequirement("workbench", model.RequirementSpec{Text: "text"}); err != nil {
				return err
			}
			_, err := dirStore.LoadRequirement("workbench", "req-001", 9)
			return err
		}},
		{name: "history of a missing requirement", call: func() error {
			_, err := dirStore.RequirementHistory("workbench", "req-404")
			return err
		}},
		{name: "update a missing requirement", call: func() error {
			_, err := dirStore.UpdateRequirement("workbench", "req-404", model.RequirementSpec{Text: "text"})
			return err
		}},
		{name: "requirements of a missing project", call: func() error {
			_, err := dirStore.ListRequirements("nope")
			return err
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); !errors.Is(err, ErrNotFound) {
				t.Fatalf("error = %v, want ErrNotFound", err)
			}
		})
	}

	if _, err := dirStore.CreateRequirement("workbench", model.RequirementSpec{Text: "  "}); err == nil {
		t.Error("CreateRequirement with empty text = nil, want an error")
	}
}

func TestRequirementIDsAndProjectCountsAdvance(t *testing.T) {
	dirStore, _ := seededStore(t)
	for _, text := range []string{"first", "second", "third"} {
		if _, err := dirStore.CreateRequirement("workbench", model.RequirementSpec{Text: text}); err != nil {
			t.Fatalf("CreateRequirement(%s): %v", text, err)
		}
	}
	if _, err := dirStore.SetRequirementStatus("workbench", "req-002", 0, model.RequirementStatusObsolete); err != nil {
		t.Fatalf("SetRequirementStatus: %v", err)
	}

	requirements, err := dirStore.ListRequirements("workbench")
	if err != nil {
		t.Fatalf("ListRequirements: %v", err)
	}
	ids := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		ids = append(ids, requirement.ID)
	}
	if strings.Join(ids, ",") != "req-001,req-002,req-003" {
		t.Fatalf("ids = %v, want a stable req-NNN sequence ordered by id", ids)
	}

	// The project document caches the counts so a landing view needs no scan.
	project, err := dirStore.Resolve("workbench")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if project.Index == nil || project.Index.Requirements != 3 || project.Index.ActiveRequirements != 2 {
		t.Fatalf("index = %+v, want 3 requirements with 2 active", project.Index)
	}
}
