package cli

import (
	"flag"
	"fmt"
	"net"
	"net/http"

	"github.com/ilyaus/loomwork/internal/server"
)

func serve(e *env, args []string) error {
	var addr string
	err := e.parse("serve", args, func(flags *flag.FlagSet) {
		flags.StringVar(&addr, "addr", "127.0.0.1:8790", "address to listen on")
	})
	if err != nil {
		return err
	}

	handler := server.New(e.home, e.config, e.store, e.presets, e.cues)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	fmt.Fprintf(e.stderr, "loomwork server: workspace %s\n", e.home)
	fmt.Fprintf(e.stderr, "loomwork server: listening on http://%s\n", listener.Addr())
	return http.Serve(listener, handler)
}
