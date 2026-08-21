package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/analysis"
	"github.com/ilyaus/loomwork/internal/model"
)

const analysisPayload = `{
	"verdict": "ready-with-gaps",
	"spec_in_sync": false,
	"summary": "testable but the error catalog is missing",
	"gaps": ["no error catalog"],
	"open_questions": ["is order creation idempotent?"],
	"extracted_requirements": [
		{"text": "POST /orders returns 201 with the order id", "source_type": "github", "source_ref": "https://github.com/x/y/blob/main/api.md", "tags": ["api"]}
	]
}`

// newAnalysisWorkspace creates a workspace whose ollama provider is an httptest
// server that answers every chat request with the canned analysis, so the run
// path is exercised end to end through the real adapter without a live model.
func newAnalysisWorkspace(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		response := map[string]any{
			"model":             "qwen3:8b",
			"message":           map[string]string{"role": "assistant", "content": analysisPayload},
			"done":              true,
			"done_reason":       "stop",
			"prompt_eval_count": 100,
			"eval_count":        50,
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	home := t.TempDir()
	configJSON := `{"providers":{"ollama":{"kind":"ollama","baseUrl":"` + server.URL + `"}}}`
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(configJSON), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return home
}

func newSourceFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "api.md")
	if err := os.WriteFile(path, []byte("POST /orders creates an order."), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return path
}

func TestAnalysisRunLifecycle(t *testing.T) {
	home := newAnalysisWorkspace(t)
	exec(t, home, "project", "create", "--name", "checkout",
		"--source", "name=api spec,type=github,url=https://github.com/x/y/blob/main/api.md,local="+newSourceFile(t))

	out := exec(t, home, "analysis", "run", "--project", "checkout", "--model", "ollama/qwen3:8b")
	if !strings.Contains(out, "verdict: ready-with-gaps") || !strings.Contains(out, "spec in sync: false") {
		t.Errorf("output = %q, want the verdict and sync flag", out)
	}
	if !strings.Contains(out, "no error catalog") || !strings.Contains(out, "is order creation idempotent?") {
		t.Errorf("output = %q, want the gaps and open questions", out)
	}
	if !strings.Contains(out, "extracted 1 requirement(s)") {
		t.Errorf("output = %q, want the extraction summary", out)
	}

	// The analysis lands as a doc artifact and the requirement in the store.
	var requirements []model.Requirement
	decodeJSON(t, exec(t, home, "requirement", "list", "--project", "checkout", "--json"), &requirements)
	if len(requirements) != 1 {
		t.Fatalf("requirements = %+v, want the extracted requirement", requirements)
	}
	requirement := requirements[0]
	if requirement.Origin != model.RequirementOriginExtracted {
		t.Errorf("origin = %q, want %q", requirement.Origin, model.RequirementOriginExtracted)
	}
	if requirement.Metadata["provider"] != "ollama" || requirement.Metadata["model"] != "qwen3:8b" {
		t.Errorf("metadata = %v, want provider and model provenance", requirement.Metadata)
	}
	shown := exec(t, home, "artifact", "show", "--project", "checkout", "--artifact", analysis.ArtifactName)
	if !strings.Contains(shown, "ready-with-gaps") {
		t.Errorf("output = %q, want the stored analysis JSON", shown)
	}

	// JSON output carries the full result for scripting.
	var result analysis.Result
	decodeJSON(t, exec(t, home, "analysis", "run", "--project", "checkout",
		"--model", "ollama/qwen3:8b", "--no-extract", "--json"), &result)
	if result.Analysis.Verdict != analysis.VerdictReadyWithGaps || result.Artifact.Version != 2 {
		t.Errorf("result = %+v, want the parsed analysis as revision 2", result)
	}
	if len(result.Requirements) != 0 {
		t.Errorf("requirements = %+v, want none with --no-extract", result.Requirements)
	}
}

func TestAnalysisImportLifecycle(t *testing.T) {
	home := t.TempDir()
	exec(t, home, "project", "create", "--name", "checkout")

	file := filepath.Join(t.TempDir(), "review.json")
	if err := os.WriteFile(file, []byte(analysisPayload), 0o644); err != nil {
		t.Fatalf("write analysis: %v", err)
	}

	out := exec(t, home, "analysis", "import", "--project", "checkout", "--file", file, "--tags", "manual")
	if !strings.Contains(out, "verdict: ready-with-gaps") || !strings.Contains(out, "extracted 1 requirement(s)") {
		t.Errorf("output = %q, want the verdict and extraction summary", out)
	}

	var requirements []model.Requirement
	decodeJSON(t, exec(t, home, "requirement", "list", "--project", "checkout", "--json"), &requirements)
	if len(requirements) != 1 || requirements[0].Origin != model.RequirementOriginExtracted {
		t.Fatalf("requirements = %+v, want one extracted requirement", requirements)
	}
	if requirements[0].Metadata["origin"] != "manual-import" || requirements[0].Metadata["importedFrom"] != file {
		t.Errorf("metadata = %v, want manual-import provenance", requirements[0].Metadata)
	}

	var project model.Project
	decodeJSON(t, exec(t, home, "project", "show", "--project", "checkout", "--json"), &project)
	if len(project.Artifacts) != 1 || project.Artifacts[0].Type != model.ArtifactTypeDoc {
		t.Fatalf("artifacts = %+v, want the stored analysis doc", project.Artifacts)
	}
	if len(project.Artifacts[0].Tags) != 1 || project.Artifacts[0].Tags[0] != "manual" {
		t.Errorf("tags = %v, want the --tags value", project.Artifacts[0].Tags)
	}
}

func TestAnalysisValidation(t *testing.T) {
	home := t.TempDir()
	exec(t, home, "project", "create", "--name", "alpha")
	exec(t, home, "project", "create", "--name", "beta",
		"--source", "name=spec,type=github,url=https://github.com/x/y/blob/main/api.md")

	badFile := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(badFile, []byte(`{"verdict":"maybe","gaps":[],"open_questions":[]}`), 0o644); err != nil {
		t.Fatalf("write analysis: %v", err)
	}

	tests := []struct {
		args    []string
		wantErr string
	}{
		{args: []string{"analysis", "run"}, wantErr: "--project and --model are required"},
		{args: []string{"analysis", "run", "--project", "alpha"}, wantErr: "--project and --model are required"},
		{args: []string{"analysis", "run", "--project", "beta", "--model", "bogus/m"}, wantErr: "unknown provider kind"},
		{args: []string{"analysis", "run", "--project", "alpha", "--model", "ollama/m"}, wantErr: "has no document sources"},
		{args: []string{"analysis", "run", "--project", "nope", "--model", "ollama/m"}, wantErr: "not found"},
		{args: []string{"analysis", "import", "--project", "alpha"}, wantErr: "--project and --file are required"},
		{args: []string{"analysis", "import", "--file", "x.json"}, wantErr: "--project and --file are required"},
		{args: []string{"analysis", "import", "--project", "alpha", "--file", filepath.Join(home, "absent.json")}, wantErr: "read --file"},
		{args: []string{"analysis", "import", "--project", "alpha", "--file", badFile}, wantErr: "unknown verdict"},
		{args: []string{"analysis", "nope"}, wantErr: `unknown subcommand "nope"`},
	}
	for _, test := range tests {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			if got := execErr(t, home, test.args...); !strings.Contains(got, test.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", got, test.wantErr)
			}
		})
	}
}
