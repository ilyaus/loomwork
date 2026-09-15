package projectimport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/store"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		".specify/memory/constitution.md":                        "# Principles\nKeep the source unchanged.\n",
		"specs/001-first/spec.md":                                "# Feature Specification: First\n\n## Requirements\n- **FR-001**: First requirement\n  with continuation.\n\n- **FR-002**: Second requirement\n\n## Success Criteria\n- **SC-001**: A measurable outcome\n",
		"specs/002-second/spec.md":                               "# Feature Specification: Second\n\n- **FR-001**: A different requirement\n",
		"specs/001-first/contracts/openapi.yaml":                 "openapi: 3.0.3\npaths: {}\n",
		"specs/001-first/sdd-qa/run/scenarios/api/FR-001.api.md": "# Scenario\nGiven a request, expect success.\n",
		"config/ignored.json":                                    "not project documentation",
	}
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestSpecKitSnapshotPreservesSourcesAndLocksRequirements(t *testing.T) {
	root := fixture(t)
	d, err := store.NewDirStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := New(d)
	request := Request{Format: "spec-kit", Path: root}
	preview, err := service.Preview(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Features) != 2 || preview.Requirements != 4 || preview.TestDocuments != 1 {
		t.Fatalf("preview = %+v", preview)
	}
	projects, _ := d.List()
	if len(projects) != 0 {
		t.Fatal("preview wrote a project")
	}
	result, err := service.Import(request)
	if err != nil {
		t.Fatal(err)
	}
	project := result.Project
	if project.Import == nil || !project.Import.RequirementsReadOnly || project.Import.SourcePath != root {
		t.Fatalf("import = %+v", project.Import)
	}
	requirements, err := d.ListRequirements(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(requirements) != 4 || requirements[0].ID == requirements[3].ID || requirements[0].Metadata["source_id"] != requirements[3].Metadata["source_id"] {
		t.Fatalf("requirements = %+v", requirements)
	}
	if requirements[0].ID != "001-FR-001" || requirements[3].ID != "002-FR-001" {
		t.Fatalf("IDs must retain spec and source numbers: %+v", requirements)
	}
	subset, err := service.Import(Request{Format: "spec-kit", Path: root, Name: "subset", Features: []string{"002-second"}})
	if err != nil {
		t.Fatal(err)
	}
	subsetRequirements, err := d.ListRequirements(subset.Project.ID)
	if err != nil || len(subsetRequirements) != 1 || subsetRequirements[0].ID != requirements[3].ID {
		t.Fatalf("feature selection changed IDs: %+v, %v", subsetRequirements, err)
	}
	if !strings.Contains(requirements[0].Text, "with continuation.") {
		t.Fatalf("multiline text = %q", requirements[0].Text)
	}
	for name, mutate := range map[string]func() error{
		"create": func() error {
			_, err := d.CreateRequirement(project.ID, model.RequirementSpec{Text: "changed"})
			return err
		},
		"amend": func() error {
			_, err := d.AmendRequirement(project.ID, requirements[0].ID, model.RequirementSpec{Text: "changed"})
			return err
		},
		"version": func() error {
			_, err := d.UpdateRequirement(project.ID, requirements[0].ID, model.RequirementSpec{Text: "changed"})
			return err
		},
		"status": func() error {
			_, err := d.SetRequirementStatus(project.ID, requirements[0].ID, 0, model.RequirementStatusObsolete)
			return err
		},
	} {
		if err := mutate(); err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Errorf("%s = %v", name, err)
		}
	}
	name := "specs/001-first/sdd-qa/run/scenarios/api/FR-001.api.md"
	artifact, ok := project.LatestArtifact(name)
	if !ok || artifact.Body.Ref != "" || artifact.Metadata["source_path"] != name {
		t.Fatalf("artifact = %+v", artifact)
	}
	if _, err := d.Update(project.ID, func(p *model.Project) error {
		_, err := p.AddArtifact(model.ArtifactSpec{Name: name, Type: artifact.Type, Body: model.Body{Content: "Local changes"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, name))
	if err != nil || string(raw) != artifact.Body.Content {
		t.Fatalf("source changed: %q, %v", raw, err)
	}
	if _, err := service.Import(request); err == nil {
		t.Fatal("duplicate name accepted")
	}
	projects, err = d.List()
	if err != nil || len(projects) != 2 {
		t.Fatalf("projects = %v, %v", projects, err)
	}
}

func TestSpecKitSelectionAndValidation(t *testing.T) {
	root := fixture(t)
	service := New(nil)
	preview, err := service.Preview(Request{Format: "spec-kit", Path: root, Features: []string{"002-second"}})
	if err != nil || len(preview.Features) != 1 || preview.Requirements != 1 || preview.TestDocuments != 0 {
		t.Fatalf("selection = %+v, %v", preview, err)
	}
	for _, request := range []Request{
		{Format: "unknown", Path: root},
		{Format: "spec-kit", Path: t.TempDir()},
		{Format: "spec-kit", Path: root, Features: []string{"../escape"}},
	} {
		if _, err := service.Preview(request); err == nil {
			t.Errorf("accepted %+v", request)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "specs/002-second/spec.md"), []byte("- **FR-001**: A\n- **FR-001**: B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Preview(Request{Format: "spec-kit", Path: root}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate = %v", err)
	}
}

func TestSpecKitRejectsSymlinkEscapeAndIgnoresCodeExamples(t *testing.T) {
	root := fixture(t)
	external := filepath.Join(t.TempDir(), "external.md")
	if err := os.WriteFile(external, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "specs/001-first/contracts/link.md")); err != nil {
		t.Fatal(err)
	}
	preview, err := New(nil).Preview(Request{Format: "spec-kit", Path: root})
	if err != nil || len(preview.Warnings) == 0 {
		t.Fatalf("symlink warning = %+v, %v", preview, err)
	}
	requirements, err := parseRequirements("```md\n- **FR-999**: example only\n```\n- **FR-001**: Real\n", "001-first", "specs/001-first/spec.md")
	if err != nil || len(requirements) != 1 || requirements[0].Metadata["source_id"] != "FR-001" {
		t.Fatalf("parsed = %+v, %v", requirements, err)
	}
}
