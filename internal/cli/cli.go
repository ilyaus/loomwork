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
	"github.com/ilyaus/loomwork/internal/preset"
	"github.com/ilyaus/loomwork/internal/store"
)

const usage = `loomwork — orchestrate prompts over project artifacts

Usage:
  loomwork <group> <command> [flags]

Commands:
  project create   --name NAME [--description TEXT] [--tags a,b]
  project list
  project show     --project REF
  artifact add     --project REF --name NAME --type TYPE (--content TEXT | --file PATH | --ref PATH) [--tags a,b] [--pin]
  artifact list    --project REF [--all-versions]
  artifact show    --project REF --artifact REF
  artifact pin     --project REF --artifact ID
  artifact unpin   --project REF --artifact ID
  run              --project REF --artifact REF --model provider/model[#preset] --prompt TEXT|--prompt-file PATH
                   [--name NAME] [--type TYPE] [--tags a,b] [--pin] [--include-pinned]
                   [--temperature N] [--top-p N] [--max-tokens N] [--seed N]
  providers        list configured providers, presets, and credential status

Global flags (accepted by every command):
  --home PATH   workspace directory (default $LOOMWORK_HOME or ~/.loomwork)
  --json        emit machine-readable JSON

Artifact types: spec, log, test-result, diagram, doc, generated
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
		})
	case "artifact":
		return runGroup(rest, stdout, stderr, map[string]commandFunc{
			"add":   artifactAdd,
			"list":  artifactList,
			"show":  artifactShow,
			"pin":   artifactPin,
			"unpin": artifactUnpin,
		})
	case "run":
		return runCommand(runPrompt, rest, stdout, stderr)
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
	store   *store.FileStore
	presets *preset.Registry
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

	projects, err := store.NewFileStore(e.paths.ProjectsDir)
	if err != nil {
		return err
	}
	e.store = projects
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
