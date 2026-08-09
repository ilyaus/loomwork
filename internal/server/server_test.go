package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ilyaus/loomwork/internal/config"
	"github.com/ilyaus/loomwork/internal/cuenote"
	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/preset"
	"github.com/ilyaus/loomwork/internal/store"
)

type fakeCues struct {
	cues []cuenote.Cue
	err  error
}

func (f *fakeCues) ListCues(_ context.Context, _ cuenote.CueFilter) ([]cuenote.Cue, error) {
	return f.cues, f.err
}

func (f *fakeCues) GetCue(_ context.Context, id string) (cuenote.Cue, error) {
	for _, cue := range f.cues {
		if cue.ID == id {
			return cue, nil
		}
	}
	return cuenote.Cue{}, cuenote.ErrNotFound
}

func (f *fakeCues) CreateCue(_ context.Context, cue cuenote.Cue) (cuenote.Cue, error) {
	return cue, nil
}

func (f *fakeCues) ListNotes(_ context.Context, _ cuenote.NoteFilter) ([]cuenote.Note, error) {
	return nil, nil
}

func (f *fakeCues) GetNote(_ context.Context, id string) (cuenote.Note, error) {
	return cuenote.Note{}, cuenote.ErrNotFound
}

func (f *fakeCues) CreateNote(_ context.Context, note cuenote.Note) (cuenote.Note, error) {
	return note, nil
}

func newTestServer(t *testing.T, cues cuenote.Client) *Server {
	t.Helper()
	projects, err := store.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	presets, err := preset.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cues == nil {
		cues = &fakeCues{}
	}
	return New("/tmp/home", config.Default().WithDefaults(), projects, presets, cues)
}

func doJSON(t *testing.T, handler http.Handler, method, path, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	decoded := map[string]any{}
	if raw := recorder.Body.Bytes(); len(raw) > 0 {
		_ = json.Unmarshal(raw, &decoded)
	}
	return recorder, decoded
}

func TestProjectAndArtifactLifecycle(t *testing.T) {
	server := newTestServer(t, nil)

	recorder, created := doJSON(t, server, http.MethodPost, "/api/projects", `{"name":"demo","description":"d","tags":["x"]}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create project: %d %s", recorder.Code, recorder.Body)
	}
	projectID := created["id"].(string)

	recorder, listed := doJSON(t, server, http.MethodGet, "/api/projects", "")
	if recorder.Code != http.StatusOK || len(listed["projects"].([]any)) != 1 {
		t.Fatalf("list projects: %d %s", recorder.Code, recorder.Body)
	}

	recorder, artifact := doJSON(t, server, http.MethodPost, "/api/projects/"+projectID+"/artifacts",
		`{"name":"contract","type":"spec","content":"POST /v1/things","tags":["api"],"pinned":true}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("add artifact: %d %s", recorder.Code, recorder.Body)
	}
	artifactID := artifact["id"].(string)

	recorder, detail := doJSON(t, server, http.MethodGet, "/api/projects/demo/artifacts/contract", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("get artifact: %d %s", recorder.Code, recorder.Body)
	}
	if detail["content"] != "POST /v1/things" {
		t.Errorf("content = %v", detail["content"])
	}

	recorder, pinned := doJSON(t, server, http.MethodPost, "/api/projects/"+projectID+"/artifacts/"+artifactID+"/pin", `{"pinned":false}`)
	if recorder.Code != http.StatusOK || pinned["pinned"] != false {
		t.Fatalf("unpin: %d %s", recorder.Code, recorder.Body)
	}

	recorder, _ = doJSON(t, server, http.MethodGet, "/api/projects/missing", "")
	if recorder.Code != http.StatusNotFound {
		t.Errorf("missing project: %d", recorder.Code)
	}
}

func TestWorkspaceEndpoint(t *testing.T) {
	server := newTestServer(t, nil)
	recorder, payload := doJSON(t, server, http.MethodGet, "/api/workspace", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("workspace: %d %s", recorder.Code, recorder.Body)
	}
	if payload["home"] != "/tmp/home" {
		t.Errorf("home = %v", payload["home"])
	}
	if len(payload["providers"].([]any)) == 0 {
		t.Error("expected default providers")
	}
	types := payload["artifactTypes"].([]any)
	if len(types) != len(model.ArtifactTypes()) {
		t.Errorf("artifactTypes = %v", types)
	}
}

func TestCuesEndpointReportsUnavailable(t *testing.T) {
	server := newTestServer(t, &fakeCues{err: context.DeadlineExceeded})
	recorder, payload := doJSON(t, server, http.MethodGet, "/api/cues", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("cues: %d %s", recorder.Code, recorder.Body)
	}
	if payload["unavailable"] == nil {
		t.Error("expected unavailable marker when cue-note is down")
	}
}

func TestCuesEndpointListsAndResolves(t *testing.T) {
	server := newTestServer(t, &fakeCues{cues: []cuenote.Cue{{ID: "cue_1", Name: "sdd-qa/analyze", Body: "classify {{report}}"}}})
	recorder, payload := doJSON(t, server, http.MethodGet, "/api/cues", "")
	if recorder.Code != http.StatusOK || len(payload["cues"].([]any)) != 1 {
		t.Fatalf("cues: %d %s", recorder.Code, recorder.Body)
	}
	recorder, cue := doJSON(t, server, http.MethodGet, "/api/cues/cue_1", "")
	if recorder.Code != http.StatusOK || cue["name"] != "sdd-qa/analyze" {
		t.Fatalf("cue: %d %s", recorder.Code, recorder.Body)
	}
}

func TestRunValidatesPromptSource(t *testing.T) {
	server := newTestServer(t, nil)
	recorder, _ := doJSON(t, server, http.MethodPost, "/api/run", `{"projectRef":"p","artifactRef":"a","selector":"ollama/x"}`)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("run without prompt or cue: %d", recorder.Code)
	}
	recorder, _ = doJSON(t, server, http.MethodPost, "/api/run", `{"projectRef":"p","artifactRef":"a","selector":"ollama/x","prompt":"hi","cueRef":"c"}`)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("run with both prompt and cue: %d", recorder.Code)
	}
}

func TestServesEmbeddedUI(t *testing.T) {
	server := newTestServer(t, nil)
	recorder, _ := doJSON(t, server, http.MethodGet, "/", "")
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "loomwork") {
		t.Fatalf("ui root: %d", recorder.Code)
	}
}
