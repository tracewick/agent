// Command tracewick-agent is the Tracewick delivery sidecar (early development).
package main

import (
	"fmt"
	"os"

	"github.com/tracewick/agent/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "tracewick-agent: %v\n", err)
		os.Exit(1)
	}
}
