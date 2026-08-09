package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/ilyaus/loomwork/internal/model"
)

func projectCreate(e *env, args []string) error {
	var name, description, tags string
	err := e.parse("project create", args, func(flags *flag.FlagSet) {
		flags.StringVar(&name, "name", "", "project name (required, unique)")
		flags.StringVar(&description, "description", "", "project description")
		flags.StringVar(&tags, "tags", "", "comma-separated tags")
	})
	if err != nil {
		return err
	}

	project, err := model.NewProject(name, description, splitList(tags))
	if err != nil {
		return err
	}
	if err := e.store.Create(project); err != nil {
		return err
	}
	return e.emit(project, fmt.Sprintf("created project %s (%s)", project.Name, project.ID))
}

func projectList(e *env, args []string) error {
	if err := e.parse("project list", args, nil); err != nil {
		return err
	}
	projects, err := e.store.List()
	if err != nil {
		return err
	}
	if e.asJSON {
		return e.emit(projects, "")
	}
	if len(projects) == 0 {
		return e.emit(projects, fmt.Sprintf("no projects in %s", e.paths.ProjectsDir))
	}
	var builder strings.Builder
	for _, project := range projects {
		fmt.Fprintf(&builder, "%s\t%s\t%d artifacts\n", project.ID, project.Name, len(project.Artifacts))
	}
	return e.emit(projects, strings.TrimRight(builder.String(), "\n"))
}

func projectShow(e *env, args []string) error {
	var ref string
	err := e.parse("project show", args, func(flags *flag.FlagSet) {
		flags.StringVar(&ref, "project", "", "project id or name (required)")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("project show: --project is required")
	}
	project, err := e.store.Resolve(ref)
	if err != nil {
		return err
	}
	if e.asJSON {
		return e.emit(project, "")
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "%s (%s)\n", project.Name, project.ID)
	if project.Description != "" {
		fmt.Fprintf(&builder, "description: %s\n", project.Description)
	}
	if len(project.Tags) > 0 {
		fmt.Fprintf(&builder, "tags: %s\n", strings.Join(project.Tags, ", "))
	}
	fmt.Fprintf(&builder, "created: %s\nupdated: %s\nartifacts: %d (pinned %d)\n",
		project.CreatedAt.Format("2006-01-02T15:04:05Z"),
		project.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		len(project.Artifacts), len(project.PinnedArtifacts()))
	for _, artifact := range project.LatestArtifacts() {
		fmt.Fprintf(&builder, "  %s\tv%d\t%s\t%s\n", artifact.ID, artifact.Version, artifact.Type, artifact.Name)
	}
	return e.emit(project, strings.TrimRight(builder.String(), "\n"))
}
