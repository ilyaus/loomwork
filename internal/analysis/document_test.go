package analysis

import (
	"strings"
	"testing"
)

func TestParseValidation(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			name: "minimal valid document",
			raw:  `{"verdict":"ready","gaps":[],"open_questions":[]}`,
		},
		{
			name: "full valid document",
			raw: `{"verdict":"ready-with-gaps","spec_in_sync":false,"summary":"mostly there",
				"gaps":["no error catalog"],"open_questions":["is retry idempotent?"],
				"extracted_requirements":[{"text":"POST /orders returns 201","source_type":"github","source_ref":"https://github.com/x/y/blob/main/api.md","tags":["api"]}],
				"sources":["api spec"],"metadata":{"author":"qa"},"created_at":"2026-08-21T00:00:00Z"}`,
		},
		{name: "not json", raw: `{nope`, wantErr: "parse document analysis"},
		{name: "unknown field", raw: `{"verdict":"ready","gaps":[],"open_questions":[],"extra":1}`, wantErr: `unknown field "extra"`},
		{name: "unknown verdict", raw: `{"verdict":"maybe","gaps":[],"open_questions":[]}`, wantErr: `unknown verdict "maybe"`},
		{name: "missing verdict", raw: `{"gaps":[],"open_questions":[]}`, wantErr: "unknown verdict"},
		{name: "missing gaps", raw: `{"verdict":"ready","open_questions":[]}`, wantErr: "gaps is required"},
		{name: "missing open questions", raw: `{"verdict":"ready","gaps":[]}`, wantErr: "open_questions is required"},
		{
			name:    "requirement without text",
			raw:     `{"verdict":"ready","gaps":[],"open_questions":[],"extracted_requirements":[{"text":"  "}]}`,
			wantErr: "extracted_requirements[0]: text is required",
		},
		{
			name:    "requirement with bad source type",
			raw:     `{"verdict":"ready","gaps":[],"open_questions":[],"extracted_requirements":[{"text":"t","source_type":"jira"}]}`,
			wantErr: `unknown source type "jira"`,
		},
		{
			name:    "requirement source ref without type",
			raw:     `{"verdict":"ready","gaps":[],"open_questions":[],"extracted_requirements":[{"text":"t","source_ref":"story-1"}]}`,
			wantErr: "needs a source_type",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, err := Parse([]byte(test.raw))
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("Parse: %v", err)
				}
				if document.Verdict == "" {
					t.Errorf("document = %+v, want a verdict", document)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, test.wantErr)
			}
		})
	}
}

func TestParseModelOutput(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantErr string
	}{
		{
			name: "bare json",
			text: `{"verdict":"ready","gaps":[],"open_questions":[]}`,
		},
		{
			name: "json wrapped in prose and a code fence",
			text: "Here is the analysis:\n```json\n{\"verdict\":\"not-ready\",\"gaps\":[\"no spec\"],\"open_questions\":[]}\n```\nLet me know if you need more.",
		},
		{
			name: "unknown fields are tolerated",
			text: `{"verdict":"ready","gaps":[],"open_questions":[],"confidence":0.9}`,
		},
		{name: "no json object", text: "I could not analyze the documents.", wantErr: "contains no JSON object"},
		{name: "invalid json object", text: `{"verdict": ready}`, wantErr: "parse model response"},
		{name: "invalid analysis", text: `{"verdict":"ready","gaps":[]}`, wantErr: "open_questions is required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			document, err := ParseModelOutput(test.text)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("ParseModelOutput: %v", err)
				}
				if document.Verdict == "" {
					t.Errorf("document = %+v, want a verdict", document)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, test.wantErr)
			}
		})
	}
}

func TestNormalizeCleansLists(t *testing.T) {
	document, err := Parse([]byte(`{"verdict":" READY ","gaps":[" a ","","b"],"open_questions":["  "],"sources":[" spec "]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if document.Verdict != VerdictReady {
		t.Errorf("verdict = %q, want it normalized to %q", document.Verdict, VerdictReady)
	}
	if strings.Join(document.Gaps, "|") != "a|b" {
		t.Errorf("gaps = %v, want trimmed non-empty entries", document.Gaps)
	}
	if len(document.OpenQuestions) != 0 {
		t.Errorf("open questions = %v, want blank entries dropped", document.OpenQuestions)
	}
	if strings.Join(document.Sources, "|") != "spec" {
		t.Errorf("sources = %v, want trimmed entries", document.Sources)
	}
}
