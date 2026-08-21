package exec

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestStartEchoesLinesUntilStdinCloses(t *testing.T) {
	script := writeScript(t, "while read line; do echo \"seen:$line\"; done\necho done\n")
	process, err := Start(context.Background(), Command{Argv: []string{script}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	for _, line := range []string{"first", "second"} {
		if err := process.WriteLine(line); err != nil {
			t.Fatalf("WriteLine: %v", err)
		}
		got, err := process.ReadLine()
		if err != nil {
			t.Fatalf("ReadLine: %v", err)
		}
		if got != "seen:"+line {
			t.Fatalf("ReadLine = %q, want %q", got, "seen:"+line)
		}
	}
	if err := process.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := process.Close(); err != nil {
		t.Fatalf("Close (repeated): %v", err)
	}
}

func TestReadLineReportsEOFWhenTheProcessStops(t *testing.T) {
	script := writeScript(t, "echo only-line\n")
	process, err := Start(context.Background(), Command{Argv: []string{script}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = process.Close() }()

	if _, err := process.ReadLine(); err != nil {
		t.Fatalf("ReadLine: %v", err)
	}
	if _, err := process.ReadLine(); !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

func TestCloseReportsANonZeroExitAndStderr(t *testing.T) {
	script := writeScript(t, "echo boom >&2\nexit 4\n")
	process, err := Start(context.Background(), Command{Argv: []string{script}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := process.ReadLine(); !errors.Is(err, io.EOF) {
		t.Fatalf("ReadLine err = %v, want io.EOF", err)
	}
	err = process.Close()
	if err == nil {
		t.Fatal("expected an error: a bridge that dies is a fault, not a result")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("err = %v, want the stderr tail included", err)
	}
	if !strings.Contains(process.StderrTail(), "boom") {
		t.Errorf("StderrTail = %q", process.StderrTail())
	}
}

func TestCloseKillsAProcessThatIgnoresStdinClose(t *testing.T) {
	script := writeScript(t, "trap '' HUP\nsleep 60\n")
	process, err := Start(context.Background(), Command{Argv: []string{script}, Timeout: time.Second})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	closed := make(chan error, 1)
	go func() { closed <- process.Close() }()
	select {
	case <-closed:
	case <-time.After(30 * time.Second):
		t.Fatal("Close did not return: a stuck bridge must not hang the caller")
	}
}

func TestStreamingRejectsStdinAndAnEmptyCommand(t *testing.T) {
	if _, err := Start(context.Background(), Command{Argv: []string{"true"}, Stdin: "x"}); err == nil {
		t.Error("expected an error for Stdin on a streaming process")
	}
	if _, err := Start(context.Background(), Command{}); err == nil {
		t.Error("expected an error for an empty command")
	}
}

func TestWriteLineRejectsAnEmbeddedNewline(t *testing.T) {
	script := writeScript(t, "cat\n")
	process, err := Start(context.Background(), Command{Argv: []string{script}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = process.Close() }()
	if err := process.WriteLine("one\ntwo"); err == nil {
		t.Error("expected an error: a newline would frame two protocol messages")
	}
}
