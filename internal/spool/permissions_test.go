package spool_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tracewick/agent/internal/spool"
)

// Documents the UID contract: when the process cannot traverse the spool
// directory (mode mismatch / wrong owner), claim and backlog see nothing.
// Full cross-UID docker/k8s checks live in deploy/ examples.
func TestUnreadableSpoolDirYieldsEmptyBacklogAndClaim(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	body := []byte("not-parsed-because-unlistable")
	if err := os.WriteFile(filepath.Join(dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool"), body, 0o600); err != nil { //nolint:gosec // temp fixture
		t.Fatal(err)
	}
	src, err := spool.NewSource(spool.Config{Directory: dir, ReaderID: "perm"})
	if err != nil {
		t.Fatal(err)
	}

	if chmodErr := os.Chmod(dir, 0o000); chmodErr != nil { //nolint:gosec // intentional denial
		t.Fatal(chmodErr)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // restore for TempDir cleanup

	if b := src.Backlog(); b.Files != 0 || b.Bytes != 0 {
		t.Fatalf("backlog under locked dir: %#v", b)
	}
	claimed, err := src.Claim()
	if err != nil {
		t.Fatal(err)
	}
	if claimed != nil {
		t.Fatalf("claim under locked dir: %#v", claimed)
	}
}
