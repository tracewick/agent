package cli_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/tracewick/agent/internal/cli"
	"github.com/tracewick/agent/internal/version"
)

func TestRunVersion(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	if err := cli.Run([]string{"version"}, &out, io.Discard); err != nil {
		t.Fatalf("Run(version): %v", err)
	}
	got := strings.TrimSpace(out.String())
	if got != version.String() {
		t.Fatalf("stdout = %q, want %q", got, version.String())
	}
}

func TestRunDefault(t *testing.T) {
	t.Parallel()

	var errBuf bytes.Buffer
	if err := cli.Run(nil, io.Discard, &errBuf); err != nil {
		t.Fatalf("Run(nil): %v", err)
	}
	if !strings.Contains(errBuf.String(), "early development") {
		t.Fatalf("stderr missing early-dev warning: %q", errBuf.String())
	}
}
