package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
)

func TestRequirementLifecycleVerticalSlice(t *testing.T) {
	home := t.TempDir()
	exec(t, home, "project", "create", "--name", "checkout",
		"--source", "name=spec,type=github,url=https://github.com/org/repo/blob/main/spec.md")

	var created model.Requirement
	decodeJSON(t, exec(t, home, "requirement", "create", "--project", "checkout",
		"--text", "Cart totals include tax", "--source-type", "ADO", "--source-ref", "AB#12",
		"--tags", "cart,tax", "--json"), &created)
	if created.ID != "req-001" || created.Version != 1 || created.Status != model.RequirementStatusActive {
		t.Fatalf("created = %+v, want an active req-001 v1", created)
	}
	if created.SourceType != model.SourceTypeADO || created.Origin != model.RequirementOriginAuthored {
		t.Errorf("created = %+v, want a normalized ADO source and an authored origin", created)
	}

	// An update creates the next version and supersedes the previous one.
	var updated model.Requirement
	decodeJSON(t, exec(t, home, "requirement", "update", "--project", "checkout",
		"--requirement", "req-001", "--text", "Cart totals include tax and shipping", "--json"), &updated)
	if updated.Version != 2 || updated.SourceRef != "AB#12" {
		t.Fatalf("updated = %+v, want v2 inheriting the source reference", updated)
	}

	// Both versions stay retrievable, the older one as superseded.
	var first model.Requirement
	decodeJSON(t, exec(t, home, "requirement", "show", "--project", "checkout",
		"--requirement", "req-001", "--version", "1", "--json"), &first)
	if first.Version != 1 || first.Text != "Cart totals include tax" || first.Status != model.RequirementStatusSuperseded {
		t.Fatalf("v1 = %+v, want the retained superseded snapshot", first)
	}
	var current model.Requirement
	decodeJSON(t, exec(t, home, "requirement", "show", "--project", "checkout",
		"--requirement", "req-001", "--json"), &current)
	if current.Version != 2 {
		t.Fatalf("current = v%d, want v2 by default", current.Version)
	}
	var history []model.Requirement
	decodeJSON(t, exec(t, home, "requirement", "show", "--project", "checkout",
		"--requirement", "req-001", "--history", "--json"), &history)
	if len(history) != 2 || history[0].Version != 1 {
		t.Fatalf("history = %+v, want both versions oldest first", history)
	}

	// Marking a requirement obsolete retains it and its versions.
	if out := exec(t, home, "requirement", "set-status", "--project", "checkout",
		"--requirement", "req-001", "--status", "obsolete"); !strings.Contains(out, "is now obsolete") {
		t.Errorf("output = %q, want a status confirmation", out)
	}
	var listed []model.Requirement
	decodeJSON(t, exec(t, home, "requirement", "list", "--project", "checkout", "--json"), &listed)
	if len(listed) != 1 || listed[0].Status != model.RequirementStatusObsolete {
		t.Fatalf("listed = %+v, want the obsolete requirement retained", listed)
	}
	decodeJSON(t, exec(t, home, "requirement", "list", "--project", "checkout", "--status", "active", "--json"), &listed)
	if len(listed) != 0 {
		t.Fatalf("listed = %+v, want no active requirements", listed)
	}

	// Extracted requirements share the schema and the store with authored ones.
	textFile := filepath.Join(t.TempDir(), "req.txt")
	if err := os.WriteFile(textFile, []byte("Guest checkout requires an email address"), 0o644); err != nil {
		t.Fatalf("write text file: %v", err)
	}
	var extracted model.Requirement
	decodeJSON(t, exec(t, home, "requirement", "create", "--project", "checkout",
		"--text-file", textFile, "--origin", "extracted", "--json"), &extracted)
	if extracted.ID != "req-002" || extracted.Origin != model.RequirementOriginExtracted {
		t.Fatalf("extracted = %+v, want req-002 with an extracted origin", extracted)
	}

	// Requirements live in the project's requirements/ subfolder, one file per
	// version, alongside the index pointing at current versions.
	var project model.Project
	decodeJSON(t, exec(t, home, "project", "show", "--project", "checkout", "--json"), &project)
	requirementsDir := filepath.Join(home, "projects", project.ID, "requirements")
	for _, name := range []string{"req-001.v1.json", "req-001.v2.json", "req-002.v1.json", "index.json"} {
		if _, err := os.Stat(filepath.Join(requirementsDir, name)); err != nil {
			t.Errorf("%s missing: %v", name, err)
		}
	}
	if project.Index == nil || project.Index.Requirements != 2 || project.Index.ActiveRequirements != 1 {
		t.Errorf("project index = %+v, want 2 requirements with 1 active", project.Index)
	}

	// Human-readable output carries the tester-facing text.
	if out := exec(t, home, "requirement", "list", "--project", "checkout"); !strings.Contains(out, "Guest checkout requires an email address") {
		t.Errorf("output = %q, want the requirement text listed", out)
	}
	if out := exec(t, home, "requirement", "show", "--project", "checkout", "--requirement", "req-002"); !strings.Contains(out, "origin: extracted") {
		t.Errorf("output = %q, want the origin shown", out)
	}
}

func TestRequirementCommandValidation(t *testing.T) {
	home := t.TempDir()
	exec(t, home, "project", "create", "--name", "alpha")
	exec(t, home, "requirement", "create", "--project", "alpha", "--text", "text")

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "create needs a project", args: []string{"requirement", "create", "--text", "t"}, wantErr: "--project is required"},
		{name: "create needs text", args: []string{"requirement", "create", "--project", "alpha"}, wantErr: "either --text or --text-file"},
		{
			name:    "create rejects both text flags",
			args:    []string{"requirement", "create", "--project", "alpha", "--text", "t", "--text-file", "f"},
			wantErr: "not both",
		},
		{
			name:    "create rejects an unreadable text file",
			args:    []string{"requirement", "create", "--project", "alpha", "--text-file", filepath.Join(home, "absent.txt")},
			wantErr: "read --text-file",
		},
		{
			name:    "create rejects an unknown source type",
			args:    []string{"requirement", "create", "--project", "alpha", "--text", "t", "--source-type", "jira"},
			wantErr: "unknown source type",
		},
		{
			name:    "create rejects an unknown origin",
			args:    []string{"requirement", "create", "--project", "alpha", "--text", "t", "--origin", "guessed"},
			wantErr: "unknown requirement origin",
		},
		{name: "show needs a requirement", args: []string{"requirement", "show", "--project", "alpha"}, wantErr: "--requirement are required"},
		{
			name:    "show rejects version with history",
			args:    []string{"requirement", "show", "--project", "alpha", "--requirement", "req-001", "--version", "1", "--history"},
			wantErr: "not both",
		},
		{
			name:    "show reports a missing requirement",
			args:    []string{"requirement", "show", "--project", "alpha", "--requirement", "req-404"},
			wantErr: "not found",
		},
		{
			name:    "show reports a missing version",
			args:    []string{"requirement", "show", "--project", "alpha", "--requirement", "req-001", "--version", "7"},
			wantErr: "not found",
		},
		{name: "list needs a project", args: []string{"requirement", "list"}, wantErr: "--project is required"},
		{
			name:    "list rejects an unknown status",
			args:    []string{"requirement", "list", "--project", "alpha", "--status", "draft"},
			wantErr: "unknown requirement status",
		},
		{name: "update needs a requirement", args: []string{"requirement", "update", "--project", "alpha"}, wantErr: "--requirement are required"},
		{
			name:    "set-status needs a status",
			args:    []string{"requirement", "set-status", "--project", "alpha", "--requirement", "req-001"},
			wantErr: "unknown requirement status",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := execErr(t, home, test.args...); !strings.Contains(got, test.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", got, test.wantErr)
			}
		})
	}
}

func TestProjectDocumentSources(t *testing.T) {
	home := t.TempDir()
	exec(t, home, "project", "create", "--name", "alpha",
		"--source", "name=spec,type=confluence,url=https://wiki/spec,local=/tmp/spec.md")
	if out := exec(t, home, "project", "source", "--project", "alpha",
		"--source", "name=stories,type=ado,url=https://dev.azure.com/org/proj",
		"--source", "name=readme,type=github,url=https://github.com/org/repo,s3=s3://bucket/readme.md"); !strings.Contains(out, "3 document sources") {
		t.Fatalf("output = %q, want three sources recorded", out)
	}

	var project model.Project
	decodeJSON(t, exec(t, home, "project", "show", "--project", "alpha", "--json"), &project)
	if len(project.Sources) != 3 {
		t.Fatalf("sources = %+v, want three", project.Sources)
	}
	byName := map[string]model.DocumentSource{}
	for _, source := range project.Sources {
		byName[source.Name] = source
	}
	if byName["spec"].Type != model.SourceTypeConfluence || byName["spec"].LocalPath != "/tmp/spec.md" {
		t.Errorf("spec = %+v, want a confluence link with its local copy", byName["spec"])
	}
	if byName["readme"].S3URI != "s3://bucket/readme.md" {
		t.Errorf("readme = %+v, want the S3 copy recorded", byName["readme"])
	}
	if out := exec(t, home, "project", "show", "--project", "alpha"); !strings.Contains(out, "https://dev.azure.com/org/proj") {
		t.Errorf("output = %q, want sources listed", out)
	}

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "source needs a project", args: []string{"project", "source", "--source", "name=a,type=ado,url=u"}, wantErr: "--project is required"},
		{name: "source needs a source", args: []string{"project", "source", "--project", "alpha"}, wantErr: "at least one --source"},
		{
			name:    "rejects a non key=value field",
			args:    []string{"project", "source", "--project", "alpha", "--source", "spec"},
			wantErr: "is not key=value",
		},
		{
			name:    "rejects an unknown field",
			args:    []string{"project", "source", "--project", "alpha", "--source", "name=a,type=ado,link=u"},
			wantErr: "unknown field",
		},
		{
			name:    "rejects an unknown type",
			args:    []string{"project", "source", "--project", "alpha", "--source", "name=a,type=jira,url=u"},
			wantErr: "unknown source type",
		},
		{
			name:    "rejects a source with no location",
			args:    []string{"project", "source", "--project", "alpha", "--source", "name=a,type=ado"},
			wantErr: "needs a url",
		},
		{
			name:    "rejects an empty source value",
			args:    []string{"project", "create", "--name", "beta", "--source", ""},
			wantErr: "must not be empty",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := execErr(t, home, test.args...); !strings.Contains(got, test.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", got, test.wantErr)
			}
		})
	}
}
