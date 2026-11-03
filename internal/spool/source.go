// Package spool implements the Tracewick spool contract from tracewick/tracewick:
// SpoolFile parse (v2 / compact-stacks) and directory claim/ack/release/reject.
package spool

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	RejectedDirectory = "rejected"
	// DefaultMaxRejectedFiles matches PHP SpoolReaderConfig::DEFAULT_MAX_REJECTED_FILES.
	DefaultMaxRejectedFiles = 100
	claimMarker             = ".claimed-"
	spoolSuffix             = ".spool"
	temporarySuffix         = ".tmp"
)

var (
	shardNameRE    = regexp.MustCompile(`^[0-9a-f]{2}$`)
	claimEpochRE   = regexp.MustCompile(`\.claimed-[A-Za-z0-9._-]+-(\d+)$`)
	readerIDRE     = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	errBadReaderID = errors.New("reader id must be 1..64 letters, digits, dots, underscores or dashes")

	// OS seams — overridden in tests for error paths.
	osStat     = os.Stat
	osRename   = os.Rename
	osReadFile = os.ReadFile
	osRemove   = os.Remove
	osReadDir  = os.ReadDir
	osMkdirAll = os.MkdirAll
)

// Config configures a directory spool reader.
type Config struct {
	Directory        string
	ReaderID         string
	MaxFileBytes     int64
	StaleClaim       time.Duration
	StaleTemporary   time.Duration
	MaxRejectedFiles int
	Now              func() time.Time // optional; defaults to time.Now
}

// Claimed is one claimed spool file held by this reader.
type Claimed struct {
	// Name is the relative spool path (possibly "ab/name.spool").
	Name string
	// Bytes is the file contents after a successful claim.
	Bytes []byte
	// Path is the absolute claimed path (.X.spool.claimed-<id>-<epoch>).
	Path string
	// ClaimedAt is the claim epoch embedded in Path (ownership budget clock).
	ClaimedAt time.Time
}

// Source reads a Tracewick spool directory with rename claims.
type Source struct {
	cfg   Config
	queue []string
}

// NewSource validates cfg and returns a directory spool source.
func NewSource(cfg Config) (*Source, error) {
	if cfg.Directory == "" {
		return nil, errors.New("spool directory is required")
	}
	if !readerIDRE.MatchString(cfg.ReaderID) {
		return nil, errBadReaderID
	}
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = 4 << 20
	}
	if cfg.StaleClaim <= 0 {
		cfg.StaleClaim = 10 * time.Minute
	}
	if cfg.StaleTemporary <= 0 {
		cfg.StaleTemporary = time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Source{cfg: cfg}, nil
}

// Claim takes the next available spool file, oldest name first.
func (s *Source) Claim() (*Claimed, error) {
	scanned := false
	for {
		if len(s.queue) == 0 {
			if scanned {
				return nil, nil
			}
			s.queue = s.scan()
			scanned = true
		}
		for len(s.queue) > 0 {
			name := s.queue[0]
			s.queue = s.queue[1:]
			if claimed := s.tryClaim(name); claimed != nil {
				return claimed, nil
			}
		}
	}
}

// Acknowledge deletes a successfully delivered claimed file.
func (s *Source) Acknowledge(c *Claimed) error {
	return osRemove(c.Path)
}

// Release returns a claimed file to the queue as its original name.
func (s *Source) Release(c *Claimed) error {
	return osRename(c.Path, s.path(c.Name))
}

// Reject moves a claimed file to rejected/ (or deletes it when MaxRejectedFiles is 0).
func (s *Source) Reject(c *Claimed) error {
	if s.cfg.MaxRejectedFiles == 0 {
		return osRemove(c.Path)
	}
	rejectedDir := s.path(RejectedDirectory)
	if err := osMkdirAll(rejectedDir, 0o700); err != nil {
		_ = osRemove(c.Path)
		return err
	}
	dest := filepath.Join(rejectedDir, filepath.Base(c.Name))
	if err := osRename(c.Path, dest); err != nil {
		_ = osRemove(c.Path)
		return err
	}
	s.trimRejected(rejectedDir)
	return nil
}

func (s *Source) scan() []string {
	var files []string
	s.scanDirectory(s.cfg.Directory, "", &files)
	sort.Strings(files)
	return files
}

func (s *Source) scanDirectory(directory, prefix string, files *[]string) {
	entries, err := osReadDir(directory)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		relative := name
		if prefix != "" {
			relative = prefix + "/" + name
		}
		path := filepath.Join(directory, name)

		if entry.IsDir() {
			if name == RejectedDirectory && prefix == "" {
				continue
			}
			if shardNameRE.MatchString(name) {
				s.scanDirectory(path, relative, files)
			}
			continue
		}

		switch {
		case isSpoolFile(name):
			*files = append(*files, relative)
		case strings.HasPrefix(name, ".") && strings.Contains(name, spoolSuffix+claimMarker):
			if spoolName, ok := claimedRelativeName(name, prefix); ok {
				if s.isClaimAbandoned(name, path) {
					if err := osRename(path, s.path(spoolName)); err == nil {
						*files = append(*files, spoolName)
					}
				}
			}
		case strings.HasPrefix(name, ".") && strings.HasSuffix(name, temporarySuffix):
			if s.isOlderThan(path, s.cfg.StaleTemporary) {
				_ = osRemove(path)
			}
		}
	}
}

func (s *Source) tryClaim(name string) *Claimed {
	source := s.path(name)
	info, err := osStat(source)
	if err != nil {
		return nil
	}
	if info.Size() > s.cfg.MaxFileBytes {
		oversized := s.claimPath(name)
		if renameErr := osRename(source, oversized); renameErr == nil {
			_ = s.Reject(&Claimed{Name: name, Path: oversized})
		}
		return nil
	}

	claimedAt := s.cfg.Now().Truncate(time.Second)
	claimedPath := s.claimPathAt(name, claimedAt)
	if renameErr := osRename(source, claimedPath); renameErr != nil {
		return nil
	}
	data, readErr := osReadFile(claimedPath)
	if readErr != nil {
		_ = osRename(claimedPath, source)
		return nil
	}
	if int64(len(data)) > s.cfg.MaxFileBytes {
		_ = s.Reject(&Claimed{Name: name, Bytes: data, Path: claimedPath, ClaimedAt: claimedAt})
		return nil
	}
	return &Claimed{Name: name, Bytes: data, Path: claimedPath, ClaimedAt: claimedAt}
}

func (s *Source) claimPath(name string) string {
	return s.claimPathAt(name, s.cfg.Now())
}

func (s *Source) claimPathAt(name string, at time.Time) string {
	epoch := at.Unix()
	dir := filepath.Dir(s.path(name))
	base := filepath.Base(name)
	return filepath.Join(dir, "."+base+claimMarker+s.cfg.ReaderID+"-"+strconv.FormatInt(epoch, 10))
}

func claimedRelativeName(entry, prefix string) (string, bool) {
	marker := strings.Index(entry, spoolSuffix+claimMarker)
	if !strings.HasPrefix(entry, ".") || marker < 0 {
		return "", false
	}
	base := entry[1 : marker+len(spoolSuffix)]
	if !isSpoolFile(base) {
		return "", false
	}
	if prefix == "" {
		return base, true
	}
	return prefix + "/" + base, true
}

func (s *Source) isClaimAbandoned(entry, path string) bool {
	if m := claimEpochRE.FindStringSubmatch(entry); len(m) == 2 {
		sec, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			return false
		}
		claimedAt := time.Unix(sec, 0)
		return s.cfg.Now().Sub(claimedAt) > s.cfg.StaleClaim
	}
	return s.isOlderThan(path, s.cfg.StaleClaim)
}

func (s *Source) isOlderThan(path string, age time.Duration) bool {
	info, err := osStat(path)
	if err != nil {
		return false
	}
	return s.cfg.Now().Sub(info.ModTime()) > age
}

func (s *Source) trimRejected(rejectedDir string) {
	entries, err := osReadDir(rejectedDir)
	if err != nil {
		return
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && isSpoolFile(e.Name()) {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	extra := len(files) - s.cfg.MaxRejectedFiles
	for i := 0; i < extra; i++ {
		_ = osRemove(filepath.Join(rejectedDir, files[i]))
	}
}

func (s *Source) path(entry string) string {
	return filepath.Join(s.cfg.Directory, filepath.FromSlash(entry))
}

func isSpoolFile(name string) bool {
	return strings.HasSuffix(name, spoolSuffix) && !strings.HasPrefix(name, ".")
}

// ValidateReaderID exposes reader-id rules for config parsing.
func ValidateReaderID(id string) error {
	if !readerIDRE.MatchString(id) {
		return fmt.Errorf("%w: %q", errBadReaderID, id)
	}
	return nil
}
