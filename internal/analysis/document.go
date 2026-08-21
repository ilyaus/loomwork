// Package analysis implements phase 2 of the roadmap: LLM-driven analysis of a
// project's document sources. It assesses QA readiness, lists documentation
// gaps and open questions, and extracts tester-facing requirements into the
// phase-1 requirement store. Like orchestrator, it is the only place its
// concerns combine and it is transport agnostic, so a CLI, an HTTP server, or
// a future workbench can share it unchanged.
package analysis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ilyaus/loomwork/internal/model"
)

// Verdict is the QA-readiness call an analysis makes.
type Verdict string

const (
	// VerdictReady means the service is sufficiently defined for functional
	// and integration testing.
	VerdictReady Verdict = "ready"
	// VerdictReadyWithGaps means testing can start, but the listed gaps limit
	// coverage.
	VerdictReadyWithGaps Verdict = "ready-with-gaps"
	// VerdictNotReady means the documentation cannot support meaningful test
	// work yet.
	VerdictNotReady Verdict = "not-ready"
)

// Verdicts lists every supported verdict.
func Verdicts() []Verdict {
	return []Verdict{VerdictReady, VerdictReadyWithGaps, VerdictNotReady}
}

// ParseVerdict validates a raw verdict string.
func ParseVerdict(raw string) (Verdict, error) {
	candidate := Verdict(strings.TrimSpace(strings.ToLower(raw)))
	for _, known := range Verdicts() {
		if candidate == known {
			return known, nil
		}
	}
	parts := make([]string, 0, len(Verdicts()))
	for _, verdict := range Verdicts() {
		parts = append(parts, string(verdict))
	}
	return "", fmt.Errorf("unknown verdict %q: supported verdicts are %s", raw, strings.Join(parts, ", "))
}

// ExtractedRequirement is a requirement candidate found in the sources. Its
// fields mirror the authoring fields of requirement.schema.json so each one can
// be written to the phase-1 store with origin: extracted.
type ExtractedRequirement struct {
	Text       string           `json:"text"`
	SourceType model.SourceType `json:"source_type,omitempty"`
	SourceRef  string           `json:"source_ref,omitempty"`
	Tags       []string         `json:"tags,omitempty"`
}

// Document is one document analysis. Its JSON shape is fixed by
// docs/schemas/document-analysis.schema.json, which is deliberately both the
// provider's required output format and the manual-import format.
type Document struct {
	Verdict               Verdict                `json:"verdict"`
	SpecInSync            *bool                  `json:"spec_in_sync,omitempty"`
	Summary               string                 `json:"summary,omitempty"`
	Gaps                  []string               `json:"gaps"`
	OpenQuestions         []string               `json:"open_questions"`
	ExtractedRequirements []ExtractedRequirement `json:"extracted_requirements,omitempty"`
	Sources               []string               `json:"sources,omitempty"`
	Metadata              map[string]string      `json:"metadata,omitempty"`
	CreatedAt             time.Time              `json:"created_at"`
}

// Parse decodes and validates a document analysis authored outside Loomwork.
// Unknown fields are rejected so a typo in a hand-written file surfaces as an
// error rather than silently dropping data.
func Parse(raw []byte) (*Document, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("parse document analysis: %w", err)
	}
	if err := document.normalize(); err != nil {
		return nil, fmt.Errorf("invalid document analysis: %w", err)
	}
	return &document, nil
}

// ParseModelOutput decodes an analysis from raw model text. Models wrap JSON in
// prose or code fences, so it decodes the outermost object rather than
// requiring the whole response to be JSON, and it tolerates unknown fields.
func ParseModelOutput(text string) (*Document, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("model response contains no JSON object")
	}
	var document Document
	if err := json.Unmarshal([]byte(text[start:end+1]), &document); err != nil {
		return nil, fmt.Errorf("parse model response as document analysis: %w", err)
	}
	if err := document.normalize(); err != nil {
		return nil, fmt.Errorf("model response is not a valid document analysis: %w", err)
	}
	return &document, nil
}

// normalize trims and validates the document in place, mirroring the schema's
// constraints: a known verdict, non-empty list entries, and extracted
// requirements that satisfy the requirement authoring rules.
func (d *Document) normalize() error {
	verdict, err := ParseVerdict(string(d.Verdict))
	if err != nil {
		return err
	}
	d.Verdict = verdict
	d.Summary = strings.TrimSpace(d.Summary)
	if d.Gaps, err = cleanList("gaps", d.Gaps); err != nil {
		return err
	}
	if d.OpenQuestions, err = cleanList("open_questions", d.OpenQuestions); err != nil {
		return err
	}
	if d.Sources != nil {
		if d.Sources, err = cleanList("sources", d.Sources); err != nil {
			return err
		}
	}
	for i := range d.ExtractedRequirements {
		requirement := &d.ExtractedRequirements[i]
		requirement.Text = strings.TrimSpace(requirement.Text)
		if requirement.Text == "" {
			return fmt.Errorf("extracted_requirements[%d]: text is required", i)
		}
		requirement.SourceRef = strings.TrimSpace(requirement.SourceRef)
		if requirement.SourceType != "" {
			sourceType, err := model.ParseSourceType(string(requirement.SourceType))
			if err != nil {
				return fmt.Errorf("extracted_requirements[%d]: %w", i, err)
			}
			requirement.SourceType = sourceType
		}
		if requirement.SourceRef != "" && requirement.SourceType == "" {
			return fmt.Errorf("extracted_requirements[%d]: source_ref %q needs a source_type", i, requirement.SourceRef)
		}
	}
	return nil
}

// cleanList trims entries and drops empty ones. The schema requires non-empty
// strings, but models pad lists with blanks often enough that dropping them is
// friendlier than failing the whole analysis.
func cleanList(name string, values []string) ([]string, error) {
	if values == nil {
		return nil, fmt.Errorf("%s is required (use an empty list when there are none)", name)
	}
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned, nil
}
