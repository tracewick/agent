package version_test

import (
	"testing"

	"github.com/tracewick/agent/internal/version"
)

func TestString(t *testing.T) {
	t.Parallel()

	got := version.String()
	if got == "" {
		t.Fatal("version.String() returned empty string")
	}
	if got != version.Version {
		t.Fatalf("version.String() = %q, want %q", got, version.Version)
	}
}
