package httpapi

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/ilyaus/loomwork/internal/store"
)

// ItemVersion is one retained version of a versioned entity, newest first, so a
// viewer can offer a version switcher without knowing the family's document
// shape. The full document of any version comes from the item endpoint with
// ?version=N.
type ItemVersion struct {
	Version   int       `json:"version"`
	Status    string    `json:"status,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// itemHistory lists the retained versions of a requirement, agent definition,
// override rule, or test suite.
func (s *Server) itemHistory(w http.ResponseWriter, _ *http.Request, projectRef, family, ref string) {
	var versions []ItemVersion
	switch family {
	case familyArtifacts:
		project, err := s.store.Resolve(projectRef)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		artifact, ok := project.ResolveArtifact(ref)
		if !ok {
			writeStoreError(w, fmt.Errorf("artifact %q: %w", ref, store.ErrNotFound))
			return
		}
		for _, revision := range project.ArtifactHistory(artifact.Name) {
			versions = append(versions, ItemVersion{Version: revision.Version, Summary: revision.Name, CreatedAt: revision.CreatedAt})
		}
	case familyRequirements:
		history, err := s.store.RequirementHistory(projectRef, ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		for _, requirement := range history {
			versions = append(versions, ItemVersion{
				Version: requirement.Version, Status: string(requirement.Status),
				Summary: requirement.Text, CreatedAt: requirement.CreatedAt,
			})
		}
	case familyAgentDefinitions:
		history, err := s.store.AgentDefinitionHistory(projectRef, ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		for _, definition := range history {
			versions = append(versions, ItemVersion{
				Version: definition.Version, Summary: definition.Description, CreatedAt: definition.CreatedAt,
			})
		}
	case familyOverrideRules:
		history, err := s.store.OverrideRuleHistory(projectRef, ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		for _, rule := range history {
			versions = append(versions, ItemVersion{
				Version: rule.Version, Status: string(rule.Status), Summary: rule.Title, CreatedAt: rule.CreatedAt,
			})
		}
	case familyTestSuites:
		history, err := s.store.TestSuiteHistory(projectRef, ref)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		for _, suite := range history {
			status := "ready"
			if suite.Incomplete {
				status = "incomplete"
			}
			versions = append(versions, ItemVersion{
				Version: suite.Version, Status: status,
				Summary: fmt.Sprintf("%d case(s), %s", len(suite.CaseIDs), suite.Origin), CreatedAt: suite.CreatedAt,
			})
		}
	default:
		writeError(w, http.StatusBadRequest, fmt.Errorf("item family %q is not versioned", family))
		return
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].Version > versions[j].Version })
	if versions == nil {
		versions = []ItemVersion{}
	}
	writeJSON(w, http.StatusOK, versions)
}
