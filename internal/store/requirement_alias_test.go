package store

import (
	"bytes"
	"os"
	"testing"

	"github.com/ilyaus/loomwork/internal/model"
)

func TestLegacyRequirementReferencesDoNotRewriteHistory(t *testing.T) {
	d, project := seededStore(t)
	r, err := d.CreateRequirement(project.ID, model.RequirementSpec{Text: "Original", Origin: model.RequirementOriginImported, Metadata: map[string]string{"import_format": "spec-kit", "feature": "008-example", "source_id": "FR-012"}})
	if err != nil {
		t.Fatal(err)
	}
	file := requirementPath(d.requirementsDir(project.ID), r.ID, 1)
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{r.ID, "008-FR-012"} {
		loaded, err := d.LoadRequirement(project.ID, ref, 0)
		if err != nil || loaded.ID != r.ID || loaded.DisplayID != "008-FR-012" {
			t.Fatalf("alias %s = %+v, %v", ref, loaded, err)
		}
		history, err := d.RequirementHistory(project.ID, ref)
		if err != nil || len(history) != 1 || history[0].Text != "Original" {
			t.Fatalf("history = %+v, %v", history, err)
		}
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("reading an alias changed the stored requirement")
	}
}
