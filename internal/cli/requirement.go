package cli

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ilyaus/loomwork/internal/model"
)

func requirementCreate(e *env, args []string) error {
	var projectRef, text, textFile, sourceType, sourceRef, status, origin, tags string
	err := e.parse("requirement create", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&text, "text", "", "tester-friendly requirement text")
		flags.StringVar(&textFile, "text-file", "", "read the requirement text from this file")
		flags.StringVar(&sourceType, "source-type", "", "system of record: ado, confluence, github, other")
		flags.StringVar(&sourceRef, "source-ref", "", "reference in the source system, e.g. a work item id or page url")
		flags.StringVar(&status, "status", string(model.RequirementStatusActive), "active or obsolete; superseded is set only by update")
		flags.StringVar(&origin, "origin", string(model.RequirementOriginAuthored), "authored (QA entry) or extracted (document analysis)")
		flags.StringVar(&tags, "tags", "", "comma-separated tags")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" {
		return fmt.Errorf("requirement create: --project is required")
	}
	body, err := requirementText(text, textFile, true)
	if err != nil {
		return err
	}
	spec, err := buildRequirementSpec(body, sourceType, sourceRef, status, origin, tags)
	if err != nil {
		return err
	}
	requirement, err := e.store.CreateRequirement(projectRef, spec)
	if err != nil {
		return err
	}
	return e.emit(requirement, fmt.Sprintf("created requirement %s v%d (%s)", requirement.ID, requirement.Version, requirement.Status))
}

func requirementUpdate(e *env, args []string) error {
	var projectRef, requirementID, text, textFile, sourceType, sourceRef, tags string
	err := e.parse("requirement update", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&requirementID, "requirement", "", "requirement id (required)")
		flags.StringVar(&text, "text", "", "new requirement text (unchanged if omitted)")
		flags.StringVar(&textFile, "text-file", "", "read the new requirement text from this file")
		flags.StringVar(&sourceType, "source-type", "", "system of record: ado, confluence, github, other")
		flags.StringVar(&sourceRef, "source-ref", "", "reference in the source system")
		flags.StringVar(&tags, "tags", "", "comma-separated tags")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(requirementID) == "" {
		return fmt.Errorf("requirement update: --project and --requirement are required")
	}
	body, err := requirementText(text, textFile, false)
	if err != nil {
		return err
	}
	// Status and origin are inherited: an update produces a new active version
	// and supersedes the previous one, so they are not settable here.
	spec := model.RequirementSpec{
		Text:      body,
		SourceRef: strings.TrimSpace(sourceRef),
		Tags:      splitList(tags),
	}
	if strings.TrimSpace(sourceType) != "" {
		parsed, err := model.ParseSourceType(sourceType)
		if err != nil {
			return err
		}
		spec.SourceType = parsed
	}
	requirement, err := e.store.UpdateRequirement(projectRef, requirementID, spec)
	if err != nil {
		return err
	}
	return e.emit(requirement, fmt.Sprintf("updated requirement %s to v%d (v%d is now superseded)", requirement.ID, requirement.Version, requirement.Version-1))
}

func requirementSetStatus(e *env, args []string) error {
	var projectRef, requirementID, status string
	var version int
	err := e.parse("requirement set-status", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&requirementID, "requirement", "", "requirement id (required)")
		flags.StringVar(&status, "status", "", "active or obsolete (required); superseded is set only by update")
		flags.IntVar(&version, "version", 0, "version to change (default: the current version)")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(requirementID) == "" {
		return fmt.Errorf("requirement set-status: --project and --requirement are required")
	}
	if version < 0 {
		return fmt.Errorf("requirement set-status: --version must be 1 or greater (0 or omitted changes the current version)")
	}
	parsed, err := model.ParseRequirementStatus(status)
	if err != nil {
		return err
	}
	requirement, err := e.store.SetRequirementStatus(projectRef, requirementID, version, parsed)
	if err != nil {
		return err
	}
	return e.emit(requirement, fmt.Sprintf("requirement %s v%d is now %s", requirement.ID, requirement.Version, requirement.Status))
}

func requirementList(e *env, args []string) error {
	var projectRef, status string
	err := e.parse("requirement list", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&status, "status", "", "only list requirements with this status")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" {
		return fmt.Errorf("requirement list: --project is required")
	}
	requirements, err := e.store.ListRequirements(projectRef)
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		wanted, err := model.ParseRequirementStatus(status)
		if err != nil {
			return err
		}
		filtered := make([]*model.Requirement, 0, len(requirements))
		for _, requirement := range requirements {
			if requirement.Status == wanted {
				filtered = append(filtered, requirement)
			}
		}
		requirements = filtered
	}
	if e.asJSON {
		return e.emit(requirements, "")
	}
	if len(requirements) == 0 {
		return e.emit(requirements, "no requirements matched")
	}
	var builder strings.Builder
	for _, requirement := range requirements {
		fmt.Fprintf(&builder, "%s\tv%d\t%s\t%s\n", requirement.ID, requirement.Version, requirement.Status, firstLine(requirement.Text))
	}
	return e.emit(requirements, strings.TrimRight(builder.String(), "\n"))
}

func requirementShow(e *env, args []string) error {
	var projectRef, requirementID string
	var version int
	var history bool
	err := e.parse("requirement show", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&requirementID, "requirement", "", "requirement id (required)")
		flags.IntVar(&version, "version", 0, "version to show (default: the current version)")
		flags.BoolVar(&history, "history", false, "show every retained version instead of one")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(requirementID) == "" {
		return fmt.Errorf("requirement show: --project and --requirement are required")
	}
	if history && version != 0 {
		return fmt.Errorf("requirement show: use either --version or --history, not both")
	}
	if version < 0 {
		return fmt.Errorf("requirement show: --version must be 1 or greater (0 or omitted shows the current version)")
	}
	if history {
		versions, err := e.store.RequirementHistory(projectRef, requirementID)
		if err != nil {
			return err
		}
		if e.asJSON {
			return e.emit(versions, "")
		}
		var builder strings.Builder
		for _, requirement := range versions {
			fmt.Fprintf(&builder, "%s\n\n", formatRequirement(requirement))
		}
		return e.emit(versions, strings.TrimRight(builder.String(), "\n"))
	}
	requirement, err := e.store.LoadRequirement(projectRef, requirementID, version)
	if err != nil {
		return err
	}
	if e.asJSON {
		return e.emit(requirement, "")
	}
	return e.emit(requirement, formatRequirement(requirement))
}

func formatRequirement(requirement *model.Requirement) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s v%d\nstatus: %s\norigin: %s\n", requirement.ID, requirement.Version, requirement.Status, requirement.Origin)
	if requirement.SourceType != "" {
		fmt.Fprintf(&builder, "source: %s %s\n", requirement.SourceType, requirement.SourceRef)
	}
	if len(requirement.Tags) > 0 {
		fmt.Fprintf(&builder, "tags: %s\n", strings.Join(requirement.Tags, ", "))
	}
	for _, key := range sortedKeys(requirement.Metadata) {
		fmt.Fprintf(&builder, "%s: %s\n", key, requirement.Metadata[key])
	}
	fmt.Fprintf(&builder, "\n%s", requirement.Text)
	return builder.String()
}

// requirementText resolves the text flags. Updates may omit both flags to keep
// the current text, so required is false there.
func requirementText(text, textFile string, required bool) (string, error) {
	inline := strings.TrimSpace(text) != ""
	fromFile := strings.TrimSpace(textFile) != ""
	switch {
	case inline && fromFile:
		return "", fmt.Errorf("supply either --text or --text-file, not both")
	case fromFile:
		raw, err := os.ReadFile(textFile)
		if err != nil {
			return "", fmt.Errorf("read --text-file %s: %w", textFile, err)
		}
		return string(raw), nil
	case inline:
		return text, nil
	case required:
		return "", fmt.Errorf("supply either --text or --text-file")
	default:
		return "", nil
	}
}

func buildRequirementSpec(text, sourceType, sourceRef, status, origin, tags string) (model.RequirementSpec, error) {
	spec := model.RequirementSpec{
		Text:      text,
		SourceRef: strings.TrimSpace(sourceRef),
		Tags:      splitList(tags),
	}
	if strings.TrimSpace(sourceType) != "" {
		parsed, err := model.ParseSourceType(sourceType)
		if err != nil {
			return model.RequirementSpec{}, err
		}
		spec.SourceType = parsed
	}
	if strings.TrimSpace(status) != "" {
		parsed, err := model.ParseRequirementStatus(status)
		if err != nil {
			return model.RequirementSpec{}, err
		}
		spec.Status = parsed
	}
	if strings.TrimSpace(origin) != "" {
		parsed, err := model.ParseRequirementOrigin(origin)
		if err != nil {
			return model.RequirementSpec{}, err
		}
		spec.Origin = parsed
	}
	return spec, nil
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	if len(line) > 80 {
		return line[:77] + "..."
	}
	return line
}
