package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/provider"
	"github.com/ilyaus/loomwork/internal/store"
)

// exec runs a command line against an isolated workspace and returns stdout.
func exec(t *testing.T, home string, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if err := Run(append(args, "--home", home), &stdout, &stderr); err != nil {
		t.Fatalf("Run(%v) = %v (stderr: %s)", args, err, stderr.String())
	}
	return stdout.String()
}

// execErr runs a command line expecting failure and returns the error text.
func execErr(t *testing.T, home string, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := Run(append(args, "--home", home), &stdout, &stderr)
	if err == nil {
		t.Fatalf("Run(%v) succeeded, want an error (stdout: %s)", args, stdout.String())
	}
	return err.Error()
}

func TestRunHelp(t *testing.T) {
	for _, args := range [][]string{{}, {"--help"}, {"help"}, {"-h"}} {
		var stdout, stderr bytes.Buffer
		if err := Run(args, &stdout, &stderr); err != nil {
			t.Fatalf("Run(%v) = %v", args, err)
		}
		if !strings.Contains(stdout.String(), "loomwork <group> <command>") {
			t.Errorf("Run(%v) stdout = %q, want usage", args, stdout.String())
		}
	}
}

func TestRunUnknownCommands(t *testing.T) {
	home := t.TempDir()
	if got := execErr(t, home, "nope"); !strings.Contains(got, `unknown command "nope"`) {
		t.Errorf("error = %q, want an unknown command error", got)
	}
	if got := execErr(t, home, "project", "nope"); !strings.Contains(got, `unknown subcommand "nope"`) {
		t.Errorf("error = %q, want an unknown subcommand error", got)
	}
	var stdout, stderr bytes.Buffer
	if err := Run([]string{"artifact"}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "missing subcommand") {
		t.Errorf("error = %v, want a missing subcommand error", err)
	}
}

func TestProjectAndArtifactVerticalSlice(t *testing.T) {
	home := t.TempDir()

	created := exec(t, home, "project", "create", "--name", "triage", "--description", "log triage", "--tags", "prod,ops")
	if !strings.Contains(created, "created project triage") {
		t.Fatalf("output = %q, want a creation confirmation", created)
	}

	logFile := filepath.Join(t.TempDir(), "api.log")
	if err := os.WriteFile(logFile, []byte("ERROR timeout\n"), 0o644); err != nil {
		t.Fatalf("write log file: %v", err)
	}
	if out := exec(t, home, "artifact", "add", "--project", "triage", "--name", "api.log",
		"--type", "log", "--file", logFile, "--tags", "prod", "--pin"); !strings.Contains(out, "added artifact api.log") {
		t.Fatalf("output = %q, want an artifact confirmation", out)
	}
	if out := exec(t, home, "artifact", "add", "--project", "triage", "--name", "spec.md",
		"--type", "spec", "--content", "the spec"); !strings.Contains(out, "v1") {
		t.Fatalf("output = %q, want the first revision", out)
	}
	// A second artifact of the same name becomes the next revision.
	if out := exec(t, home, "artifact", "add", "--project", "triage", "--name", "spec.md",
		"--type", "spec", "--content", "the revised spec"); !strings.Contains(out, "v2") {
		t.Fatalf("output = %q, want the second revision", out)
	}

	listed := exec(t, home, "artifact", "list", "--project", "triage")
	if lines := strings.Split(strings.TrimSpace(listed), "\n"); len(lines) != 2 || !strings.Contains(listed, "* ") {
		t.Errorf("output = %q, want two latest artifacts with the pinned marker", listed)
	}
	all := exec(t, home, "artifact", "list", "--project", "triage", "--all-versions")
	if lines := strings.Split(strings.TrimSpace(all), "\n"); len(lines) != 3 {
		t.Errorf("lines = %v, want every revision", lines)
	}

	shown := exec(t, home, "artifact", "show", "--project", "triage", "--artifact", "api.log")
	if !strings.Contains(shown, "ERROR timeout") || !strings.Contains(shown, "pinned: true") {
		t.Errorf("output = %q, want the artifact content and pin state", shown)
	}

	if out := exec(t, home, "artifact", "unpin", "--project", "triage", "--artifact", "api.log"); !strings.Contains(out, "unpinned api.log") {
		t.Errorf("output = %q, want an unpin confirmation", out)
	}
	if out := exec(t, home, "artifact", "pin", "--project", "triage", "--artifact", "spec.md"); !strings.Contains(out, "pinned spec.md") {
		t.Errorf("output = %q, want a pin confirmation", out)
	}

	// Projects and artifacts survive across process invocations.
	var project model.Project
	decodeJSON(t, exec(t, home, "project", "show", "--project", "triage", "--json"), &project)
	if project.Name != "triage" || len(project.Artifacts) != 3 {
		t.Fatalf("project = %+v, want the persisted project", project)
	}
	if len(project.PinnedArtifacts()) != 1 || project.PinnedArtifacts()[0].Name != "spec.md" {
		t.Errorf("pinned = %+v, want only spec.md pinned", project.PinnedArtifacts())
	}

	var projects []model.Project
	decodeJSON(t, exec(t, home, "project", "list", "--json"), &projects)
	if len(projects) != 1 || projects[0].ID != project.ID {
		t.Errorf("projects = %+v, want the single created project", projects)
	}

	// The workspace layout is created under --home: one directory per project.
	if _, err := os.Stat(filepath.Join(home, "projects", project.ID, store.ProjectFileName)); err != nil {
		t.Errorf("project document missing: %v", err)
	}
	for _, name := range store.ProjectSubdirs() {
		if _, err := os.Stat(filepath.Join(home, "projects", project.ID, name)); err != nil {
			t.Errorf("project subfolder %s missing: %v", name, err)
		}
	}
}

func TestProjectCreateRejectsDuplicateAndEmptyNames(t *testing.T) {
	home := t.TempDir()
	exec(t, home, "project", "create", "--name", "alpha")
	if got := execErr(t, home, "project", "create", "--name", "alpha"); !strings.Contains(got, "already used") {
		t.Errorf("error = %q, want a duplicate rejection", got)
	}
	if got := execErr(t, home, "project", "create"); !strings.Contains(got, "name is required") {
		t.Errorf("error = %q, want a required name error", got)
	}
}

func TestCommandsRequireReferences(t *testing.T) {
	home := t.TempDir()
	exec(t, home, "project", "create", "--name", "alpha")

	tests := []struct {
		args    []string
		wantErr string
	}{
		{args: []string{"project", "show"}, wantErr: "--project is required"},
		{args: []string{"project", "show", "--project", "nope"}, wantErr: "not found"},
		{args: []string{"artifact", "add", "--name", "n", "--content", "c"}, wantErr: "--project is required"},
		{args: []string{"artifact", "list"}, wantErr: "--project is required"},
		{args: []string{"artifact", "show", "--project", "alpha"}, wantErr: "--artifact are required"},
		{args: []string{"artifact", "show", "--project", "alpha", "--artifact", "nope"}, wantErr: "not found in project"},
		{args: []string{"artifact", "pin", "--project", "alpha"}, wantErr: "--artifact are required"},
		{args: []string{"run", "--project", "alpha", "--artifact", "a"}, wantErr: "--model are required"},
		{args: []string{"project", "list", "extra"}, wantErr: "unexpected arguments"},
	}
	for _, test := range tests {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			if got := execErr(t, home, test.args...); !strings.Contains(got, test.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", got, test.wantErr)
			}
		})
	}
}

func TestArtifactAddBodySelection(t *testing.T) {
	home := t.TempDir()
	exec(t, home, "project", "create", "--name", "alpha")

	if got := execErr(t, home, "artifact", "add", "--project", "alpha", "--name", "n"); !strings.Contains(got, "exactly one of --content") {
		t.Errorf("error = %q, want a body selection error", got)
	}
	if got := execErr(t, home, "artifact", "add", "--project", "alpha", "--name", "n",
		"--content", "c", "--ref", "/tmp/x"); !strings.Contains(got, "exactly one of --content") {
		t.Errorf("error = %q, want a body selection error", got)
	}
	if got := execErr(t, home, "artifact", "add", "--project", "alpha", "--name", "n",
		"--file", filepath.Join(home, "absent.txt"), "--type", "log"); !strings.Contains(got, "read --file") {
		t.Errorf("error = %q, want a file read error", got)
	}
	if got := execErr(t, home, "artifact", "add", "--project", "alpha", "--name", "n",
		"--content", "c", "--type", "nonsense"); !strings.Contains(got, "unknown artifact type") {
		t.Errorf("error = %q, want an artifact type error", got)
	}

	// A --ref artifact stores the path and reads it back on show.
	referenced := filepath.Join(t.TempDir(), "results.json")
	if err := os.WriteFile(referenced, []byte(`{"passed":1}`), 0o644); err != nil {
		t.Fatalf("write reference: %v", err)
	}
	exec(t, home, "artifact", "add", "--project", "alpha", "--name", "results",
		"--type", "test-result", "--ref", referenced)
	if out := exec(t, home, "artifact", "show", "--project", "alpha", "--artifact", "results"); !strings.Contains(out, `{"passed":1}`) {
		t.Errorf("output = %q, want the referenced file contents", out)
	}
}

func TestRunPromptValidation(t *testing.T) {
	home := t.TempDir()
	exec(t, home, "project", "create", "--name", "alpha")
	exec(t, home, "artifact", "add", "--project", "alpha", "--name", "a.log", "--type", "log", "--content", "x")

	if got := execErr(t, home, "run", "--project", "alpha", "--artifact", "a.log",
		"--model", "ollama/m"); !strings.Contains(got, "exactly one of --prompt") {
		t.Errorf("error = %q, want a prompt selection error", got)
	}
	if got := execErr(t, home, "run", "--project", "alpha", "--artifact", "a.log",
		"--model", "ollama/m", "--prompt", "p", "--prompt-file", "f"); !strings.Contains(got, "exactly one of --prompt") {
		t.Errorf("error = %q, want a prompt selection error", got)
	}
	if got := execErr(t, home, "run", "--project", "alpha", "--artifact", "a.log",
		"--model", "bogus/m", "--prompt", "p"); !strings.Contains(got, "unknown provider kind") {
		t.Errorf("error = %q, want a selector error", got)
	}
	if got := execErr(t, home, "run", "--project", "alpha", "--artifact", "a.log",
		"--model", "ollama/m", "--prompt", "p", "--type", "nonsense"); !strings.Contains(got, "unknown artifact type") {
		t.Errorf("error = %q, want an output type error", got)
	}
}

func TestProvidersCommand(t *testing.T) {
	home := t.TempDir()
	text := exec(t, home, "providers")
	if !strings.Contains(text, "ollama") || !strings.Contains(text, "lmstudio") || !strings.Contains(text, home) {
		t.Fatalf("output = %q, want the default local providers and the workspace path", text)
	}

	configFile := filepath.Join(home, "config.json")
	if err := os.WriteFile(configFile, []byte(`{"providers":{
      "ollama":{"kind":"ollama","baseUrl":"http://localhost:11434","defaultModel":"qwen3:8b"},
      "azure":{"kind":"azure","azure":{"endpoint":"https://example.openai.azure.com","deployment":"gpt4o"}}}}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	presetsFile := filepath.Join(home, "presets.json")
	if err := os.WriteFile(presetsFile, []byte(`{"entries":[
      {"provider":"ollama","model":"qwen3:8b","defaults":{"temperature":0.2},
       "presets":{"code-review":{"temperature":0.1}}}]}`), 0o644); err != nil {
		t.Fatalf("write presets: %v", err)
	}

	var payload struct {
		Home      string `json:"home"`
		Providers []struct {
			Name       string   `json:"name"`
			Kind       string   `json:"kind"`
			BaseURL    string   `json:"baseUrl"`
			Status     string   `json:"status"`
			PresetKeys []string `json:"presetKeys"`
		} `json:"providers"`
		PresetGroups []string `json:"presetGroups"`
	}
	decodeJSON(t, exec(t, home, "providers", "--json"), &payload)
	if len(payload.Providers) != 2 {
		t.Fatalf("providers = %+v, want both declarations", payload.Providers)
	}
	byName := map[string]string{}
	endpoints := map[string]string{}
	for _, view := range payload.Providers {
		byName[view.Name] = view.Status
		endpoints[view.Name] = view.BaseURL
	}
	if endpoints["azure"] != "https://example.openai.azure.com" {
		t.Errorf("azure endpoint = %q, want the endpoint from its kind-specific config", endpoints["azure"])
	}
	if byName["ollama"] != "configured" {
		t.Errorf("ollama status = %q, want configured", byName["ollama"])
	}
	if !strings.Contains(byName["azure"], "unavailable") || !strings.Contains(byName["azure"], provider.EnvAzureAPIKey) {
		t.Errorf("azure status = %q, want it reported as unavailable without a key in the environment", byName["azure"])
	}
	t.Setenv(provider.EnvAzureAPIKey, "test-key")
	decodeJSON(t, exec(t, home, "providers", "--json"), &payload)
	for _, view := range payload.Providers {
		if view.Name == "azure" && view.Status != "configured" {
			t.Errorf("azure status = %q, want configured once the key is in the environment", view.Status)
		}
	}
	if len(payload.PresetGroups) != 1 || payload.PresetGroups[0] != "ollama/qwen3:8b" {
		t.Errorf("preset groups = %v, want the configured entry", payload.PresetGroups)
	}
}

func TestInvalidWorkspaceFilesAreReported(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if got := execErr(t, home, "project", "list"); !strings.Contains(got, "parse config") {
		t.Fatalf("error = %q, want a config parse error", got)
	}

	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(`{"providers":{"azure":{"kind":"azure"}}}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if got := execErr(t, home, "project", "list"); !strings.Contains(got, "azure.endpoint") {
		t.Fatalf("error = %q, want provider validation to fail", got)
	}

	home = t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "presets.json"), []byte(`{"entries":[{"provider":"nope","model":"m"}]}`), 0o644); err != nil {
		t.Fatalf("write presets: %v", err)
	}
	if got := execErr(t, home, "project", "list"); !strings.Contains(got, "unknown provider kind") {
		t.Fatalf("error = %q, want preset validation to fail", got)
	}
}

func TestSplitList(t *testing.T) {
	if got := splitList(" a , b ,, c "); strings.Join(got, "|") != "a|b|c" {
		t.Errorf("splitList = %v, want trimmed non-empty values", got)
	}
	if got := splitList("   "); got != nil {
		t.Errorf("splitList = %v, want nil", got)
	}
}

func decodeJSON(t *testing.T, raw string, out any) {
	t.Helper()
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
}
