package model

import (
	"fmt"
	"regexp"
	"strings"
)

var localRequirementID = regexp.MustCompile(`^req-[0-9]{3,}$`)
var specRequirementID = regexp.MustCompile(`^[0-9]{3,}-(FR|NFR|SC)-[0-9]+[A-Z]?$`)

func NormalizeRequirementID(raw string) (string, error) {
	id := strings.TrimSpace(raw)
	if localRequirementID.MatchString(strings.ToLower(id)) {
		return strings.ToLower(id), nil
	}
	if specRequirementID.MatchString(strings.ToUpper(id)) {
		return strings.ToUpper(id), nil
	}
	return "", fmt.Errorf("requirement id %q must look like req-001 or 008-FR-001", raw)
}

func SpecRequirementID(feature, sourceID string) (string, error) {
	prefix, _, _ := strings.Cut(feature, "-")
	id := prefix + "-" + strings.ToUpper(sourceID)
	if !specRequirementID.MatchString(id) {
		return "", fmt.Errorf("cannot derive a spec requirement id from feature %q and requirement %q", feature, sourceID)
	}
	return id, nil
}

func (r Requirement) ReferenceID() string {
	if r.Metadata["import_format"] == "spec-kit" {
		if id, err := SpecRequirementID(r.Metadata["feature"], r.Metadata["source_id"]); err == nil {
			return id
		}
	}
	return r.ID
}
