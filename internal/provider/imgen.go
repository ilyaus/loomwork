package provider

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ImGen calls the local ilyaus/im-gen service, which executes image generation
// jobs asynchronously: submit, poll, then collect artifacts.
type ImGen struct {
	baseURL      string
	defaultModel string
	pollInterval time.Duration
	client       *http.Client
}

// NewImGen builds the im-gen adapter.
func NewImGen(cfg Config) *ImGen {
	interval := DefaultPollInterval
	if cfg.ImGen.PollIntervalSeconds > 0 {
		interval = time.Duration(cfg.ImGen.PollIntervalSeconds) * time.Second
	}
	return &ImGen{
		baseURL:      firstNonEmpty(cfg.BaseURL, DefaultImGenBaseURL),
		defaultModel: cfg.DefaultModel,
		pollInterval: interval,
		client:       newHTTPClient(cfg.Timeout()),
	}
}

// Name identifies the adapter.
func (i *ImGen) Name() string { return string(KindImGen) }

// im-gen job statuses.
const (
	imGenStatusQueued    = "queued"
	imGenStatusRunning   = "running"
	imGenStatusSucceeded = "succeeded"
	imGenStatusFailed    = "failed"
)

type imGenSubmission struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

type imGenArtifact struct {
	Filename    string `json:"filename"`
	Path        string `json:"path"`
	MediaType   string `json:"media_type"`
	SizeBytes   int64  `json:"size_bytes"`
	DownloadURL string `json:"download_url"`
	Seed        *int   `json:"seed"`
}

type imGenResult struct {
	Model     string          `json:"model"`
	OutputDir string          `json:"output_dir"`
	Artifacts []imGenArtifact `json:"artifacts"`
}

type imGenJobStatus struct {
	JobID  string       `json:"job_id"`
	Status string       `json:"status"`
	Error  string       `json:"error"`
	Result *imGenResult `json:"result"`
}

type imGenModel struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// GenerateImages submits a job and polls until it succeeds, fails, or ctx ends.
func (i *ImGen) GenerateImages(ctx context.Context, req ImageRequest) (ImageResult, error) {
	req.Model = firstNonEmpty(req.Model, i.defaultModel)
	if err := req.Validate(); err != nil {
		return ImageResult{}, fmt.Errorf("imgen: %w", err)
	}

	var submission imGenSubmission
	submitURL := joinURL(i.baseURL, "/jobs")
	if err := postJSON(ctx, i.client, submitURL, nil, imGenBody(req), &submission); err != nil {
		return ImageResult{}, fmt.Errorf("imgen submit (model %s): %w", req.Model, err)
	}
	if strings.TrimSpace(submission.JobID) == "" {
		return ImageResult{}, fmt.Errorf("imgen submit (model %s): service returned no job id", req.Model)
	}

	statusURL := joinURL(i.baseURL, "/jobs/"+submission.JobID)
	for {
		var status imGenJobStatus
		if err := getJSON(ctx, i.client, statusURL, nil, &status); err != nil {
			return ImageResult{}, fmt.Errorf("imgen poll (job %s): %w", submission.JobID, err)
		}
		switch status.Status {
		case imGenStatusSucceeded:
			if status.Result == nil {
				return ImageResult{}, fmt.Errorf("imgen poll (job %s): job succeeded without a result payload", submission.JobID)
			}
			return toImageResult(submission.JobID, req.Model, *status.Result), nil
		case imGenStatusFailed:
			return ImageResult{}, fmt.Errorf("imgen job %s failed: %s", submission.JobID, firstNonEmpty(status.Error, "no error reported"))
		case imGenStatusQueued, imGenStatusRunning, "":
			// keep polling
		default:
			return ImageResult{}, fmt.Errorf("imgen poll (job %s): unknown status %q", submission.JobID, status.Status)
		}

		select {
		case <-ctx.Done():
			return ImageResult{}, fmt.Errorf("imgen poll (job %s): %w", submission.JobID, ctx.Err())
		case <-time.After(i.pollInterval):
		}
	}
}

// Models lists the models the service supports.
func (i *ImGen) Models(ctx context.Context) ([]Model, error) {
	var decoded []imGenModel
	url := joinURL(i.baseURL, "/models")
	if err := getJSON(ctx, i.client, url, nil, &decoded); err != nil {
		return nil, fmt.Errorf("imgen models: %w", err)
	}
	models := make([]Model, 0, len(decoded))
	for _, entry := range decoded {
		models = append(models, Model{ID: entry.Name, Description: entry.Description})
	}
	return models, nil
}

func toImageResult(jobID, requestedModel string, result imGenResult) ImageResult {
	artifacts := make([]ImageArtifact, 0, len(result.Artifacts))
	for _, entry := range result.Artifacts {
		artifacts = append(artifacts, ImageArtifact{
			Filename:    entry.Filename,
			Path:        entry.Path,
			DownloadURL: entry.DownloadURL,
			MediaType:   firstNonEmpty(entry.MediaType, "image/png"),
			SizeBytes:   entry.SizeBytes,
			Seed:        entry.Seed,
		})
	}
	return ImageResult{
		JobID:     jobID,
		Model:     firstNonEmpty(result.Model, requestedModel),
		Artifacts: artifacts,
	}
}

// imGenBody maps the normalized image request onto im-gen's GenerationRequest.
func imGenBody(req ImageRequest) map[string]any {
	body := map[string]any{
		"model":  req.Model,
		"prompt": req.Prompt,
	}
	if req.NegativePrompt != "" {
		body["neg_prompt"] = req.NegativePrompt
	}
	if req.Width > 0 {
		body["width"] = req.Width
	}
	if req.Height > 0 {
		body["height"] = req.Height
	}
	if req.Count > 0 {
		body["num_images_per_prompt"] = req.Count
	}
	if req.Steps > 0 {
		body["inf_steps"] = req.Steps
	}
	if req.GuidanceScale != nil {
		body["guidance_scale"] = *req.GuidanceScale
	}
	if req.Seed != nil {
		body["seed"] = *req.Seed
	}
	for key, value := range req.Extra {
		body[key] = value
	}
	return body
}
