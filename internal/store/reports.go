package store

import (
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ReportFile describes one file in a project's reports folder.
type ReportFile struct {
	Name      string    `json:"name"`
	MediaType string    `json:"mediaType"`
	Size      int64     `json:"size"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ListReports returns regular files inside reports/, ordered by relative path.
func (d *DirStore) ListReports(projectRef string) ([]ReportFile, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return nil, err
	}
	root := filepath.Join(d.ProjectDir(project.ID), ReportsDirName)
	reports := []ReportFile{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) && path == root {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		reports = append(reports, ReportFile{
			Name:      filepath.ToSlash(name),
			MediaType: reportMediaType(name),
			Size:      info.Size(),
			UpdatedAt: info.ModTime().UTC(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk reports directory %s: %w", root, err)
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].Name < reports[j].Name })
	return reports, nil
}

// LoadReport reads one report by its slash-separated relative path.
func (d *DirStore) LoadReport(projectRef, name string) (ReportFile, []byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	project, err := d.resolveLocked(projectRef)
	if err != nil {
		return ReportFile{}, nil, err
	}
	clean := filepath.FromSlash(strings.TrimSpace(name))
	if !filepath.IsLocal(clean) {
		return ReportFile{}, nil, fmt.Errorf("report name %q must stay inside the reports directory", name)
	}
	root := filepath.Join(d.ProjectDir(project.ID), ReportsDirName)
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return ReportFile{}, nil, fmt.Errorf("resolve reports directory %s: %w", root, err)
	}
	path := filepath.Join(root, clean)
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ReportFile{}, nil, fmt.Errorf("report %q in project %s: %w", name, project.Name, ErrNotFound)
		}
		return ReportFile{}, nil, fmt.Errorf("resolve report %s: %w", path, err)
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || !filepath.IsLocal(relative) {
		return ReportFile{}, nil, fmt.Errorf("report name %q resolves outside the reports directory", name)
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return ReportFile{}, nil, fmt.Errorf("read report information %s: %w", resolvedPath, err)
	}
	if !info.Mode().IsRegular() {
		return ReportFile{}, nil, fmt.Errorf("report %q is not a regular file", name)
	}
	raw, err := os.ReadFile(resolvedPath)
	if err != nil {
		return ReportFile{}, nil, fmt.Errorf("read report %s: %w", resolvedPath, err)
	}
	return ReportFile{
		Name:      filepath.ToSlash(clean),
		MediaType: reportMediaType(clean),
		Size:      info.Size(),
		UpdatedAt: info.ModTime().UTC(),
	}, raw, nil
}

func reportMediaType(name string) string {
	if detected := mime.TypeByExtension(filepath.Ext(name)); detected != "" {
		return detected
	}
	return "text/plain"
}
