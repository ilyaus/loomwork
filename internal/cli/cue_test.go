package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/cuenote"
	"github.com/ilyaus/loomwork/internal/model"
)

// cueNoteStub serves the documented cue-note contract from a fixed cue set.
func cueNoteStub(t *testing.T, cues ...cuenote.Cue) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if id, found := strings.CutPrefix(r.URL.Path, "/api/v1/cues/"); found {
			for _, cue := range cues {
				if cue.ID == id {
					_ = json.NewEncoder(w).Encode(cue)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path != "/api/v1/cues" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		search := strings.ToLower(r.URL.Query().Get("q"))
		matched := make([]cuenote.Cue, 0, len(cues))
		for _, cue := range cues {
			if search == "" || strings.Contains(strings.ToLower(cue.Name+cue.Body), search) {
				matched = append(matched, cue)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"cues": matched})
	}))
	t.Cleanup(server.Close)
	return server
}

// ollamaStub answers /api/chat with a canned completion and records the prompt.
func ollamaStub(t *testing.T, prompts *[]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		for _, message := range payload.Messages {
			if message.Role == "user" {
				*prompts = append(*prompts, message.Content)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":       "m",
			"message":     map[string]string{"role": "assistant", "content": "stub answer"},
			"done":        true,
			"done_reason": "stop",
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// writeCueHome prepares a workspace whose cue-note and ollama endpoints are stubs.
func writeCueHome(t *testing.T, cueNoteURL, ollamaURL string) string {
	t.Helper()
	home := t.TempDir()
	config := fmt.Sprintf(`{"providers":{"ollama":{"kind":"ollama","baseUrl":%q,"defaultModel":"m"}},
	  "cuenote":{"baseUrl":%q}}`, ollamaURL, cueNoteURL)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return home
}

func TestCueListAndShow(t *testing.T) {
	cues := cueNoteStub(t,
		cuenote.Cue{ID: "cue-1", Name: "triage", Body: "Summarize {{service}} errors", Tags: []string{"ops"}},
		cuenote.Cue{ID: "cue-2", Name: "review", Body: "Review this diff", Variables: []string{"unused"}},
	)
	home := writeCueHome(t, cues.URL, "http://127.0.0.1:1")

	listed := exec(t, home, "cue", "list")
	if !strings.Contains(listed, "cue-1\ttriage") || !strings.Contains(listed, "vars: service") ||
		!strings.Contains(listed, "tags: ops") {
		t.Errorf("output = %q, want the cue id, name, variables, and tags", listed)
	}

	var listPayload struct {
		Cues []cuenote.Cue `json:"cues"`
	}
	decodeJSON(t, exec(t, home, "cue", "list", "--json"), &listPayload)
	if len(listPayload.Cues) != 2 {
		t.Fatalf("cues = %+v, want both cues", listPayload.Cues)
	}

	// A search narrows the listing server-side.
	decodeJSON(t, exec(t, home, "cue", "list", "--search", "triage", "--json"), &listPayload)
	if len(listPayload.Cues) != 1 || listPayload.Cues[0].ID != "cue-1" {
		t.Errorf("cues = %+v, want only the matching cue", listPayload.Cues)
	}

	// show resolves by id and by exact, case-insensitive name.
	byID := exec(t, home, "cue", "show", "--cue", "cue-1")
	if !strings.Contains(byID, "Summarize {{service}} errors") || !strings.Contains(byID, "variables: service") {
		t.Errorf("output = %q, want the body and variables", byID)
	}
	var shown cuenote.Cue
	decodeJSON(t, exec(t, home, "cue", "show", "--cue", "TRIAGE", "--json"), &shown)
	if shown.ID != "cue-1" {
		t.Errorf("cue = %+v, want a case-insensitive name match", shown)
	}

	// A cue whose body has no placeholders falls back to the declared list.
	decodeJSON(t, exec(t, home, "cue", "show", "--cue", "cue-2", "--json"), &shown)
	if len(shown.Variables) != 1 || shown.Variables[0] != "unused" {
		t.Errorf("cue = %+v, want the declared variables preserved", shown)
	}

	if got := execErr(t, home, "cue", "show"); !strings.Contains(got, "--cue is required") {
		t.Errorf("error = %q, want a required reference error", got)
	}
	if got := execErr(t, home, "cue", "show", "--cue", "absent"); !strings.Contains(got, "not found") {
		t.Errorf("error = %q, want a not-found error", got)
	}
}

func TestCueListEmptyAndUnreachableService(t *testing.T) {
	empty := cueNoteStub(t)
	home := writeCueHome(t, empty.URL, "http://127.0.0.1:1")
	if out := exec(t, home, "cue", "list"); !strings.Contains(out, "no cues") {
		t.Errorf("output = %q, want an empty-listing message", out)
	}

	unreachable := writeCueHome(t, "http://127.0.0.1:1", "http://127.0.0.1:1")
	got := execErr(t, unreachable, "cue", "list")
	if !strings.Contains(got, "cuenote list cues") || !strings.Contains(got, "127.0.0.1:1") {
		t.Errorf("error = %q, want an actionable connection error naming the endpoint", got)
	}
}

func TestRunWithCue(t *testing.T) {
	cues := cueNoteStub(t,
		cuenote.Cue{ID: "cue-1", Name: "triage", Body: "Summarize {{service}} errors since {{since}}"},
	)
	var prompts []string
	llm := ollamaStub(t, &prompts)
	home := writeCueHome(t, cues.URL, llm.URL)

	exec(t, home, "project", "create", "--name", "alpha")
	exec(t, home, "artifact", "add", "--project", "alpha", "--name", "a.log", "--type", "log", "--content", "ERROR")

	var result struct {
		Generated model.Artifact `json:"generated"`
	}
	decodeJSON(t, exec(t, home, "run", "--project", "alpha", "--artifact", "a.log", "--model", "ollama/m",
		"--cue", "triage", "--var", "service=api", "--var", "since=yesterday", "--json"), &result)

	if len(prompts) == 0 || prompts[len(prompts)-1] != "Summarize api errors since yesterday" {
		t.Fatalf("prompts = %q, want the rendered cue body", prompts)
	}
	metadata := result.Generated.Metadata
	if metadata["cue"] != "triage" || metadata["cueId"] != "cue-1" {
		t.Errorf("metadata = %v, want cue provenance", metadata)
	}
	if metadata["provider"] != "ollama" {
		t.Errorf("metadata = %v, want the orchestrator's own keys preserved", metadata)
	}
}

func TestRunCueValidation(t *testing.T) {
	cues := cueNoteStub(t,
		cuenote.Cue{ID: "cue-1", Name: "triage", Body: "Summarize {{service}} errors"},
		cuenote.Cue{ID: "cue-2", Name: "Triage", Body: "duplicate name"},
		cuenote.Cue{ID: "cue-3", Name: "blank", Body: "   "},
	)
	var prompts []string
	llm := ollamaStub(t, &prompts)
	home := writeCueHome(t, cues.URL, llm.URL)
	exec(t, home, "project", "create", "--name", "alpha")
	exec(t, home, "artifact", "add", "--project", "alpha", "--name", "a.log", "--type", "log", "--content", "ERROR")

	base := []string{"run", "--project", "alpha", "--artifact", "a.log", "--model", "ollama/m"}
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"prompt and cue", []string{"--prompt", "p", "--cue", "cue-1"}, "exactly one of --prompt"},
		{"var without cue", []string{"--prompt", "p", "--var", "a=b"}, "--var applies to --cue only"},
		{"missing variable", []string{"--cue", "cue-1"}, "unresolved template variables: service"},
		{"ambiguous name", []string{"--cue", "triage", "--var", "service=api"}, "ambiguous"},
		{"unknown cue", []string{"--cue", "absent"}, "not found"},
		{"empty render", []string{"--cue", "cue-3"}, "renders to an empty prompt"},
		{"malformed var", []string{"--cue", "cue-1", "--var", "service"}, "expected key=value"},
		{"duplicate var", []string{"--cue", "cue-1", "--var", "service=a", "--var", "service=b"}, "supplied twice"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := execErr(t, home, append(append([]string{}, base...), test.args...)...); !strings.Contains(got, test.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", got, test.wantErr)
			}
		})
	}
	if len(prompts) != 0 {
		t.Errorf("prompts = %q, want no provider call for a rejected run", prompts)
	}
}
