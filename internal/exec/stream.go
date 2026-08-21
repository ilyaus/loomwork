package exec

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	osexec "os/exec"
	"strings"
	"sync"
	"time"
)

// closeGrace is how long a process gets to exit after its stdin is closed
// before it is killed.
const closeGrace = 5 * time.Second

// maxLineBytes bounds one line read from a streaming process, so a bridge that
// emits an unterminated flood cannot exhaust memory.
const maxLineBytes = 8 << 20

// Process is a long-running child process spoken to over stdin and stdout, one
// line at a time. It exists for stateful bridges (an agent SDK session) that a
// single Run cannot express, and keeps Run's policy: no shell, an explicit
// environment allowlist, and a bounded lifetime.
//
// A Process is not safe for concurrent WriteLine callers; serialize writes or
// wrap it. ReadLine is expected to be driven by a single reader.
type Process struct {
	argv    []string
	cmd     *osexec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	stderr  *cappedBuffer
	cancel  context.CancelFunc
	closing sync.Once
	waitErr error
}

// Start launches the command with pipes attached and returns before it exits.
// Command.Stdin is not supported here: input is written line by line instead.
// The process is killed when ctx is cancelled, when Timeout elapses, or when
// Close is called.
func Start(ctx context.Context, command Command) (*Process, error) {
	if len(command.Argv) == 0 || strings.TrimSpace(command.Argv[0]) == "" {
		return nil, fmt.Errorf("exec: command is required")
	}
	if command.Stdin != "" {
		return nil, fmt.Errorf("exec: Stdin is not supported for a streaming process; use WriteLine")
	}

	timeout := command.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)

	cmd := osexec.CommandContext(runCtx, command.Argv[0], command.Argv[1:]...)
	cmd.Dir = command.Dir
	cmd.Env = buildEnv(command.Env, command.ExtraEnv)
	stderr := &cappedBuffer{max: maxCapturedBytes}
	cmd.Stderr = stderr
	// A bridge may spawn its own children (an SDK launching a CLI), and those
	// inherit the stderr pipe. Without a delay, Wait blocks until the last
	// grandchild exits, so shutting a session down would hang on them.
	cmd.WaitDelay = closeGrace

	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("exec %s: %w", command.Argv[0], err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("exec %s: %w", command.Argv[0], err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("exec %s: %w", command.Argv[0], err)
	}
	return &Process{
		argv:   command.Argv,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReaderSize(stdout, 64<<10),
		stderr: stderr,
		cancel: cancel,
	}, nil
}

// WriteLine sends one newline-terminated line to the process.
func (p *Process) WriteLine(line string) error {
	if strings.ContainsRune(line, '\n') {
		return fmt.Errorf("exec %s: a written line must not contain a newline", p.argv[0])
	}
	if _, err := io.WriteString(p.stdin, line+"\n"); err != nil {
		return fmt.Errorf("exec %s: write: %w (stderr: %s)", p.argv[0], err, p.StderrTail())
	}
	return nil
}

// ReadLine returns the next line the process wrote, without its newline. It
// returns io.EOF once the process closes stdout.
func (p *Process) ReadLine() (string, error) {
	var builder strings.Builder
	for {
		chunk, isPrefix, err := p.stdout.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) && builder.Len() == 0 {
				return "", io.EOF
			}
			if errors.Is(err, io.EOF) {
				return builder.String(), nil
			}
			return "", fmt.Errorf("exec %s: read: %w (stderr: %s)", p.argv[0], err, p.StderrTail())
		}
		if builder.Len()+len(chunk) > maxLineBytes {
			return "", fmt.Errorf("exec %s: output line exceeds %d bytes", p.argv[0], maxLineBytes)
		}
		builder.Write(chunk)
		if !isPrefix {
			return builder.String(), nil
		}
	}
}

// StderrTail returns what the process has written to stderr so far, trimmed to
// the last 2000 bytes so it can be embedded in an error message.
func (p *Process) StderrTail() string {
	text := strings.TrimSpace(p.stderr.String())
	if len(text) > 2000 {
		return "..." + text[len(text)-2000:]
	}
	return text
}

// Close closes stdin, waits briefly for the process to exit on its own, then
// kills it. It reports the process's exit error, if any; a non-zero exit is an
// error here, unlike Run, because a bridge that dies is a fault rather than a
// result. Close is safe to call more than once.
func (p *Process) Close() error {
	p.closing.Do(func() {
		_ = p.stdin.Close()
		done := make(chan error, 1)
		go func() { done <- p.cmd.Wait() }()
		select {
		case err := <-done:
			p.waitErr = err
		case <-time.After(closeGrace):
			p.cancel()
			p.waitErr = <-done
		}
		p.cancel()
	})
	if p.waitErr != nil {
		return fmt.Errorf("exec %s: %w (stderr: %s)", p.argv[0], p.waitErr, p.StderrTail())
	}
	return nil
}
