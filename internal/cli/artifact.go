package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/orchestrator"
)

func artifactAdd(e *env, args []string) error {
	var projectRef, name, rawType, content, file, ref, tags, mediaType string
	var pin bool
	err := e.parse("artifact add", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&name, "name", "", "artifact name (required)")
		flags.StringVar(&rawType, "type", string(model.ArtifactTypeDoc), "artifact type: spec, log, test-result, diagram, doc, generated")
		flags.StringVar(&content, "content", "", "inline artifact content")
		flags.StringVar(&file, "file", "", "read inline content from this file")
		flags.StringVar(&ref, "ref", "", "store a reference to this path instead of inline content")
		flags.StringVar(&mediaType, "media-type", "", "artifact media type, e.g. text/markdown")
		flags.StringVar(&tags, "tags", "", "comma-separated tags")
		flags.BoolVar(&pin, "pin", false, "pin the artifact as standing prompt context")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" {
		return fmt.Errorf("artifact add: --project is required")
	}

	artifactType, err := model.ParseArtifactType(rawType)
	if err != nil {
		return err
	}
	body, err := buildBody(content, file, ref, mediaType)
	if err != nil {
		return err
	}

	project, err := e.store.Resolve(projectRef)
	if err != nil {
		return err
	}
	artifact, err := project.AddArtifact(model.ArtifactSpec{
		Name:   name,
		Type:   artifactType,
		Body:   body,
		Tags:   splitList(tags),
		Pinned: pin,
	})
	if err != nil {
		return err
	}
	if err := e.store.Save(project); err != nil {
		return err
	}
	return e.emit(artifact, fmt.Sprintf("added artifact %s (%s v%d, %s)", artifact.Name, artifact.ID, artifact.Version, artifact.Type))
}

func buildBody(content, file, ref, mediaType string) (model.Body, error) {
	supplied := 0
	for _, value := range []string{content, file, ref} {
		if strings.TrimSpace(value) != "" {
			supplied++
		}
	}
	if supplied != 1 {
		return model.Body{}, fmt.Errorf("supply exactly one of --content, --file, or --ref")
	}
	switch {
	case strings.TrimSpace(file) != "":
		raw, err := os.ReadFile(file)
		if err != nil {
			return model.Body{}, fmt.Errorf("read --file %s: %w", file, err)
		}
		return model.Body{Content: string(raw), MediaType: mediaType}, nil
	case strings.TrimSpace(ref) != "":
		return model.Body{Ref: ref, MediaType: mediaType}, nil
	default:
		return model.Body{Content: content, MediaType: mediaType}, nil
	}
}

func artifactList(e *env, args []string) error {
	var projectRef string
	var allVersions bool
	err := e.parse("artifact list", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.BoolVar(&allVersions, "all-versions", false, "list every revision instead of only the latest")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" {
		return fmt.Errorf("artifact list: --project is required")
	}
	project, err := e.store.Resolve(projectRef)
	if err != nil {
		return err
	}

	artifacts := project.LatestArtifacts()
	if allVersions {
		artifacts = project.Artifacts
	}
	if e.asJSON {
		return e.emit(artifacts, "")
	}
	if len(artifacts) == 0 {
		return e.emit(artifacts, fmt.Sprintf("project %s has no artifacts", project.Name))
	}
	var builder strings.Builder
	for _, artifact := range artifacts {
		pinned := " "
		if artifact.Pinned {
			pinned = "*"
		}
		fmt.Fprintf(&builder, "%s %s\tv%d\t%s\t%s\n", pinned, artifact.ID, artifact.Version, artifact.Type, artifact.Name)
	}
	return e.emit(artifacts, strings.TrimRight(builder.String(), "\n"))
}

func artifactShow(e *env, args []string) error {
	var projectRef, artifactRef string
	err := e.parse("artifact show", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&artifactRef, "artifact", "", "artifact id, or name for its latest revision (required)")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(artifactRef) == "" {
		return fmt.Errorf("artifact show: --project and --artifact are required")
	}
	project, err := e.store.Resolve(projectRef)
	if err != nil {
		return err
	}
	artifact, ok := project.ResolveArtifact(artifactRef)
	if !ok {
		return fmt.Errorf("artifact %q not found in project %q", artifactRef, project.Name)
	}
	if e.asJSON {
		return e.emit(artifact, "")
	}
	content, err := orchestrator.ArtifactContent(artifact)
	if err != nil {
		return err
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "%s (%s)\ntype: %s\nversion: %d\npinned: %t\n", artifact.Name, artifact.ID, artifact.Type, artifact.Version, artifact.Pinned)
	if artifact.ParentID != "" {
		fmt.Fprintf(&builder, "parent: %s\n", artifact.ParentID)
	}
	if len(artifact.Tags) > 0 {
		fmt.Fprintf(&builder, "tags: %s\n", strings.Join(artifact.Tags, ", "))
	}
	for _, key := range sortedKeys(artifact.Metadata) {
		fmt.Fprintf(&builder, "%s: %s\n", key, artifact.Metadata[key])
	}
	fmt.Fprintf(&builder, "\n%s\n", content)
	return e.emit(artifact, strings.TrimRight(builder.String(), "\n"))
}

func artifactPin(e *env, args []string) error {
	return setPinned(e, args, true, "artifact pin")
}

func artifactUnpin(e *env, args []string) error {
	return setPinned(e, args, false, "artifact unpin")
}

func setPinned(e *env, args []string, pinned bool, name string) error {
	var projectRef, artifactRef string
	err := e.parse(name, args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&artifactRef, "artifact", "", "artifact id, or name for its latest revision (required)")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(artifactRef) == "" {
		return fmt.Errorf("%s: --project and --artifact are required", name)
	}
	project, err := e.store.Resolve(projectRef)
	if err != nil {
		return err
	}
	target, ok := project.ResolveArtifact(artifactRef)
	if !ok {
		return fmt.Errorf("artifact %q not found in project %q", artifactRef, project.Name)
	}
	artifact, err := project.SetPinned(target.ID, pinned)
	if err != nil {
		return err
	}
	if err := e.store.Save(project); err != nil {
		return err
	}
	verb := "pinned"
	if !pinned {
		verb = "unpinned"
	}
	return e.emit(artifact, fmt.Sprintf("%s %s (%s v%d)", verb, artifact.Name, artifact.ID, artifact.Version))
}
