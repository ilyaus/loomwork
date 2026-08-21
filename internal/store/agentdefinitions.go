package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ilyaus/loomwork/internal/model"
)

// Agent definition layout inside a project:
//
//	agent-definitions/
//	  rest-api-test-generator.v1.md      # markdown body + frontmatter
//	  rest-api-test-generator.v2.md
//	  override-rules/empty-list-on-missing.v1.json
//	  current.json                       # current version per agent and rule
//
// Override rules are JSON rather than the markdown the vision sketches: the
// confirmed design is hybrid — a structured condition/action pair the store can
// audit deterministically plus the free-text rationale the agent reasons over —
// and only the free text would survive a markdown body. current.json carries
// both families so a project has one pointer manifest, as sketched.
const (
	overrideRulesDirName        = "override-rules"
	agentDefinitionsPointerFile = "current.json"
)

// AgentDefinitionStore persists agent definition and override rule versions.
// Every version is a discrete file; nothing is overwritten or deleted.
type AgentDefinitionStore interface {
	CreateAgentDefinition(projectRef string, spec model.AgentDefinitionSpec) (*model.AgentDefinition, error)
	// UpdateAgentDefinition writes the next version of an existing definition.
	UpdateAgentDefinition(projectRef, agentName string, spec model.AgentDefinitionSpec) (*model.AgentDefinition, error)
	// LoadAgentDefinition reads one version; version 0 means the current one.
	LoadAgentDefinition(projectRef, agentName string, version int) (*model.AgentDefinition, error)
	// ListAgentDefinitions returns the current version of every definition.
	ListAgentDefinitions(projectRef string) ([]*model.AgentDefinition, error)
	// AgentDefinitionHistory returns every retained version, oldest first.
	AgentDefinitionHistory(projectRef, agentName string) ([]*model.AgentDefinition, error)

	CreateOverrideRule(projectRef string, spec model.OverrideRuleSpec) (*model.OverrideRule, error)
	// UpdateOverrideRule writes the next version and marks the previous one
	// superseded, keeping it retrievable.
	UpdateOverrideRule(projectRef, ruleID string, spec model.OverrideRuleSpec) (*model.OverrideRule, error)
	// SetOverrideRuleStatus updates one version's status; 0 means current.
	SetOverrideRuleStatus(projectRef, ruleID string, version int, status model.OverrideRuleStatus) (*model.OverrideRule, error)
	// LoadOverrideRule reads one version; version 0 means the current one.
	LoadOverrideRule(projectRef, ruleID string, version int) (*model.OverrideRule, error)
	// ListOverrideRules returns the current version of every rule.
	ListOverrideRules(projectRef string) ([]*model.OverrideRule, error)
	// ActiveOverrideRules returns the current version of every rule whose
	// status is active: the set that shapes a new generation.
	ActiveOverrideRules(projectRef string) ([]*model.OverrideRule, error)
	// OverrideRuleHistory returns every retained version, oldest first.
	OverrideRuleHistory(projectRef, ruleID string) ([]*model.OverrideRule, error)
}

// AgentDefinitionsManifest is the agent-definitions/current.json document.
type AgentDefinitionsManifest struct {
	Agents        []AgentDefinitionPointer `json:"agents"`
	OverrideRules []OverrideRulePointer    `json:"override_rules"`
	UpdatedAt     time.Time                `json:"updated_at"`
}

// AgentDefinitionPointer names the current version of one agent definition.
type AgentDefinitionPointer struct {
	AgentName      string            `json:"agent_name"`
	CurrentVersion int               `json:"current_version"`
	Versions       []int             `json:"versions"`
	TargetProvider model.AgentTarget `json:"target_provider"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// OverrideRulePointer names the current version of one override rule.
type OverrideRulePointer struct {
	ID             string                   `json:"id"`
	CurrentVersion int                      `json:"current_version"`
	Versions       []int                    `json:"versions"`
	Status         model.OverrideRuleStatus `json:"status"`
	UpdatedAt      time.Time                `json:"updated_at"`
}

func (m AgentDefinitionsManifest) findAgent(name string) (AgentDefinitionPointer, bool) {
	for _, pointer := range m.Agents {
		if strings.EqualFold(pointer.AgentName, name) {
			return pointer, true
		}
	}
	return AgentDefinitionPointer{}, false
}

func (m *AgentDefinitionsManifest) upsertAgent(pointer AgentDefinitionPointer) {
	for position := range m.Agents {
		if strings.EqualFold(m.Agents[position].AgentName, pointer.AgentName) {
			m.Agents[position] = pointer
			return
		}
	}
	m.Agents = append(m.Agents, pointer)
	sort.Slice(m.Agents, func(a, b int) bool { return m.Agents[a].AgentName < m.Agents[b].AgentName })
}

func (m AgentDefinitionsManifest) findRule(id string) (OverrideRulePointer, bool) {
	for _, pointer := range m.OverrideRules {
		if strings.EqualFold(pointer.ID, id) {
			return pointer, true
		}
	}
	return OverrideRulePointer{}, false
}

func (m *AgentDefinitionsManifest) upsertRule(pointer OverrideRulePointer) {
	for position := range m.OverrideRules {
		if strings.EqualFold(m.OverrideRules[position].ID, pointer.ID) {
			m.OverrideRules[position] = pointer
			return
		}
	}
	m.OverrideRules = append(m.OverrideRules, pointer)
	sort.Slice(m.OverrideRules, func(a, b int) bool { return m.OverrideRules[a].ID < m.OverrideRules[b].ID })
}

// CreateAgentDefinition writes v1 of a new agent definition.
func (d *DirStore) CreateAgentDefinition(projectRef string, spec model.AgentDefinitionSpec) (*model.AgentDefinition, error) {
	var created *model.AgentDefinition
	err := d.withAgentDefinitions(projectRef, func(project *model.Project, dir string, manifest *AgentDefinitionsManifest) error {
		definition, err := model.NewAgentDefinition(spec)
		if err != nil {
			return err
		}
		if _, exists := manifest.findAgent(definition.AgentName); exists {
			return fmt.Errorf("agent definition %q already exists in project %s: update it to add a version", definition.AgentName, project.Name)
		}
		if err := writeAgentDefinition(dir, definition); err != nil {
			return err
		}
		manifest.upsertAgent(AgentDefinitionPointer{
			AgentName:      definition.AgentName,
			CurrentVersion: definition.Version,
			Versions:       []int{definition.Version},
			TargetProvider: definition.TargetProvider,
			UpdatedAt:      definition.CreatedAt,
		})
		created = definition
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// UpdateAgentDefinition writes the next version of an existing definition and
// moves the pointer, keeping every earlier version retrievable.
func (d *DirStore) UpdateAgentDefinition(projectRef, agentName string, spec model.AgentDefinitionSpec) (*model.AgentDefinition, error) {
	var updated *model.AgentDefinition
	err := d.withAgentDefinitions(projectRef, func(project *model.Project, dir string, manifest *AgentDefinitionsManifest) error {
		pointer, ok := manifest.findAgent(agentName)
		if !ok {
			return fmt.Errorf("agent definition %q in project %s: %w", agentName, project.Name, ErrNotFound)
		}
		current, err := readAgentDefinition(dir, pointer.AgentName, pointer.CurrentVersion)
		if err != nil {
			return err
		}
		next, err := current.NextVersion(spec)
		if err != nil {
			return err
		}
		if err := writeAgentDefinition(dir, next); err != nil {
			return err
		}
		pointer.CurrentVersion = next.Version
		pointer.Versions = append(pointer.Versions, next.Version)
		pointer.TargetProvider = next.TargetProvider
		pointer.UpdatedAt = next.CreatedAt
		manifest.upsertAgent(pointer)
		updated = next
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// LoadAgentDefinition reads one version (0 = current).
func (d *DirStore) LoadAgentDefinition(projectRef, agentName string, version int) (*model.AgentDefinition, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return nil, err
	}
	dir := d.agentDefinitionsDir(project.ID)
	manifest, err := readAgentDefinitionsManifest(dir)
	if err != nil {
		return nil, err
	}
	pointer, ok := manifest.findAgent(agentName)
	if !ok {
		return nil, fmt.Errorf("agent definition %q in project %s: %w", agentName, project.Name, ErrNotFound)
	}
	if version == 0 {
		version = pointer.CurrentVersion
	}
	return readAgentDefinition(dir, pointer.AgentName, version)
}

// ListAgentDefinitions returns the current version of every definition, by name.
func (d *DirStore) ListAgentDefinitions(projectRef string) ([]*model.AgentDefinition, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return nil, err
	}
	dir := d.agentDefinitionsDir(project.ID)
	manifest, err := readAgentDefinitionsManifest(dir)
	if err != nil {
		return nil, err
	}
	definitions := make([]*model.AgentDefinition, 0, len(manifest.Agents))
	for _, pointer := range manifest.Agents {
		definition, err := readAgentDefinition(dir, pointer.AgentName, pointer.CurrentVersion)
		if err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	return definitions, nil
}

// AgentDefinitionHistory returns every retained version, oldest first.
func (d *DirStore) AgentDefinitionHistory(projectRef, agentName string) ([]*model.AgentDefinition, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return nil, err
	}
	dir := d.agentDefinitionsDir(project.ID)
	manifest, err := readAgentDefinitionsManifest(dir)
	if err != nil {
		return nil, err
	}
	pointer, ok := manifest.findAgent(agentName)
	if !ok {
		return nil, fmt.Errorf("agent definition %q in project %s: %w", agentName, project.Name, ErrNotFound)
	}
	versions := append([]int(nil), pointer.Versions...)
	sort.Ints(versions)
	history := make([]*model.AgentDefinition, 0, len(versions))
	for _, version := range versions {
		definition, err := readAgentDefinition(dir, pointer.AgentName, version)
		if err != nil {
			return nil, err
		}
		history = append(history, definition)
	}
	return history, nil
}

// CreateOverrideRule writes v1 of a new override rule.
func (d *DirStore) CreateOverrideRule(projectRef string, spec model.OverrideRuleSpec) (*model.OverrideRule, error) {
	var created *model.OverrideRule
	err := d.withAgentDefinitions(projectRef, func(project *model.Project, dir string, manifest *AgentDefinitionsManifest) error {
		rule, err := model.NewOverrideRule(spec)
		if err != nil {
			return err
		}
		if _, exists := manifest.findRule(rule.ID); exists {
			return fmt.Errorf("override rule %q already exists in project %s: update it to add a version", rule.ID, project.Name)
		}
		if err := writeOverrideRule(dir, rule); err != nil {
			return err
		}
		manifest.upsertRule(OverrideRulePointer{
			ID:             rule.ID,
			CurrentVersion: rule.Version,
			Versions:       []int{rule.Version},
			Status:         rule.Status,
			UpdatedAt:      rule.CreatedAt,
		})
		created = rule
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// UpdateOverrideRule writes the next version and supersedes the previous one.
// Superseding rather than replacing matters here: a suite generated earlier cites
// the exact rule version that shaped it, so that version must stay readable.
func (d *DirStore) UpdateOverrideRule(projectRef, ruleID string, spec model.OverrideRuleSpec) (*model.OverrideRule, error) {
	var updated *model.OverrideRule
	err := d.withAgentDefinitions(projectRef, func(project *model.Project, dir string, manifest *AgentDefinitionsManifest) error {
		pointer, ok := manifest.findRule(ruleID)
		if !ok {
			return fmt.Errorf("override rule %q in project %s: %w", ruleID, project.Name, ErrNotFound)
		}
		current, err := readOverrideRule(dir, pointer.ID, pointer.CurrentVersion)
		if err != nil {
			return err
		}
		next, err := current.NextVersion(spec)
		if err != nil {
			return err
		}
		if err := writeOverrideRule(dir, next); err != nil {
			return err
		}
		current.Status = model.OverrideRuleStatusSuperseded
		if err := writeOverrideRule(dir, current); err != nil {
			return err
		}
		pointer.CurrentVersion = next.Version
		pointer.Versions = append(pointer.Versions, next.Version)
		pointer.Status = next.Status
		pointer.UpdatedAt = next.CreatedAt
		manifest.upsertRule(pointer)
		updated = next
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// SetOverrideRuleStatus updates the status of one version (0 = current).
func (d *DirStore) SetOverrideRuleStatus(projectRef, ruleID string, version int, status model.OverrideRuleStatus) (*model.OverrideRule, error) {
	var changed *model.OverrideRule
	err := d.withAgentDefinitions(projectRef, func(project *model.Project, dir string, manifest *AgentDefinitionsManifest) error {
		pointer, ok := manifest.findRule(ruleID)
		if !ok {
			return fmt.Errorf("override rule %q in project %s: %w", ruleID, project.Name, ErrNotFound)
		}
		target := version
		if target == 0 {
			target = pointer.CurrentVersion
		}
		rule, err := readOverrideRule(dir, pointer.ID, target)
		if err != nil {
			return err
		}
		if err := rule.SetStatus(status); err != nil {
			return err
		}
		if err := writeOverrideRule(dir, rule); err != nil {
			return err
		}
		if rule.Version == pointer.CurrentVersion {
			pointer.Status = rule.Status
			pointer.UpdatedAt = time.Now().UTC()
			manifest.upsertRule(pointer)
		}
		changed = rule
		return nil
	})
	if err != nil {
		return nil, err
	}
	return changed, nil
}

// LoadOverrideRule reads one version (0 = current).
func (d *DirStore) LoadOverrideRule(projectRef, ruleID string, version int) (*model.OverrideRule, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return nil, err
	}
	dir := d.agentDefinitionsDir(project.ID)
	manifest, err := readAgentDefinitionsManifest(dir)
	if err != nil {
		return nil, err
	}
	pointer, ok := manifest.findRule(ruleID)
	if !ok {
		return nil, fmt.Errorf("override rule %q in project %s: %w", ruleID, project.Name, ErrNotFound)
	}
	if version == 0 {
		version = pointer.CurrentVersion
	}
	return readOverrideRule(dir, pointer.ID, version)
}

// ListOverrideRules returns the current version of every rule, by id.
func (d *DirStore) ListOverrideRules(projectRef string) ([]*model.OverrideRule, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return nil, err
	}
	dir := d.agentDefinitionsDir(project.ID)
	manifest, err := readAgentDefinitionsManifest(dir)
	if err != nil {
		return nil, err
	}
	rules := make([]*model.OverrideRule, 0, len(manifest.OverrideRules))
	for _, pointer := range manifest.OverrideRules {
		rule, err := readOverrideRule(dir, pointer.ID, pointer.CurrentVersion)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// ActiveOverrideRules returns the current version of every active rule.
func (d *DirStore) ActiveOverrideRules(projectRef string) ([]*model.OverrideRule, error) {
	rules, err := d.ListOverrideRules(projectRef)
	if err != nil {
		return nil, err
	}
	active := make([]*model.OverrideRule, 0, len(rules))
	for _, rule := range rules {
		if rule.Status == model.OverrideRuleStatusActive {
			active = append(active, rule)
		}
	}
	return active, nil
}

// OverrideRuleHistory returns every retained version, oldest first.
func (d *DirStore) OverrideRuleHistory(projectRef, ruleID string) ([]*model.OverrideRule, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return nil, err
	}
	dir := d.agentDefinitionsDir(project.ID)
	manifest, err := readAgentDefinitionsManifest(dir)
	if err != nil {
		return nil, err
	}
	pointer, ok := manifest.findRule(ruleID)
	if !ok {
		return nil, fmt.Errorf("override rule %q in project %s: %w", ruleID, project.Name, ErrNotFound)
	}
	versions := append([]int(nil), pointer.Versions...)
	sort.Ints(versions)
	history := make([]*model.OverrideRule, 0, len(versions))
	for _, version := range versions {
		rule, err := readOverrideRule(dir, pointer.ID, version)
		if err != nil {
			return nil, err
		}
		history = append(history, rule)
	}
	return history, nil
}

// AgentDefinitionsManifest returns the current-version pointers for a project.
func (d *DirStore) AgentDefinitionsManifest(projectRef string) (AgentDefinitionsManifest, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return AgentDefinitionsManifest{}, err
	}
	return readAgentDefinitionsManifest(d.agentDefinitionsDir(project.ID))
}

// withAgentDefinitions runs mutate against a project's agent-definitions folder
// under the store lock, then persists the pointer manifest, so the read and the
// write form one atomic cycle even across processes.
func (d *DirStore) withAgentDefinitions(projectRef string, mutate func(project *model.Project, dir string, manifest *AgentDefinitionsManifest) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	release, err := lockDir(d.dir)
	if err != nil {
		return err
	}
	defer release()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return err
	}
	if err := d.ensureLayoutLocked(project.ID); err != nil {
		return err
	}
	dir := d.agentDefinitionsDir(project.ID)
	if err := os.MkdirAll(filepath.Join(dir, overrideRulesDirName), 0o755); err != nil {
		return fmt.Errorf("create override rules directory: %w", err)
	}
	manifest, err := readAgentDefinitionsManifest(dir)
	if err != nil {
		return err
	}
	if err := mutate(project, dir, &manifest); err != nil {
		return err
	}
	manifest.UpdatedAt = time.Now().UTC()
	if err := writeAgentDefinitionsManifest(dir, manifest); err != nil {
		return err
	}
	project.UpdatedAt = time.Now().UTC()
	return d.writeLocked(project)
}

func (d *DirStore) agentDefinitionsDir(projectID string) string {
	return filepath.Join(d.ProjectDir(projectID), AgentDefinitionsDirName)
}

func agentDefinitionPath(dir, name string, version int) string {
	return filepath.Join(dir, fmt.Sprintf("%s.v%d.md", name, version))
}

func readAgentDefinition(dir, name string, version int) (*model.AgentDefinition, error) {
	path := agentDefinitionPath(dir, name, version)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("agent definition %s v%d: %w", name, version, ErrNotFound)
		}
		return nil, fmt.Errorf("read agent definition %s: %w", path, err)
	}
	definition, err := model.ParseAgentDefinitionMarkdown(raw)
	if err != nil {
		return nil, fmt.Errorf("parse agent definition %s: %w", path, err)
	}
	return definition, nil
}

func writeAgentDefinition(dir string, definition *model.AgentDefinition) error {
	return writeFileAtomic(
		agentDefinitionPath(dir, definition.AgentName, definition.Version),
		[]byte(definition.Markdown()),
	)
}

func overrideRulePath(dir, id string, version int) string {
	return filepath.Join(dir, overrideRulesDirName, fmt.Sprintf("%s.v%d.json", id, version))
}

func readOverrideRule(dir, id string, version int) (*model.OverrideRule, error) {
	path := overrideRulePath(dir, id, version)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("override rule %s v%d: %w", id, version, ErrNotFound)
		}
		return nil, fmt.Errorf("read override rule %s: %w", path, err)
	}
	var rule model.OverrideRule
	if err := json.Unmarshal(raw, &rule); err != nil {
		return nil, fmt.Errorf("parse override rule %s: %w", path, err)
	}
	return &rule, nil
}

func writeOverrideRule(dir string, rule *model.OverrideRule) error {
	payload, err := json.MarshalIndent(rule, "", "  ")
	if err != nil {
		return fmt.Errorf("encode override rule %s v%d: %w", rule.ID, rule.Version, err)
	}
	return writeFileAtomic(overrideRulePath(dir, rule.ID, rule.Version), payload)
}

func readAgentDefinitionsManifest(dir string) (AgentDefinitionsManifest, error) {
	path := filepath.Join(dir, agentDefinitionsPointerFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AgentDefinitionsManifest{
				Agents:        []AgentDefinitionPointer{},
				OverrideRules: []OverrideRulePointer{},
			}, nil
		}
		return AgentDefinitionsManifest{}, fmt.Errorf("read agent definitions manifest %s: %w", path, err)
	}
	var manifest AgentDefinitionsManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return AgentDefinitionsManifest{}, fmt.Errorf("parse agent definitions manifest %s: %w", path, err)
	}
	return manifest, nil
}

func writeAgentDefinitionsManifest(dir string, manifest AgentDefinitionsManifest) error {
	if manifest.Agents == nil {
		manifest.Agents = []AgentDefinitionPointer{}
	}
	if manifest.OverrideRules == nil {
		manifest.OverrideRules = []OverrideRulePointer{}
	}
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode agent definitions manifest: %w", err)
	}
	return writeFileAtomic(filepath.Join(dir, agentDefinitionsPointerFile), payload)
}
