// Package cli implements the tracewick-agent command entrypoints.
package cli

import (
	"fmt"
	"io"

	"github.com/tracewick/agent/internal/version"
)

// Run executes the CLI with the given args. stdout/stderr are injectable for tests.
func Run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 1 && (args[0] == "-version" || args[0] == "--version" || args[0] == "version") {
		_, _ = fmt.Fprintln(stdout, version.String())
		return nil
	}

	_, _ = fmt.Fprintf(stderr, "tracewick-agent %s — early development; not production-ready\n", version.String())
	_, _ = fmt.Fprintln(stderr, "usage: tracewick-agent version")
	return nil
}
