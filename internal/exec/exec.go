// Package exec runs external processes for the testing workbench. It never
// invokes a shell: commands are argv vectors, the environment is an explicit
// allowlist, and every run is bounded by a context or timeout. Loomwork
// orchestrates sibling tools (api-test-runner first); it does not interpret
// what they do, so this package stays generic.
package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	osexec "os/exec"
	"strings"
	"sync"
	"time"
)

// DefaultTimeout bounds a run when the command does not set one.
const DefaultTimeout = 10 * time.Minute

// maxCapturedBytes bounds each captured stream so a runaway process cannot
// exhaust memory. Output beyond the cap is dropped, not an error.
const maxCapturedBytes = 8 << 20

// Command describes a single process invocation.
type Command struct {
	// Argv is the program and its arguments. Argv[0] is resolved via PATH
	// unless it contains a path separator.
	Argv []string
	// Dir is the working directory (defaults to the current directory).
	Dir string
	// Env is an explicit allowlist of environment variable names passed
	// through from the parent process. Nothing else is inherited.
	Env []string
	// ExtraEnv sets additional NAME=value pairs, after the allowlist.
	ExtraEnv []string
	// Stdin is written to the process's standard input.
	Stdin string
	// Timeout bounds the run; zero means DefaultTimeout.
	Timeout time.Duration
}

// Result captures a finished (or killed) process.
type Result struct {
	ExitCode int           `json:"exitCode"`
	Stdout   string        `json:"-"`
	Stderr   string        `json:"-"`
	Duration time.Duration `json:"-"`
	TimedOut bool          `json:"timedOut,omitempty"`
}

// cappedBuffer keeps at most max bytes and silently drops the rest. It is safe
// for a reader to call String while os/exec's copier goroutine writes.
type cappedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	remaining := c.max - c.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			c.buf.Write(p[:remaining])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil
}

func (c *cappedBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

// Run executes the command and waits for it. A non-zero exit is not an error:
// callers inspect Result.ExitCode, because a failing test run is a result, not
// a fault. Errors are reserved for the process not running at all (missing
// binary, bad directory) or being killed by the deadline.
func Run(ctx context.Context, command Command) (Result, error) {
	if len(command.Argv) == 0 || strings.TrimSpace(command.Argv[0]) == "" {
		return Result{}, fmt.Errorf("exec: command is required")
	}

	timeout := command.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := osexec.CommandContext(ctx, command.Argv[0], command.Argv[1:]...)
	cmd.Dir = command.Dir
	cmd.Env = buildEnv(command.Env, command.ExtraEnv)
	if command.Stdin != "" {
		cmd.Stdin = strings.NewReader(command.Stdin)
	}
	stdout := &cappedBuffer{max: maxCapturedBytes}
	stderr := &cappedBuffer{max: maxCapturedBytes}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	started := time.Now()
	err := cmd.Run()
	result := Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: time.Since(started),
	}

	if ctx.Err() != nil {
		result.TimedOut = true
		result.ExitCode = -1
		return result, fmt.Errorf("exec %s: timed out after %s", command.Argv[0], timeout)
	}
	var exitErr *osexec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("exec %s: %w", command.Argv[0], err)
	}
	return result, nil
}

// buildEnv assembles the child environment: allowlisted names copied from the
// parent, then explicit NAME=value extras (which win on collision).
func buildEnv(allow []string, extra []string) []string {
	env := make([]string, 0, len(allow)+len(extra))
	for _, name := range allow {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return append(env, extra...)
}
