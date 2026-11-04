package spool

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeFileInfo struct {
	size    int64
	mode    fs.FileMode
	modTime time.Time
}

func (f fakeFileInfo) Name() string       { return "x" }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return f.modTime }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return nil }

func restoreOS(t *testing.T) {
	t.Helper()
	stat, rename, readFile := osStat, osRename, osReadFile
	remove, readDir, mkdirAll := osRemove, osReadDir, osMkdirAll
	t.Cleanup(func() {
		osStat, osRename, osReadFile = stat, rename, readFile
		osRemove, osReadDir, osMkdirAll = remove, readDir, mkdirAll
	})
}

func TestTryClaimErrorBranches(t *testing.T) {
	restoreOS(t)

	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	src, err := NewSource(Config{
		Directory:    dir,
		ReaderID:     "reader-a",
		MaxFileBytes: 8,
		Now:          func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	name := "00000000000000000001-aaaaaaaaaaaaaaaa.spool"
	path := filepath.Join(dir, name)
	if writeErr := os.WriteFile(path, []byte("tiny"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}

	osStat = func(string) (os.FileInfo, error) { return nil, errors.New("stat boom") }
	if got := src.tryClaim(name); got != nil {
		t.Fatalf("stat fail: %#v", got)
	}

	osStat = func(string) (os.FileInfo, error) {
		return fakeFileInfo{size: 4, modTime: now}, nil
	}
	osRename = func(string, string) error { return errors.New("rename boom") }
	if got := src.tryClaim(name); got != nil {
		t.Fatalf("rename fail: %#v", got)
	}

	osRename = os.Rename
	osReadFile = func(string) ([]byte, error) { return nil, errors.New("read boom") }
	if got := src.tryClaim(name); got != nil {
		t.Fatalf("read fail: %#v", got)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("file should be restored after read fail: %v", statErr)
	}

	osReadFile = func(string) ([]byte, error) { return []byte("0123456789abcdef"), nil }
	osStat = func(string) (os.FileInfo, error) {
		return fakeFileInfo{size: 4, modTime: now}, nil
	}
	if got := src.tryClaim(name); got != nil {
		t.Fatalf("post-read oversize: %#v", got)
	}
}

func TestRejectRenameFailure(t *testing.T) {
	restoreOS(t)

	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	src, err := NewSource(Config{
		Directory:        dir,
		ReaderID:         "reader-a",
		MaxRejectedFiles: 2,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	claimedPath := filepath.Join(dir, ".x.spool.claimed-reader-a-1")
	if writeErr := os.WriteFile(claimedPath, []byte("z"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	osRename = func(string, string) error { return errors.New("reject rename boom") }
	if rejectErr := src.Reject(&Claimed{Name: "x.spool", Path: claimedPath}); rejectErr == nil {
		t.Fatal("expected reject rename error")
	}
}

func TestScanReadDirFailureAndDefaults(t *testing.T) {
	restoreOS(t)

	src, err := NewSource(Config{Directory: t.TempDir(), ReaderID: "reader-a"})
	if err != nil {
		t.Fatal(err)
	}
	if src.cfg.Now == nil {
		t.Fatal("Now default missing")
	}
	if src.cfg.MaxFileBytes != 4<<20 || src.cfg.StaleClaim != 10*time.Minute || src.cfg.StaleTemporary != time.Hour {
		t.Fatalf("defaults = %#v", src.cfg)
	}

	osReadDir = func(string) ([]os.DirEntry, error) { return nil, errors.New("readdir boom") }
	files, scanErr := src.scan()
	if scanErr == nil || len(files) != 0 {
		t.Fatalf("scan on readdir fail: files=%v err=%v", files, scanErr)
	}
	claimed, claimErr := src.Claim()
	if claimErr == nil || claimed != nil {
		t.Fatalf("claim on unreadable dir: claimed=%v err=%v", claimed, claimErr)
	}

	// Nested shard ReadDir failure is ignored (not a root error).
	dir := t.TempDir()
	src2, err2 := NewSource(Config{Directory: dir, ReaderID: "reader-a"})
	if err2 != nil {
		t.Fatal(err2)
	}
	shard := filepath.Join(dir, "ab")
	if mkdirErr := os.Mkdir(shard, 0o700); mkdirErr != nil { //nolint:gosec // temp
		t.Fatal(mkdirErr)
	}
	calls := 0
	osReadDir = func(path string) ([]os.DirEntry, error) {
		calls++
		if path == shard {
			return nil, errors.New("shard boom")
		}
		return os.ReadDir(path)
	}
	files2, scanErr2 := src2.scan()
	if scanErr2 != nil || len(files2) != 0 {
		t.Fatalf("nested shard readdir fail should not abort scan: files=%v err=%v", files2, scanErr2)
	}
	issues := src2.TakeScanIssues()
	if len(issues) != 1 || !strings.Contains(issues[0], "shard boom") {
		t.Fatalf("expected scan issue for EACCES-like shard error, got %v", issues)
	}
	if calls < 2 {
		t.Fatalf("expected root+shard ReadDir, calls=%d", calls)
	}

	osStat = func(string) (os.FileInfo, error) { return nil, errors.New("stat boom") }
	if src.isOlderThan(filepath.Join(t.TempDir(), "missing"), time.Second) {
		t.Fatal("missing path must not be older")
	}

	src.trimRejected(filepath.Join(t.TempDir(), "nope"))
}

func TestClaimedRelativeNameAndAbandonedEpoch(t *testing.T) {
	t.Parallel()

	if _, ok := claimedRelativeName("nope", ""); ok {
		t.Fatal("expected reject")
	}
	if _, ok := claimedRelativeName(".notaspool.claimed-x-1", ""); ok {
		t.Fatal("expected reject without .spool.claimed marker shape")
	}
	// leading-dot base after strip → not a spool file
	if name, ok := claimedRelativeName("..spool.claimed-x-1", "ab"); ok {
		t.Fatalf("expected reject dotted base, got %q", name)
	}
	if name, ok := claimedRelativeName(".ok.spool.claimed-x-1", ""); !ok || name != "ok.spool" {
		t.Fatalf("got %q %v", name, ok)
	}
	if name, ok := claimedRelativeName(".ok.spool.claimed-x-1", "ab"); !ok || name != "ab/ok.spool" {
		t.Fatalf("got %q %v", name, ok)
	}

	dir := t.TempDir()
	now := time.Unix(1_700_000_000, 0)
	src, err := NewSource(Config{
		Directory:  dir,
		ReaderID:   "reader-a",
		StaleClaim: time.Minute,
		Now:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	// digits overflow int64 → ParseInt fails → not abandoned via epoch
	entry := ".x.spool.claimed-id-99999999999999999999"
	path := filepath.Join(dir, entry)
	if writeErr := os.WriteFile(path, []byte("x"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if src.isClaimAbandoned(entry, path) {
		t.Fatal("overflow epoch must not count as abandoned via epoch parse")
	}
}
