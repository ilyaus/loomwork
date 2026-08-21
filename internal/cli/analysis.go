package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ilyaus/loomwork/internal/analysis"
	"github.com/ilyaus/loomwork/internal/provider"
)

func analysisRun(e *env, args []string) error {
	var projectRef, selector, systemPrompt, outputName, tags string
	var noExtract bool
	var temperature, topP float64
	var maxTokens, seed, topK int
	err := e.parse("analysis run", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&selector, "model", "", "provider/model[#preset], e.g. ollama/qwen3:8b (required)")
		flags.StringVar(&systemPrompt, "system", "", "override the analysis system prompt")
		flags.StringVar(&outputName, "name", "", "name for the stored analysis artifact (default "+analysis.ArtifactName+")")
		flags.StringVar(&tags, "tags", "", "comma-separated tags for the stored analysis artifact")
		flags.BoolVar(&noExtract, "no-extract", false, "store the analysis without writing extracted requirements")
		flags.Float64Var(&temperature, "temperature", -1, "override temperature")
		flags.Float64Var(&topP, "top-p", -1, "override top_p")
		flags.IntVar(&topK, "top-k", -1, "override top_k")
		flags.IntVar(&maxTokens, "max-tokens", -1, "override max output tokens")
		flags.IntVar(&seed, "seed", -1, "override seed")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(selector) == "" {
		return fmt.Errorf("analysis run: --project and --model are required")
	}

	overrides := provider.Params{}
	if temperature >= 0 {
		overrides.Temperature = provider.Float(temperature)
	}
	if topP >= 0 {
		overrides.TopP = provider.Float(topP)
	}
	if topK >= 0 {
		overrides.TopK = provider.Int(topK)
	}
	if maxTokens >= 0 {
		overrides.MaxOutputTokens = provider.Int(maxTokens)
	}
	if seed >= 0 {
		overrides.Seed = provider.Int(seed)
	}

	analyzer := analysis.New(e.config, e.store, e.presets, nil)
	result, err := analyzer.Run(context.Background(), analysis.RunRequest{
		ProjectRef:   projectRef,
		Selector:     selector,
		SystemPrompt: systemPrompt,
		OutputName:   outputName,
		Tags:         splitList(tags),
		SkipExtract:  noExtract,
		Overrides:    overrides,
	})
	if err != nil {
		return err
	}
	return emitAnalysis(e, result)
}

func analysisImport(e *env, args []string) error {
	var projectRef, file, outputName, tags string
	var noExtract bool
	err := e.parse("analysis import", args, func(flags *flag.FlagSet) {
		flags.StringVar(&projectRef, "project", "", "project id or name (required)")
		flags.StringVar(&file, "file", "", "JSON file matching document-analysis.schema.json (required)")
		flags.StringVar(&outputName, "name", "", "name for the stored analysis artifact (default "+analysis.ArtifactName+")")
		flags.StringVar(&tags, "tags", "", "comma-separated tags for the stored analysis artifact")
		flags.BoolVar(&noExtract, "no-extract", false, "store the analysis without writing extracted requirements")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(projectRef) == "" || strings.TrimSpace(file) == "" {
		return fmt.Errorf("analysis import: --project and --file are required")
	}
	payload, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read --file %s: %w", file, err)
	}

	analyzer := analysis.New(e.config, e.store, e.presets, nil)
	result, err := analyzer.Import(analysis.ImportRequest{
		ProjectRef:  projectRef,
		Payload:     payload,
		SourcePath:  file,
		OutputName:  outputName,
		Tags:        splitList(tags),
		SkipExtract: noExtract,
	})
	if err != nil {
		return err
	}
	return emitAnalysis(e, result)
}

func emitAnalysis(e *env, result analysis.Result) error {
	if e.asJSON {
		return e.emit(result, "")
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "verdict: %s", result.Analysis.Verdict)
	if result.Analysis.SpecInSync != nil {
		fmt.Fprintf(&builder, "\tspec in sync: %t", *result.Analysis.SpecInSync)
	}
	builder.WriteString("\n")
	if result.Analysis.Summary != "" {
		fmt.Fprintf(&builder, "%s\n", result.Analysis.Summary)
	}
	if result.Provider != "" {
		fmt.Fprintf(&builder, "provider: %s\tmodel: %s", result.Provider, result.Model)
		if result.Preset != "" {
			fmt.Fprintf(&builder, "\tpreset: %s", result.Preset)
		}
		fmt.Fprintf(&builder, "\tduration: %dms\n", result.DurationMs)
	}
	fmt.Fprintf(&builder, "stored: %s (%s v%d)\n", result.Artifact.Name, result.Artifact.ID, result.Artifact.Version)
	if len(result.Analysis.Gaps) > 0 {
		builder.WriteString("\ngaps:\n")
		for _, gap := range result.Analysis.Gaps {
			fmt.Fprintf(&builder, "  - %s\n", gap)
		}
	}
	if len(result.Analysis.OpenQuestions) > 0 {
		builder.WriteString("\nopen questions:\n")
		for _, question := range result.Analysis.OpenQuestions {
			fmt.Fprintf(&builder, "  - %s\n", question)
		}
	}
	if len(result.Requirements) > 0 {
		fmt.Fprintf(&builder, "\nextracted %d requirement(s):\n", len(result.Requirements))
		for _, requirement := range result.Requirements {
			fmt.Fprintf(&builder, "  %s\t%s\n", requirement.ID, requirement.Text)
		}
	}
	return e.emit(result, strings.TrimRight(builder.String(), "\n"))
}
