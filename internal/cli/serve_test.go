package cli

import (
	"strings"
	"testing"
)

func TestLoopbackAddr(t *testing.T) {
	for raw, want := range map[string]string{
		"":                 defaultServeAddr,
		"9000":             "127.0.0.1:9000",
		":9000":            "127.0.0.1:9000",
		"localhost:9000":   "localhost:9000",
		"127.0.0.1:8787":   "127.0.0.1:8787",
		"[::1]:8787":       "[::1]:8787",
		"  127.0.0.1:80  ": "127.0.0.1:80",
	} {
		got, err := loopbackAddr(raw)
		if err != nil {
			t.Errorf("loopbackAddr(%q) = %v", raw, err)
			continue
		}
		if got != want {
			t.Errorf("loopbackAddr(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestServeRejectsNonLoopbackAddresses guards the single-user, no-auth guiding
// principle: the workbench must not be reachable from another host.
func TestServeRejectsNonLoopbackAddresses(t *testing.T) {
	home := t.TempDir()
	for _, addr := range []string{"0.0.0.0:8787", "192.168.1.10:8787", "example.com:8787", "[::]:8787"} {
		got := execErr(t, home, "serve", "--addr", addr)
		if !strings.Contains(got, "not a loopback address") {
			t.Errorf("serve --addr %s = %q, want a loopback-only error", addr, got)
		}
	}
	if got := execErr(t, home, "serve", "--addr", "127.0.0.1:notaport"); !strings.Contains(got, "must be host:port") {
		t.Errorf("error = %q, want an address parse error", got)
	}
}
