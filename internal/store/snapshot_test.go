package store

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
)

func TestSnapshotFailureLeavesNoPartialProject(t *testing.T) {
	d := newStore(t)
	project := newProject(t, "invalid-snapshot")
	if err := d.CreateWithRequirements(project, []model.RequirementSpec{{Text: "Valid"}, {Text: ""}}); err == nil {
		t.Fatal("invalid requirement accepted")
	}
	projects, err := d.List()
	if err != nil || len(projects) != 0 {
		t.Fatalf("projects = %v, %v", projects, err)
	}
	entries, err := os.ReadDir(d.Root())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("partial snapshot directory remains: %s", entry.Name())
		}
	}
}

func TestConcurrentSnapshotsRejectDuplicateNames(t *testing.T) {
	d := newStore(t)
	var workers sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		project := newProject(t, "same-name")
		workers.Add(1)
		go func() {
			defer workers.Done()
			results <- d.CreateWithRequirements(project, []model.RequirementSpec{{Text: "Requirement"}})
		}()
	}
	workers.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("successful duplicate imports = %d", success)
	}
	projects, err := d.List()
	if err != nil || len(projects) != 1 {
		t.Fatalf("projects = %v, %v", projects, err)
	}
}

func TestReportUploadsStayInsideReports(t *testing.T) {
	d, project := seededStore(t)
	outside := t.TempDir()
	for name, target := range map[string]string{"outside": outside, "requirements": "../requirements"} {
		if err := os.Symlink(target, filepath.Join(d.ProjectDir(project.ID), ReportsDirName, name)); err != nil {
			t.Fatal(err)
		}
		if _, err := d.AddReport(project.ID, name+"/escaped.txt", []byte("content")); err == nil {
			t.Fatalf("accepted symlink %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped.txt")); !os.IsNotExist(err) {
		t.Fatalf("escaped report: %v", err)
	}
	content := []byte("original")
	if _, err := d.AddReport(project.ID, "run.txt", content); err != nil {
		t.Fatal(err)
	}
	if _, err := d.AddReport(project.ID, "run.txt", []byte("replacement")); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("replacement = %v", err)
	}
	_, raw, err := d.LoadReport(project.ID, "run.txt")
	if err != nil || string(raw) != string(content) {
		t.Fatalf("report replaced: %s, %v", raw, err)
	}
}
