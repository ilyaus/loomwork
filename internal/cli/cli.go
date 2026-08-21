// Package cli implements the loomwork command line. It contains argument
// parsing and output formatting only; all behavior lives in internal packages.
package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/ilyaus/loomwork/internal/config"
	"github.com/ilyaus/loomwork/internal/cuenote"
	"github.com/ilyaus/loomwork/internal/preset"
	"github.com/ilyaus/loomwork/internal/store"
)

const usage = `loomwork — QA workbench: projects, requirements, and prompt runs

Usage:
  loomwork <group> <command> [flags]

Commands:
  project create   --name NAME [--description TEXT] [--tags a,b]
                   [--source "name=NAME,type=github,url=URL[,local=PATH][,s3=URI]" ...]
  project list
  project show     --project REF
  project source   --project REF --source "name=NAME,type=ado,url=URL" ...
  requirement create     --project REF (--text TEXT | --text-file PATH)
                         [--source-type ado|confluence|github|other] [--source-ref REF]
                         [--status active|obsolete] [--origin authored|extracted]
                         [--tags a,b]
  requirement list       --project REF [--status STATUS]
  requirement show       --project REF --requirement ID [--version N | --history]
  requirement update     --project REF --requirement ID [--text TEXT | --text-file PATH]
                         [--source-type TYPE] [--source-ref REF] [--tags a,b]
  requirement set-status --project REF --requirement ID --status active|obsolete [--version N]
  artifact add     --project REF --name NAME --type TYPE (--content TEXT | --file PATH | --ref PATH) [--tags a,b] [--pin]
  artifact list    --project REF [--all-versions]
  artifact show    --project REF --artifact REF
  artifact pin     --project REF --artifact ID
  artifact unpin   --project REF --artifact ID
  analysis run     --project REF --model provider/model[#preset]
                   [--system TEXT] [--name NAME] [--tags a,b] [--no-extract]
                   [--temperature N] [--top-p N] [--max-tokens N] [--seed N]
  analysis import  --project REF --file PATH [--name NAME] [--tags a,b] [--no-extract]
  agent-definition create  --project REF --name NAME (--body TEXT | --body-file PATH)
                           [--target claude-agent-sdk|copilot-sdk] [--model MODEL]
                           [--tools read_swagger,read_requirements] [--description TEXT] [--tags a,b]
  agent-definition update  --project REF --name NAME [--body TEXT | --body-file PATH]
                           [--target TARGET] [--model MODEL] [--tools a,b] [--tags a,b]
  agent-definition list    --project REF
  agent-definition show    --project REF --name NAME [--version N | --history]
  agent-definition rule-create     --project REF --rule ID --title TEXT --rationale TEXT
                                   [--methods GET,POST] [--path /orders/*] [--scenario SCENARIO]
                                   [--spec-status N] [--action expect-status|expect-empty-collection|skip-test]
                                   [--expect-status N] [--tags a,b]
  agent-definition rule-update     --project REF --rule ID [same flags as rule-create]
  agent-definition rule-set-status --project REF --rule ID --status active|obsolete [--version N]
  agent-definition rule-list       --project REF [--active]
  agent-definition rule-show       --project REF --rule ID [--version N | --history]
  test-suite generate --project REF --suite ID --agent NAME --spec PATH
                      [--templates a.json,b.json] [--model MODEL] [--title TEXT]
                      [--description TEXT] [--tags a,b] [--instructions TEXT]
  test-suite import   --project REF --file PATH [--suite ID] [--title TEXT] [--tags a,b]
  test-suite list     --project REF
  test-suite show     --project REF --suite ID [--version N | --history]
  cue list         [--tag a,b] [--search TEXT] [--limit N]
  cue show         --cue REF
  run              --project REF --artifact REF --model provider/model[#preset]
                   (--prompt TEXT | --prompt-file PATH | --cue REF [--var key=value ...])
                   [--name NAME] [--type TYPE] [--tags a,b] [--pin] [--include-pinned]
                   [--temperature N] [--top-p N] [--max-tokens N] [--seed N]
  workbench run    --project REF --scenarios ART[,ART...] --base-url URL
                   [--runner PATH] [--auth-config PATH | --token-provider-config PATH]
                   [--dry-run] [--arg VALUE ...] [--timeout SECONDS]
                   [--name NAME] [--tags a,b]
  serve            [--addr 127.0.0.1:8787] browser UI over this workspace (loopback only)
  providers        list configured providers, presets, and credential status

Global flags (accepted by every command):
  --home PATH   workspace directory (default $LOOMWORK_HOME or ~/.loomwork)
  --json        emit machine-readable JSON

Artifact types: spec, log, test-result, diagram, doc, generated
Test scenarios: happy-path, missing-item, invalid-input, missing-authentication,
  unauthorized, conflict, rate-limit, server-error, other

Test generation runs an agent SDK session; see docs/agent-bridge-protocol.md for
the Claude Agent SDK setup. A suite whose cases are not all linked to requirements
is stored and flagged INCOMPLETE rather than rejected or silently accepted.
`

// Run dispatches a command line. It returns an error for any failure; the caller
// is responsible for the exit code.
func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, usage)
		return nil
	}

	group := args[0]
	rest := args[1:]

	switch group {
	case "project":
		return runGroup(rest, stdout, stderr, map[string]commandFunc{
			"create": projectCreate,
			"list":   projectList,
			"show":   projectShow,
			"source": projectSource,
		})
	case "requirement":
		return runGroup(rest, stdout, stderr, map[string]commandFunc{
			"create":     requirementCreate,
			"list":       requirementList,
			"show":       requirementShow,
			"update":     requirementUpdate,
			"set-status": requirementSetStatus,
		})
	case "artifact":
		return runGroup(rest, stdout, stderr, map[string]commandFunc{
			"add":   artifactAdd,
			"list":  artifactList,
			"show":  artifactShow,
			"pin":   artifactPin,
			"unpin": artifactUnpin,
		})
	case "analysis":
		return runGroup(rest, stdout, stderr, map[string]commandFunc{
			"run":    analysisRun,
			"import": analysisImport,
		})
	case "agent-definition":
		return runGroup(rest, stdout, stderr, map[string]commandFunc{
			"create":          agentDefinitionCreate,
			"update":          agentDefinitionUpdate,
			"list":            agentDefinitionList,
			"show":            agentDefinitionShow,
			"rule-create":     overrideRuleCreate,
			"rule-update":     overrideRuleUpdate,
			"rule-set-status": overrideRuleSetStatus,
			"rule-list":       overrideRuleList,
			"rule-show":       overrideRuleShow,
		})
	case "test-suite":
		return runGroup(rest, stdout, stderr, map[string]commandFunc{
			"generate": testSuiteGenerate,
			"import":   testSuiteImport,
			"list":     testSuiteList,
			"show":     testSuiteShow,
		})
	case "cue":
		return runGroup(rest, stdout, stderr, map[string]commandFunc{
			"list": cueList,
			"show": cueShow,
		})
	case "run":
		return runCommand(runPrompt, rest, stdout, stderr)
	case "workbench":
		return runGroup(rest, stdout, stderr, map[string]commandFunc{
			"run": workbenchRun,
		})
	case "serve":
		return runCommand(serve, rest, stdout, stderr)
	case "providers":
		return runCommand(providersList, rest, stdout, stderr)
	default:
		return fmt.Errorf("unknown command %q\n\n%s", group, usage)
	}
}

type commandFunc func(*env, []string) error

func runGroup(args []string, stdout, stderr io.Writer, commands map[string]commandFunc) error {
	if len(args) == 0 {
		names := make([]string, 0, len(commands))
		for name := range commands {
			names = append(names, name)
		}
		return fmt.Errorf("missing subcommand: expected one of %s", strings.Join(names, ", "))
	}
	command, ok := commands[args[0]]
	if !ok {
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
	return runCommand(command, args[1:], stdout, stderr)
}

func runCommand(command commandFunc, args []string, stdout, stderr io.Writer) error {
	environment := &env{stdout: stdout, stderr: stderr}
	return command(environment, args)
}

// env carries resolved workspace state and output settings for one command.
type env struct {
	stdout  jsonWriter
	stderr  io.Writer
	asJSON  bool
	home    string
	paths   config.Paths
	config  config.Config
	store   *store.DirStore
	presets *preset.Registry
	cues    cuenote.Client
}

type jsonWriter = io.Writer

// bindGlobals registers the flags every command accepts.
func (e *env) bindGlobals(flags *flag.FlagSet) {
	flags.StringVar(&e.home, "home", "", "workspace directory (default $LOOMWORK_HOME or ~/.loomwork)")
	flags.BoolVar(&e.asJSON, "json", false, "emit machine-readable JSON")
}

// open resolves the workspace: paths, config, preset registry, and project store.
func (e *env) open() error {
	home, err := config.Home(e.home)
	if err != nil {
		return err
	}
	e.home = home
	e.paths = config.ResolvePaths(home)

	cfg, err := config.Load(e.paths.ConfigFile)
	if err != nil {
		return err
	}
	e.config = cfg

	presets, err := preset.LoadFile(e.paths.PresetsFile)
	if err != nil {
		return err
	}
	e.presets = presets

	projects, err := store.NewDirStore(e.paths.ProjectsDir)
	if err != nil {
		return err
	}
	e.store = projects
	e.cues = cuenote.NewHTTPClient(cfg.CueNote)
	return nil
}

// parse binds global flags, lets the caller bind command flags, parses, and then
// opens the workspace.
func (e *env) parse(name string, args []string, bind func(*flag.FlagSet)) error {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(e.stderr)
	e.bindGlobals(flags)
	if bind != nil {
		bind(flags)
	}
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse flags for %s: %w", name, err)
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("%s: unexpected arguments: %s", name, strings.Join(flags.Args(), " "))
	}
	return e.open()
}

// emit writes payload as JSON when --json is set, otherwise writes the text form.
func (e *env) emit(payload any, text string) error {
	if e.asJSON {
		encoder := json.NewEncoder(e.stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(payload)
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	_, err := io.WriteString(e.stdout, text)
	return err
}

func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}
