package exec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeScript materializes an executable shell script for driving the runner
// under test. Tests run on POSIX systems.
func writeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "script.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func TestRunCapturesOutputAndExitCode(t *testing.T) {
	script := writeScript(t, "echo out-line\necho err-line >&2\nexit 3\n")
	result, err := Run(context.Background(), Command{Argv: []string{script}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ExitCode != 3 {
		t.Fatalf("ExitCode = %d, want 3", result.ExitCode)
	}
	if strings.TrimSpace(result.Stdout) != "out-line" {
		t.Fatalf("Stdout = %q", result.Stdout)
	}
	if strings.TrimSpace(result.Stderr) != "err-line" {
		t.Fatalf("Stderr = %q", result.Stderr)
	}
	if result.Duration <= 0 {
		t.Fatalf("Duration = %v, want > 0", result.Duration)
	}
}

func TestRunPassesArgsStdinAndDir(t *testing.T) {
	dir := t.TempDir()
	script := writeScript(t, "printf 'args=%s ' \"$@\"\ncat\npwd\n")
	result, err := Run(context.Background(), Command{
		Argv:  []string{script, "one", "two"},
		Dir:   dir,
		Stdin: "from-stdin\n",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, want := range []string{"args=one args=two", "from-stdin", dir} {
		if !strings.Contains(result.Stdout, want) {
			t.Fatalf("Stdout %q missing %q", result.Stdout, want)
		}
	}
}

func TestRunEnvironmentAllowlist(t *testing.T) {
	t.Setenv("LOOMWORK_EXEC_ALLOWED", "yes")
	t.Setenv("LOOMWORK_EXEC_BLOCKED", "no")
	script := writeScript(t, "echo allowed=$LOOMWORK_EXEC_ALLOWED blocked=$LOOMWORK_EXEC_BLOCKED extra=$LOOMWORK_EXEC_EXTRA\n")
	result, err := Run(context.Background(), Command{
		Argv:     []string{script},
		Env:      []string{"LOOMWORK_EXEC_ALLOWED"},
		ExtraEnv: []string{"LOOMWORK_EXEC_EXTRA=set"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(result.Stdout) != "allowed=yes blocked= extra=set" {
		t.Fatalf("Stdout = %q", result.Stdout)
	}
}

func TestRunTimeout(t *testing.T) {
	script := writeScript(t, "sleep 5\n")
	result, err := Run(context.Background(), Command{
		Argv:    []string{script},
		Timeout: 100 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("Run should fail on timeout")
	}
	if !result.TimedOut {
		t.Fatal("TimedOut should be true")
	}
}

func TestRunMissingBinary(t *testing.T) {
	if _, err := Run(context.Background(), Command{Argv: []string{filepath.Join(t.TempDir(), "missing")}}); err == nil {
		t.Fatal("Run should fail for a missing binary")
	}
}

func TestRunEmptyCommand(t *testing.T) {
	if _, err := Run(context.Background(), Command{}); err == nil {
		t.Fatal("Run should reject an empty command")
	}
}

func TestCappedBufferDropsOverflow(t *testing.T) {
	buffer := &cappedBuffer{max: 4}
	if n, err := buffer.Write([]byte("123456")); err != nil || n != 6 {
		t.Fatalf("Write = (%d, %v), want (6, nil)", n, err)
	}
	if buffer.buf.String() != "1234" {
		t.Fatalf("buffer = %q, want %q", buffer.buf.String(), "1234")
	}
}

func TestCappedBufferIsSafeForAConcurrentReader(t *testing.T) {
	buffer := &cappedBuffer{max: maxCapturedBytes}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2000; i++ {
			_, _ = buffer.Write([]byte("stderr line\n"))
		}
	}()
	for i := 0; i < 2000; i++ {
		_ = buffer.String()
	}
	<-done
}
