package cli

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/ilyaus/loomwork/internal/cuenote"
)

// variableFlag collects repeated `--var key=value` pairs.
type variableFlag map[string]string

func (v variableFlag) String() string {
	pairs := make([]string, 0, len(v))
	for _, key := range sortedKeys(v) {
		pairs = append(pairs, key+"="+v[key])
	}
	return strings.Join(pairs, ",")
}

func (v variableFlag) Set(raw string) error {
	key, value, found := strings.Cut(raw, "=")
	key = strings.TrimSpace(key)
	if !found || key == "" {
		return fmt.Errorf("expected key=value, got %q", raw)
	}
	if _, duplicate := v[key]; duplicate {
		return fmt.Errorf("variable %q supplied twice", key)
	}
	v[key] = value
	return nil
}

func cueList(e *env, args []string) error {
	var tags, search string
	var limit int
	err := e.parse("cue list", args, func(flags *flag.FlagSet) {
		flags.StringVar(&tags, "tag", "", "comma-separated tags every cue must carry")
		flags.StringVar(&search, "search", "", "free-text search over cue names and bodies")
		flags.IntVar(&limit, "limit", 0, "maximum number of cues to return")
	})
	if err != nil {
		return err
	}

	cues, err := e.cues.ListCues(context.Background(), cuenote.CueFilter{
		Tags:   splitList(tags),
		Search: search,
		Limit:  limit,
	})
	if err != nil {
		return err
	}

	if e.asJSON {
		return e.emit(map[string]any{"cues": cues}, "")
	}
	if len(cues) == 0 {
		return e.emit(map[string]any{"cues": cues}, "no cues")
	}
	var builder strings.Builder
	for _, cue := range cues {
		fmt.Fprintf(&builder, "%s\t%s", cue.ID, cue.Name)
		if variables := cueVariables(cue); len(variables) > 0 {
			fmt.Fprintf(&builder, "\tvars: %s", strings.Join(variables, ", "))
		}
		if len(cue.Tags) > 0 {
			fmt.Fprintf(&builder, "\ttags: %s", strings.Join(cue.Tags, ", "))
		}
		builder.WriteString("\n")
	}
	return e.emit(map[string]any{"cues": cues}, strings.TrimRight(builder.String(), "\n"))
}

func cueShow(e *env, args []string) error {
	var ref string
	err := e.parse("cue show", args, func(flags *flag.FlagSet) {
		flags.StringVar(&ref, "cue", "", "cue id or name (required)")
	})
	if err != nil {
		return err
	}
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("cue show: --cue is required")
	}

	cue, err := cuenote.Resolve(context.Background(), e.cues, ref)
	if err != nil {
		return err
	}

	if e.asJSON {
		return e.emit(cue, "")
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s\t%s\n", cue.ID, cue.Name)
	if len(cue.Tags) > 0 {
		fmt.Fprintf(&builder, "tags: %s\n", strings.Join(cue.Tags, ", "))
	}
	if variables := cueVariables(cue); len(variables) > 0 {
		fmt.Fprintf(&builder, "variables: %s\n", strings.Join(variables, ", "))
	}
	fmt.Fprintf(&builder, "\n%s\n", cue.Body)
	return e.emit(cue, strings.TrimRight(builder.String(), "\n"))
}

// cueVariables prefers the placeholders actually present in the body over the
// service's declared list, so a stale declaration cannot mislead the caller.
func cueVariables(cue cuenote.Cue) []string {
	if found := cuenote.TemplateVariables(cue.Body); len(found) > 0 {
		return found
	}
	declared := append([]string(nil), cue.Variables...)
	sort.Strings(declared)
	return declared
}

// resolveCuePrompt turns a cue reference plus variables into prompt text.
func resolveCuePrompt(ctx context.Context, client cuenote.Client, ref string, values map[string]string) (cuenote.Cue, string, error) {
	cue, err := cuenote.Resolve(ctx, client, ref)
	if err != nil {
		return cuenote.Cue{}, "", err
	}
	rendered, err := cue.Render(values)
	if err != nil {
		return cuenote.Cue{}, "", err
	}
	if strings.TrimSpace(rendered) == "" {
		return cuenote.Cue{}, "", fmt.Errorf("cue %q renders to an empty prompt", ref)
	}
	return cue, rendered, nil
}
