package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	osexec "os/exec"
	"path/filepath"
	"strings"
	"time"

	procexec "github.com/ilyaus/loomwork/internal/exec"
	"github.com/ilyaus/loomwork/internal/ingest"
	"github.com/ilyaus/loomwork/internal/model"
	"github.com/ilyaus/loomwork/internal/orchestrator"
)

// stringsFlag collects a repeatable string flag.
type stringsFlag []string

func (s *stringsFlag) String() string { return strings.Join(*s, ",") }

func (s *stringsFlag) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("value must not be empty")
	}
	*s = append(*s, value)
	return nil
}

func workbenchRun(e *env, args []string) error {
	var projectRef, scenarios, baseURL, runner, authConfig, tokenProviderConfig, outputName, tags string
	var dryRun bool
	var timeoutSeconds int
	var extraArgs stringsFlag
	err := e.parse("workbench run", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&scenarios, "scenarios", "", "comma-separated scenario artifact refs (required)")
		flags.StringVar(&baseURL, "base-url", "", "target REST API base URL (required)")
		flags.StringVar(&runner, "runner", "", "api-test-runner binary (default: workbench.runnerPath config, then PATH)")
		flags.StringVar(&authConfig, "auth-config", "", "path to the runner's auth-config JSON")
		flags.StringVar(&tokenProviderConfig, "token-provider-config", "", "path to the runner's token-provider JSON")
		flags.BoolVar(&dryRun, "dry-run", false, "validate scenarios without sending HTTP requests")
		flags.IntVar(&timeoutSeconds, "timeout", 0, "whole-run timeout in seconds (default: workbench.timeoutSeconds config)")
		flags.Var(&extraArgs, "arg", "extra argument passed to the runner verbatim (repeatable)")
		flags.StringVar(&outputName, "name", "", "name for the test-result artifact")
		flags.StringVar(&tags, "tags", "", "comma-separated tags for the test-result artifact")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(scenarios) == "" || strings.TrimSpace(baseURL) == "" {
		return fmt.Errorf("workbench run: --project, --scenarios, and --base-url are required")
	}
	if strings.TrimSpace(authConfig) != "" && strings.TrimSpace(tokenProviderConfig) != "" {
		return fmt.Errorf("workbench run: --auth-config and --token-provider-config are mutually exclusive")
	}

	binary, err := resolveRunner(firstNonEmpty(runner, e.config.Workbench.RunnerPath))
	if err != nil {
		return err
	}

	project, err := e.store.Resolve(projectRef)
	if err != nil {
		return err
	}
	refs := splitList(scenarios)
	resolved := make([]model.Artifact, 0, len(refs))
	for _, ref := range refs {
		artifact, ok := project.ResolveArtifact(ref)
		if !ok {
			return fmt.Errorf("workbench run: scenario artifact %q not found in project %q", ref, project.Name)
		}
		resolved = append(resolved, artifact)
	}

	// The runner reads scenario files from disk, so materialize each artifact
	// into a temporary directory it can scan.
	scenarioDir, err := os.MkdirTemp("", "loomwork-scenarios-")
	if err != nil {
		return fmt.Errorf("workbench run: create scenario directory: %w", err)
	}
	defer os.RemoveAll(scenarioDir)
	for index, artifact := range resolved {
		content, err := orchestrator.ArtifactContent(artifact)
		if err != nil {
			return err
		}
		filename := fmt.Sprintf("%03d-%s.md", index+1, sanitizeFilename(artifact.Name))
		if err := os.WriteFile(filepath.Join(scenarioDir, filename), []byte(content), 0o644); err != nil {
			return fmt.Errorf("workbench run: write scenario %s: %w", filename, err)
		}
	}

	argv := []string{binary, "--scenarios", scenarioDir, "--base-url", baseURL}
	if strings.TrimSpace(authConfig) != "" {
		argv = append(argv, "--auth-config", authConfig)
	}
	if strings.TrimSpace(tokenProviderConfig) != "" {
		argv = append(argv, "--token-provider-config", tokenProviderConfig)
	}
	if dryRun {
		argv = append(argv, "--dry-run")
	}
	argv = append(argv, extraArgs...)

	if timeoutSeconds <= 0 {
		timeoutSeconds = e.config.Workbench.TimeoutSeconds
	}
	result, err := procexec.Run(context.Background(), procexec.Command{
		Argv:    argv,
		Env:     e.config.Workbench.Env,
		Timeout: time.Duration(timeoutSeconds) * time.Second,
	})
	if err != nil {
		if strings.TrimSpace(result.Stderr) != "" {
			return fmt.Errorf("%w\nrunner stderr:\n%s", err, strings.TrimSpace(result.Stderr))
		}
		return err
	}

	report, err := ingest.ParseRunReport([]byte(result.Stdout))
	if err != nil {
		detail := strings.TrimSpace(result.Stderr)
		if detail == "" {
			detail = strings.TrimSpace(result.Stdout)
		}
		return fmt.Errorf("workbench run: runner exited %d without a readable report: %w\n%s", result.ExitCode, err, truncateForError(detail))
	}

	if strings.TrimSpace(outputName) == "" {
		outputName = resolved[0].Name + ".test-result"
	}
	spec := ingest.ArtifactSpec(outputName, []byte(result.Stdout), report, splitList(tags))
	spec.Metadata["exitCode"] = fmt.Sprintf("%d", result.ExitCode)
	spec.Metadata["durationMs"] = fmt.Sprintf("%d", result.Duration.Milliseconds())
	scenarioIDs := make([]string, 0, len(resolved))
	for _, artifact := range resolved {
		scenarioIDs = append(scenarioIDs, artifact.ID)
	}
	spec.Metadata["scenarios"] = strings.Join(scenarioIDs, ",")

	var stored model.Artifact
	if _, err := e.store.Update(project.ID, func(project *model.Project) error {
		added, err := project.DeriveArtifact(resolved[0].ID, spec)
		stored = added
		return err
	}); err != nil {
		return err
	}

	payload := map[string]any{"artifact": stored, "report": report}
	text := fmt.Sprintf("%s (%s v%d)\n%s", stored.Name, stored.ID, stored.Version, ingest.Summarize(report))
	return e.emit(payload, text)
}

// resolveRunner locates the api-test-runner binary: an explicit path is
// verified as-is; otherwise the well-known name is looked up on PATH.
func resolveRunner(configured string) (string, error) {
	if strings.TrimSpace(configured) != "" {
		if _, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("workbench run: runner binary %s: %w", configured, err)
		}
		return configured, nil
	}
	found, err := osexec.LookPath("api-test-runner")
	if err != nil {
		return "", fmt.Errorf("workbench run: api-test-runner not found on PATH; set workbench.runnerPath in config.json or pass --runner")
	}
	return found, nil
}

// sanitizeFilename keeps artifact-derived file names safe for a flat directory.
func sanitizeFilename(name string) string {
	var builder strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteRune('-')
		}
	}
	trimmed := strings.Trim(builder.String(), "-.")
	if trimmed == "" {
		return "scenario"
	}
	return strings.TrimSuffix(trimmed, ".md")
}

func truncateForError(text string) string {
	const max = 2048
	if len(text) <= max {
		return text
	}
	return text[:max] + " …"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
