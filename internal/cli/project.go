package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/ilyaus/loomwork/internal/model"
)

// sourceFlagUsage documents the key=value form of --source.
const sourceFlagUsage = `document source link, "name=NAME,type=ado|confluence|github|other,url=URL[,local=PATH][,s3=URI]" (repeatable)`

// parseSource reads one --source value into a document source. Sources are
// key=value pairs so a link and its optional local/S3 copy travel together.
func parseSource(raw string) (model.DocumentSource, error) {
	source := model.DocumentSource{}
	for _, field := range strings.Split(raw, ",") {
		if strings.TrimSpace(field) == "" {
			continue
		}
		key, value, found := strings.Cut(field, "=")
		if !found {
			return model.DocumentSource{}, fmt.Errorf("source %q: field %q is not key=value", raw, field)
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "name":
			source.Name = value
		case "type":
			source.Type = model.SourceType(value)
		case "url":
			source.URL = value
		case "local":
			source.LocalPath = value
		case "s3":
			source.S3URI = value
		default:
			return model.DocumentSource{}, fmt.Errorf("source %q: unknown field %q (want name, type, url, local, s3)", raw, key)
		}
	}
	return source, nil
}

func projectCreate(e *env, args []string) error {
	var name, description, tags string
	var sources stringsFlag
	err := e.parse("project create", args, func(flags *flag.FlagSet) {
		flags.StringVar(&name, "name", "", "project name (required, unique)")
		flags.StringVar(&description, "description", "", "project description")
		flags.StringVar(&tags, "tags", "", "comma-separated tags")
		flags.Var(&sources, "source", sourceFlagUsage)
	})
	if err != nil {
		return err
	}

	project, err := model.NewProject(name, description, splitList(tags))
	if err != nil {
		return err
	}
	if err := addSources(project, sources); err != nil {
		return err
	}
	if err := e.store.Create(project); err != nil {
		return err
	}
	return e.emit(project, fmt.Sprintf("created project %s (%s) with %d document sources", project.Name, project.ID, len(project.Sources)))
}

func projectSource(e *env, args []string) error {
	var ref string
	var sources stringsFlag
	err := e.parse("project source", args, func(flags *flag.FlagSet) {
		flags.StringVar(&ref, "project", "", "project id or name (required)")
		flags.Var(&sources, "source", sourceFlagUsage)
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("project source: --project is required")
	}
	if len(sources) == 0 {
		return fmt.Errorf("project source: at least one --source is required")
	}
	project, err := e.store.Update(ref, func(project *model.Project) error {
		return addSources(project, sources)
	})
	if err != nil {
		return err
	}
	return e.emit(project.Sources, fmt.Sprintf("project %s now has %d document sources", project.Name, len(project.Sources)))
}

func addSources(project *model.Project, raw []string) error {
	for _, value := range raw {
		source, err := parseSource(value)
		if err != nil {
			return err
		}
		if _, err := project.AddSource(source); err != nil {
			return err
		}
	}
	return nil
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
	for _, source := range project.Sources {
		fmt.Fprintf(&builder, "source: %s\t%s\t%s\n", source.Name, source.Type, sourceLocation(source))
	}
	if project.Index != nil {
		fmt.Fprintf(&builder, "requirements: %d (%d active)\n", project.Index.Requirements, project.Index.ActiveRequirements)
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

func sourceLocation(source model.DocumentSource) string {
	locations := make([]string, 0, 3)
	for _, location := range []string{source.URL, source.LocalPath, source.S3URI} {
		if location != "" {
			locations = append(locations, location)
		}
	}
	return strings.Join(locations, " ")
}
