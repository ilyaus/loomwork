package projectimport

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ilyaus/loomwork/internal/model"
)

const maxDocumentBytes = 6 << 20
const maxSnapshotBytes = 32 << 20
const maxEntries = 2000

type SpecKit struct{}

func (SpecKit) Read(source string, selected []string) (*Snapshot, error) {
	if !filepath.IsAbs(source) {
		return nil, fmt.Errorf("project path must be an absolute local directory")
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return nil, fmt.Errorf("open source project: %w", err)
	}
	defer root.Close()
	if info, err := root.Stat(".specify"); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("not a spec-kit project: .specify directory is missing")
	}
	entries, err := readDirectory(root, "specs")
	if err != nil {
		return nil, fmt.Errorf("read spec-kit features: %w", err)
	}
	wanted := map[string]bool{}
	for _, id := range selected {
		wanted[id] = true
	}
	snapshot := &Snapshot{Preview: Preview{Name: filepath.Base(filepath.Clean(source)), SourcePath: filepath.Clean(source), Features: []Feature{}, Warnings: []string{}}}
	total, visited := 0, 0
	add := func(name, feature string, raw []byte) (bool, error) {
		if raw == nil {
			var err error
			raw, err = readDocument(root, name)
			if err != nil {
				return false, err
			}
		}
		total += len(raw)
		if total > maxSnapshotBytes {
			return false, fmt.Errorf("project documentation exceeds the 32 MB snapshot limit")
		}
		if len(strings.TrimSpace(string(raw))) == 0 {
			snapshot.Preview.Warnings = append(snapshot.Preview.Warnings, "Skipped empty document: "+name)
			return false, nil
		}
		testDocument := strings.Contains(name, "/scenarios/") || strings.Contains(name, "/generated-scenarios/")
		kind := "document"
		if testDocument {
			kind = "test-scenario"
		}
		tags := []string{"spec-kit", feature}
		if testDocument {
			tags = append(tags, "test-scenario")
		}
		media := map[string]string{".md": "text/markdown", ".json": "application/json", ".yaml": "application/yaml", ".yml": "application/yaml"}[strings.ToLower(path.Ext(name))]
		artifactType := model.ArtifactTypeDoc
		if path.Base(name) == "spec.md" || strings.Contains(name, "/contracts/") || testDocument {
			artifactType = model.ArtifactTypeSpec
		}
		snapshot.Artifacts = append(snapshot.Artifacts, model.ArtifactSpec{
			Name: name, Type: artifactType, Body: model.Body{Content: string(raw), MediaType: media}, Tags: tags,
			Metadata: map[string]string{"import_format": "spec-kit", "source_path": name, "feature": feature, "document_kind": kind},
		})
		return testDocument, nil
	}
	for _, entry := range entries {
		if len(selected) > 0 && !wanted[entry.Name()] {
			continue
		}
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		id := entry.Name()
		specPath := path.Join("specs", id, "spec.md")
		if _, err := root.Lstat(specPath); os.IsNotExist(err) {
			continue
		}
		raw, err := readDocument(root, specPath)
		if err != nil {
			return nil, err
		}
		requirements, err := parseRequirements(string(raw), id, specPath)
		if err != nil {
			return nil, err
		}
		feature := Feature{ID: id, Title: featureTitle(string(raw), id), Requirements: len(requirements)}
		if len(requirements) == 0 {
			snapshot.Preview.Warnings = append(snapshot.Preview.Warnings, "No numbered FR, NFR, or SC entries found in "+specPath)
		}
		snapshot.Requirements = append(snapshot.Requirements, requirements...)
		var walk func(string, int) error
		walk = func(dir string, depth int) error {
			if depth > 16 {
				return fmt.Errorf("documentation nesting exceeds 16 levels")
			}
			children, err := readDirectory(root, dir)
			if err != nil {
				return err
			}
			for _, child := range children {
				visited++
				if visited > maxEntries {
					return fmt.Errorf("project documentation exceeds %d entries", maxEntries)
				}
				name := path.Join(dir, child.Name())
				if child.Type()&os.ModeSymlink != 0 {
					snapshot.Preview.Warnings = append(snapshot.Preview.Warnings, "Skipped symbolic link: "+name)
					continue
				}
				if strings.HasPrefix(child.Name(), ".") {
					continue
				}
				if child.IsDir() {
					if depth == 0 && child.Name() != "contracts" && child.Name() != "checklists" && child.Name() != "sdd-qa" {
						continue
					}
					if err := walk(name, depth+1); err != nil {
						return err
					}
					continue
				}
				if !child.Type().IsRegular() {
					continue
				}
				ext := strings.ToLower(path.Ext(name))
				if ext != ".md" && ext != ".yaml" && ext != ".yml" && ext != ".json" {
					continue
				}
				before := len(snapshot.Artifacts)
				var content []byte
				if name == specPath {
					content = raw
				}
				isTest, err := add(name, id, content)
				if err != nil {
					return err
				}
				feature.Artifacts += len(snapshot.Artifacts) - before
				if isTest {
					feature.TestDocuments++
				}
			}
			return nil
		}
		if err := walk(path.Join("specs", id), 0); err != nil {
			return nil, err
		}
		snapshot.Preview.Features = append(snapshot.Preview.Features, feature)
		snapshot.Preview.TestDocuments += feature.TestDocuments
		delete(wanted, id)
	}
	if len(wanted) > 0 {
		return nil, fmt.Errorf("selected features were not found in the spec-kit project")
	}
	if len(snapshot.Preview.Features) == 0 {
		return nil, fmt.Errorf("no spec-kit features with spec.md found under specs/")
	}
	constitution := ".specify/memory/constitution.md"
	if _, err := root.Lstat(constitution); err == nil {
		if _, err := add(constitution, "constitution", nil); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	snapshot.Preview.Requirements = len(snapshot.Requirements)
	snapshot.Preview.Artifacts = len(snapshot.Artifacts)
	return snapshot, nil
}

func readDirectory(root *os.Root, name string) ([]os.DirEntry, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries, err := file.ReadDir(maxEntries + 1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > maxEntries {
		return nil, fmt.Errorf("directory %q exceeds %d entries", name, maxEntries)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

func readDocument(root *os.Root, name string) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("document %q must be a regular file, not a symbolic link", name)
	}
	file, err := root.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open document %q: %w", name, err)
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxDocumentBytes {
		return nil, fmt.Errorf("document %q must be a regular file up to 6 MB", name)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxDocumentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read document %q: %w", name, err)
	}
	if len(raw) > maxDocumentBytes || !utf8.Valid(raw) || strings.ContainsRune(string(raw), 0) {
		return nil, fmt.Errorf("document %q must be UTF-8 text up to 6 MB", name)
	}
	return raw, nil
}

var requirementLine = regexp.MustCompile(`^\s*[-*+]\s+(?:\*\*|__)?((?:FR|NFR|SC)-[0-9]+[A-Za-z]?)(?:\*\*|__)?\s*:\s*(.*)$`)

func parseRequirements(raw, feature, source string) ([]model.RequirementSpec, error) {
	requirements := []model.RequirementSpec{}
	seen := map[string]bool{}
	current := -1
	fence := ""
	for number, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := trimmed[:3]
			if fence == "" {
				fence = marker
				current = -1
			} else if fence == marker {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		if match := requirementLine.FindStringSubmatch(line); match != nil {
			id := match[1]
			if seen[id] {
				return nil, fmt.Errorf("duplicate requirement %s in %s", id, source)
			}
			seen[id] = true
			canonical, err := model.SpecRequirementID(feature, id)
			if err != nil {
				return nil, err
			}
			requirements = append(requirements, model.RequirementSpec{
				ID:   canonical,
				Text: match[2], SourceType: model.SourceTypeOther, SourceRef: source + "#" + id, Origin: model.RequirementOriginImported,
				Tags:     []string{feature, strings.ToLower(strings.SplitN(id, "-", 2)[0])},
				Metadata: map[string]string{"import_format": "spec-kit", "source_id": id, "source_path": source, "source_line": strconv.Itoa(number + 1), "feature": feature},
			})
			current = len(requirements) - 1
			continue
		}
		if current >= 0 && trimmed != "" {
			if strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t") {
				requirements[current].Text += "\n" + line
			} else {
				current = -1
			}
		}
	}
	for _, requirement := range requirements {
		if strings.TrimSpace(requirement.Text) == "" {
			return nil, fmt.Errorf("empty requirement %s in %s", requirement.Metadata["source_id"], source)
		}
	}
	return requirements, nil
}

func featureTitle(raw, fallback string) string {
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "# "), "Feature Specification:"))
		}
	}
	return fallback
}
