// Command loomwork is the CLI entry point for the Loomwork orchestrator.
package main

import (
	"fmt"
	"os"

	"github.com/ilyaus/loomwork/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "loomwork: %v\n", err)
		os.Exit(1)
	}
}
