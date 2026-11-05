//go:build e2e

package e2e_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var (
	agentBinOnce sync.Once
	agentBinPath string
	agentBinErr  error
)

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func agentBinary(t *testing.T) string {
	t.Helper()
	agentBinOnce.Do(func() {
		root := repoRoot(t)
		dir, err := os.MkdirTemp("", "tracewick-agent-e2e-bin-*")
		if err != nil {
			agentBinErr = err
			return
		}
		bin := filepath.Join(dir, "tracewick-agent")
		cmd := exec.Command("go", "build", "-o", bin, "./cmd/tracewick-agent")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			agentBinErr = fmt.Errorf("%w: %s", err, out)
			return
		}
		agentBinPath = bin
	})
	if agentBinErr != nil {
		t.Fatalf("build agent: %v", agentBinErr)
	}
	t.Logf("e2e: agent binary %s", agentBinPath)
	return agentBinPath
}

func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot(t), "testdata", "spool", "v2", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func seedSpool(t *testing.T, dir string, body []byte, n int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil { //nolint:gosec // test spool
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		name := filepath.Join(dir, formatSpoolName(i+1))
		if err := os.WriteFile(name, body, 0o600); err != nil { //nolint:gosec // test spool
			t.Fatal(err)
		}
	}
}

func formatSpoolName(seq int) string {
	// 20-digit sequence + 16-hex aggregation hint (hint only; headers still win).
	return fmt.Sprintf("%020d-aaaaaaaaaaaaaaaa.spool", seq)
}

func listSpoolLeftovers(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, e := range entries {
		name := e.Name()
		if name == "rejected" {
			continue
		}
		if strings.HasSuffix(name, ".spool") || strings.Contains(name, ".spool.claimed-") {
			left = append(left, name)
		}
	}
	return left
}
