package spool_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tracewick/agent/internal/spool"
)

func writeSpool(t *testing.T, dir, name string, body []byte) {
	t.Helper()
	path := filepath.Join(dir, filepath.Base(name))
	if err := os.WriteFile(path, body, 0o600); err != nil { //nolint:gosec // temp dir test helper
		t.Fatal(err)
	}
}

func TestClaimAckReleaseAndStaleReclaim(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	src, err := spool.NewSource(spool.Config{
		Directory:    dir,
		ReaderID:     "reader-a",
		MaxFileBytes: 1 << 20,
		StaleClaim:   10 * time.Minute,
		Now:          func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}

	fixture, readErr := os.ReadFile(filepath.Join("..", "..", "testdata", "spool", "v2", "web-request.spool"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	name := "00000000000000000001-aaaaaaaaaaaaaaaa.spool"
	writeSpool(t, dir, name, fixture)

	claimed, claimErr := src.Claim()
	if claimErr != nil || claimed == nil {
		t.Fatalf("claim: %v %#v", claimErr, claimed)
	}
	if claimed.Name != name {
		t.Fatalf("name = %q", claimed.Name)
	}
	wantPath := filepath.Join(dir, "."+name+".claimed-reader-a-1700000000")
	if claimed.Path != wantPath {
		t.Fatalf("path = %q want %q", claimed.Path, wantPath)
	}
	if _, parseErr := spool.Parse(claimed.Bytes); parseErr != nil {
		t.Fatalf("parse claimed: %v", parseErr)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), ".") {
		t.Fatalf("entries after claim: %v", names(entries))
	}

	if releaseErr := src.Release(claimed); releaseErr != nil {
		t.Fatal(releaseErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, name)); statErr != nil {
		t.Fatalf("released file missing: %v", statErr)
	}

	claimed, claimErr = src.Claim()
	if claimErr != nil || claimed == nil {
		t.Fatalf("reclaim: %v", claimErr)
	}
	if ackErr := src.Acknowledge(claimed); ackErr != nil {
		t.Fatal(ackErr)
	}
	entries, _ = os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("entries after ack: %v", names(entries))
	}
}

func TestStaleClaimReclaimedOnScan(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Unix(1_700_000_601, 0) // >10 minutes after claim epoch
	src, err := spool.NewSource(spool.Config{
		Directory:  dir,
		ReaderID:   "reader-b",
		StaleClaim: 10 * time.Minute,
		Now:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}

	name := "00000000000000000002-bbbbbbbbbbbbbbbb.spool"
	claimedName := "." + name + ".claimed-reader-a-1700000000"
	writeSpool(t, dir, claimedName, []byte("x"))

	got, claimErr := src.Claim()
	if claimErr != nil {
		t.Fatal(claimErr)
	}
	if got == nil || got.Name != name {
		t.Fatalf("stale reclaim claim = %#v", got)
	}
	if !strings.Contains(got.Path, "reader-b-1700000601") {
		t.Fatalf("new claim path = %q", got.Path)
	}
}

func TestTwoReadersOneWinner(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	a, errA := spool.NewSource(spool.Config{Directory: dir, ReaderID: "a", Now: func() time.Time { return now }})
	if errA != nil {
		t.Fatal(errA)
	}
	b, errB := spool.NewSource(spool.Config{Directory: dir, ReaderID: "b", Now: func() time.Time { return now }})
	if errB != nil {
		t.Fatal(errB)
	}
	name := "00000000000000000003-cccccccccccccccc.spool"
	writeSpool(t, dir, name, []byte("payload"))

	ca, claimA := a.Claim()
	if claimA != nil || ca == nil {
		t.Fatalf("a: %v %#v", claimA, ca)
	}
	cb, claimB := b.Claim()
	if claimB != nil {
		t.Fatal(claimB)
	}
	if cb != nil {
		t.Fatalf("b should lose claim, got %#v", cb)
	}
}

func TestRejectAndShardAndOversized(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	src, err := spool.NewSource(spool.Config{
		Directory:        dir,
		ReaderID:         "reader-a",
		MaxFileBytes:     8,
		MaxRejectedFiles: 2,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}

	shard := filepath.Join(dir, "ab")
	if mkdirErr := os.Mkdir(shard, 0o700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	name := "ab/00000000000000000004-dddddddddddddddd.spool"
	writeSpool(t, shard, filepath.Base(name), []byte("tiny"))

	claimed, claimErr := src.Claim()
	if claimErr != nil || claimed == nil || claimed.Name != name {
		t.Fatalf("shard claim: %v %#v", claimErr, claimed)
	}
	if rejectErr := src.Reject(claimed); rejectErr != nil {
		t.Fatal(rejectErr)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "rejected", filepath.Base(name))); statErr != nil {
		t.Fatalf("rejected missing: %v", statErr)
	}

	bigName := "00000000000000000005-eeeeeeeeeeeeeeee.spool"
	writeSpool(t, dir, bigName, []byte("0123456789abcdef")) // 16 > max 8
	got, claimBig := src.Claim()
	if claimBig != nil {
		t.Fatal(claimBig)
	}
	if got != nil {
		t.Fatalf("oversized should not return claim: %#v", got)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "rejected")); len(entries) < 1 {
		t.Fatalf("expected oversized in rejected, got %d", len(entries))
	}
}

func TestLegacyStaleClaimUsesMtime(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	src, err := spool.NewSource(spool.Config{
		Directory:  dir,
		ReaderID:   "reader-c",
		StaleClaim: time.Minute,
		Now:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}

	name := "00000000000000000006-ffffffffffffffff.spool"
	legacy := "." + name + ".claimed-oldreader" // no epoch
	path := filepath.Join(dir, legacy)
	writeSpool(t, dir, legacy, []byte("x"))
	old := now.Add(-2 * time.Minute)
	if chtimesErr := os.Chtimes(path, old, old); chtimesErr != nil {
		t.Fatal(chtimesErr)
	}

	got, claimErr := src.Claim()
	if claimErr != nil || got == nil || got.Name != name {
		t.Fatalf("legacy stale: %v %#v", claimErr, got)
	}
}

func TestValidateReaderID(t *testing.T) {
	t.Parallel()
	if err := spool.ValidateReaderID("ok_id.1"); err != nil {
		t.Fatal(err)
	}
	if err := spool.ValidateReaderID(""); err == nil {
		t.Fatal("expected error")
	}
	if _, err := spool.NewSource(spool.Config{Directory: t.TempDir(), ReaderID: "bad id"}); err == nil {
		t.Fatal("expected bad reader id error")
	}
	if _, err := spool.NewSource(spool.Config{ReaderID: "ok"}); err == nil {
		t.Fatal("expected missing directory error")
	}
}

func TestClaimEmptyAndRejectDeletesWhenDisabled(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	src, err := spool.NewSource(spool.Config{
		Directory:        dir,
		ReaderID:         "reader-a",
		MaxRejectedFiles: 0,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, claimErr := src.Claim()
	if claimErr != nil || got != nil {
		t.Fatalf("empty claim: %v %#v", claimErr, got)
	}

	name := "00000000000000000007-1111111111111111.spool"
	writeSpool(t, dir, name, []byte("z"))
	claimed, claimErr := src.Claim()
	if claimErr != nil || claimed == nil {
		t.Fatalf("claim: %v %#v", claimErr, claimed)
	}
	if rejectErr := src.Reject(claimed); rejectErr != nil {
		t.Fatal(rejectErr)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("expected delete on reject, got %v", names(entries))
	}
}

func TestRejectTrimsOldFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	src, err := spool.NewSource(spool.Config{
		Directory:        dir,
		ReaderID:         "reader-a",
		MaxRejectedFiles: 1,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"00000000000000000008-aaaaaaaaaaaaaaaa.spool",
		"00000000000000000009-bbbbbbbbbbbbbbbb.spool",
	} {
		writeSpool(t, dir, name, []byte("p"))
		claimed, claimErr := src.Claim()
		if claimErr != nil || claimed == nil {
			t.Fatalf("claim %s: %v %#v", name, claimErr, claimed)
		}
		if rejectErr := src.Reject(claimed); rejectErr != nil {
			t.Fatal(rejectErr)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "rejected"))
	if len(entries) != 1 {
		t.Fatalf("trim rejected want 1 got %d %v", len(entries), names(entries))
	}
}

func TestStaleClaimInShard(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Unix(1_700_000_601, 0)
	src, err := spool.NewSource(spool.Config{
		Directory:  dir,
		ReaderID:   "reader-b",
		StaleClaim: 10 * time.Minute,
		Now:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	shard := filepath.Join(dir, "cd")
	if mkdirErr := os.Mkdir(shard, 0o700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	name := "cd/0000000000000000000a-cccccccccccccccc.spool"
	legacy := "." + filepath.Base(name) + ".claimed-reader-a-1700000000"
	writeSpool(t, shard, legacy, []byte("x"))

	got, claimErr := src.Claim()
	if claimErr != nil || got == nil || got.Name != name {
		t.Fatalf("shard stale: %v %#v", claimErr, got)
	}
}

func TestTemporaryCleanupAndDefaults(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	src, err := spool.NewSource(spool.Config{
		Directory:      dir,
		ReaderID:       "reader-a",
		StaleTemporary: time.Minute,
		Now:            func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	tmp := ".orphan.tmp"
	writeSpool(t, dir, tmp, []byte("t"))
	old := now.Add(-2 * time.Minute)
	if chtimesErr := os.Chtimes(filepath.Join(dir, tmp), old, old); chtimesErr != nil {
		t.Fatal(chtimesErr)
	}
	got, claimErr := src.Claim()
	if claimErr != nil || got != nil {
		t.Fatalf("claim: %v %#v", claimErr, got)
	}
	if _, statErr := os.Stat(filepath.Join(dir, tmp)); !os.IsNotExist(statErr) {
		t.Fatalf("temp should be removed: %v", statErr)
	}
}

func TestRejectWhenRejectedPathIsFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	src, err := spool.NewSource(spool.Config{
		Directory:        dir,
		ReaderID:         "reader-a",
		MaxRejectedFiles: 2,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	writeSpool(t, dir, "rejected", []byte("not-a-dir"))
	name := "0000000000000000000b-dddddddddddddddd.spool"
	writeSpool(t, dir, name, []byte("z"))
	claimed, claimErr := src.Claim()
	if claimErr != nil || claimed == nil {
		t.Fatalf("claim: %v %#v", claimErr, claimed)
	}
	if rejectErr := src.Reject(claimed); rejectErr == nil {
		t.Fatal("expected reject error when rejected is a file")
	}
}

func TestParseMissingLabelsKey(t *testing.T) {
	t.Parallel()
	h := map[string]any{
		"application": "blog",
		"event_type":  "wall",
		"period_ns":   1,
		"start_ns":    startNs,
		"end_ns":      startNs,
		"format":      spool.FormatCompactStacks,
	}
	if _, err := spool.Parse(buildSpool(t, h, nil)); err == nil {
		t.Fatal("expected missing labels error")
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
