package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/projectimport"
)

func TestProjectImportCLI(t *testing.T) {
	home, source := t.TempDir(), t.TempDir()
	for _, dir := range []string{".specify", "specs/001-example"} {
		if err := os.MkdirAll(filepath.Join(source, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(source, "specs/001-example/spec.md"), []byte("# Example\n- **FR-001**: Return the order\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var preview projectimport.Preview
	decodeJSON(t, exec(t, home, "project", "import", "--path", source, "--preview", "--json"), &preview)
	if preview.Requirements != 1 {
		t.Fatalf("preview = %+v", preview)
	}
	var projects []model.Project
	decodeJSON(t, exec(t, home, "project", "list", "--json"), &projects)
	if len(projects) != 0 {
		t.Fatal("preview wrote a project")
	}
	var result projectimport.Result
	decodeJSON(t, exec(t, home, "project", "import", "--path", source, "--name", "example", "--features", "001-example", "--json"), &result)
	if result.Project.Import == nil || !result.Project.Import.RequirementsReadOnly {
		t.Fatalf("project = %+v", result.Project)
	}
	var output, errors bytes.Buffer
	if err := Run([]string{"requirement", "update", "--home", home, "--project", "example", "--requirement", "req-001", "--text", "Change"}, &output, &errors); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("update = %v", err)
	}
	exec(t, home, "artifact", "add", "--project", "example", "--name", "local-test.md", "--type", "spec", "--content", "Local test")
}
