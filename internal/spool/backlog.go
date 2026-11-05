package spool

import (
	"path/filepath"
	"time"
)

// Backlog is a point-in-time view of unclaimed *.spool files (not claimed/rejected).
type Backlog struct {
	Files      int
	Bytes      int64
	OldestAge  time.Duration // zero when Files == 0
	Unreadable bool          // root spool directory could not be listed (EACCES / missing)
	Incomplete bool          // some shards/files could not be listed (partial view)
}

// Backlog scans the spool directory for deliverable files without claiming them.
// Age uses file ModTime relative to Now (legacy-friendly; claim epoch applies only after claim).
// When the root directory is unreadable, Unreadable is set and counts stay zero (not "empty").
// Nested listing failures set Incomplete (counts cover only reachable shards).
func (s *Source) Backlog() Backlog {
	now := s.cfg.Now()
	var out Backlog
	var oldestMod time.Time
	var walk func(directory, prefix string) bool
	walk = func(directory, prefix string) bool {
		entries, err := osReadDir(directory)
		if err != nil {
			if prefix == "" {
				out.Unreadable = true
			} else {
				out.Incomplete = true
				s.noteScanIssue("backlog "+prefix, err)
			}
			return false
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
					walk(path, relative)
				}
				continue
			}
			if !isSpoolFile(name) {
				continue
			}
			info, err := osStat(path)
			if err != nil {
				if !isBenignScanErr(err) {
					out.Incomplete = true
					s.noteScanIssue("backlog "+relative, err)
				}
				continue
			}
			out.Files++
			out.Bytes += info.Size()
			mod := info.ModTime()
			if oldestMod.IsZero() || mod.Before(oldestMod) {
				oldestMod = mod
			}
		}
		return true
	}
	walk(s.cfg.Directory, "")
	if out.Unreadable {
		return out
	}
	if out.Files > 0 && !oldestMod.IsZero() {
		age := now.Sub(oldestMod)
		if age > 0 {
			out.OldestAge = age
		}
	}
	return out
}
