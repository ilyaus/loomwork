package model

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

type TestDocumentConfig struct {
	Roots []string `json:"roots"`
}

type DocumentSuite struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Root      string     `json:"root"`
	Documents []Artifact `json:"documents"`
}

func NormalizeTestDocumentRoots(roots []string) ([]string, error) {
	if len(roots) > 1000 {
		return nil, fmt.Errorf("at most 1000 test folder roots are supported")
	}
	result := []string{}
	seen := map[string]bool{}
	for _, root := range roots {
		root = strings.TrimSuffix(strings.TrimSpace(root), "/")
		if !fs.ValidPath(root) || root == "." || strings.ContainsAny(root, "\\*?[]") {
			return nil, fmt.Errorf("test folder %q must be a relative folder path without wildcards", root)
		}
		if !seen[root] {
			result = append(result, root)
			seen[root] = true
		}
	}
	sort.Strings(result)
	for i, root := range result {
		for _, parent := range result[:i] {
			if strings.HasPrefix(root, parent+"/") {
				return nil, fmt.Errorf("test folders %q and %q overlap", parent, root)
			}
		}
	}
	return result, nil
}

func (p *Project) DefaultTestDocumentRoots() []string {
	roots := []string{}
	if p.Import != nil && p.Import.Format == "spec-kit" {
		for _, feature := range p.Import.Features {
			roots = append(roots, "specs/"+feature+"/sdd-qa")
		}
	}
	return roots
}

func (p *Project) TestDocumentRoots() []string {
	if p.TestDocuments != nil {
		return append([]string{}, p.TestDocuments.Roots...)
	}
	return p.DefaultTestDocumentRoots()
}

func (p *Project) DocumentSuites() []DocumentSuite {
	suites := []DocumentSuite{}
	artifacts := p.LatestArtifacts()
	for _, root := range p.TestDocumentRoots() {
		suite := DocumentSuite{ID: root, Name: strings.TrimPrefix(root, "specs/"), Root: root, Documents: []Artifact{}}
		for _, artifact := range artifacts {
			if strings.HasPrefix(artifact.Name, root+"/") {
				suite.Documents = append(suite.Documents, artifact)
			}
		}
		if len(suite.Documents) > 0 {
			suites = append(suites, suite)
		}
	}
	return suites
}
