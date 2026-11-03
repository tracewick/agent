package spool_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/spool"
)

func TestBacklogCountsUnclaimed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	src, err := spool.NewSource(spool.Config{
		Directory: dir,
		ReaderID:  "backlog",
		Now:       func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if b := src.Backlog(); b.Files != 0 || b.Bytes != 0 || b.OldestAge != 0 {
		t.Fatalf("empty %#v", b)
	}

	old := filepath.Join(dir, "00000000000000000001-aaaaaaaaaaaaaaaa.spool")
	if err := os.WriteFile(old, []byte("aaaa"), 0o600); err != nil { //nolint:gosec // temp fixture
		t.Fatal(err)
	}
	mtime := now.Add(-90 * time.Second)
	if err := os.Chtimes(old, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	shard := filepath.Join(dir, "ab")
	if err := os.Mkdir(shard, 0o700); err != nil { //nolint:gosec // temp fixture
		t.Fatal(err)
	}
	nested := filepath.Join(shard, "00000000000000000002-bbbbbbbbbbbbbbbb.spool")
	if err := os.WriteFile(nested, []byte("bbbbbb"), 0o600); err != nil { //nolint:gosec // temp fixture
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, ".hidden.spool"), []byte("x"), 0o600) //nolint:gosec // ignored
	_ = os.Mkdir(filepath.Join(dir, spool.RejectedDirectory), 0o700)          //nolint:gosec // ignored

	b := src.Backlog()
	if b.Files != 2 || b.Bytes != 10 {
		t.Fatalf("%#v", b)
	}
	if b.OldestAge < 89*time.Second || b.OldestAge > 91*time.Second {
		t.Fatalf("oldest %s", b.OldestAge)
	}
}
