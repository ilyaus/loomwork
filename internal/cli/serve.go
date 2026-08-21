package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ilyaus/loomwork/internal/httpapi"
	"github.com/ilyaus/loomwork/web"
)

// defaultServeAddr binds the loopback interface only. Loomwork is single-user and
// local-first with no authentication, so the server must never be reachable from
// another host.
const defaultServeAddr = "127.0.0.1:8787"

func serve(e *env, args []string) error {
	var addr string
	err := e.parse("serve", args, func(flags *flag.FlagSet) {
		flags.StringVar(&addr, "addr", defaultServeAddr, "loopback address to listen on")
	})
	if err != nil {
		return err
	}
	addr, err = loopbackAddr(addr)
	if err != nil {
		return err
	}

	api, err := httpapi.New(httpapi.Options{Store: e.store, Assets: web.Assets(), Home: e.home})
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	server := &http.Server{
		Handler:           api.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	fmt.Fprintf(e.stderr, "loomwork ui on http://%s (workspace %s)\n", listener.Addr(), e.home)

	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve %s: %w", addr, err)
	case <-signals.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}

// loopbackAddr normalizes --addr and rejects anything that would expose the
// workbench beyond this machine. A bare port is accepted as a convenience.
func loopbackAddr(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultServeAddr, nil
	}
	if !strings.Contains(raw, ":") {
		if err := validatePort(raw); err != nil {
			return "", fmt.Errorf("address %q must be host:port or a port: %w", raw, err)
		}
		return net.JoinHostPort("127.0.0.1", raw), nil
	}
	host, port, err := net.SplitHostPort(raw)
	if err != nil {
		return "", fmt.Errorf("address %q must be host:port or a port: %w", raw, err)
	}
	if err := validatePort(port); err != nil {
		return "", fmt.Errorf("address %q must be host:port or a port: %w", raw, err)
	}
	host = strings.TrimSpace(host)
	if host == "" {
		host = "127.0.0.1"
	}
	if strings.EqualFold(host, "localhost") {
		return net.JoinHostPort(host, port), nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("address %q is not a loopback address: loomwork has no authentication and serves 127.0.0.1, ::1, or localhost only", raw)
	}
	return net.JoinHostPort(host, port), nil
}

// validatePort keeps a mistyped port a flag error rather than a listen error.
func validatePort(raw string) error {
	number, err := strconv.Atoi(raw)
	if err != nil || number < 0 || number > 65535 {
		return fmt.Errorf("port %q must be a number between 0 and 65535", raw)
	}
	return nil
}
