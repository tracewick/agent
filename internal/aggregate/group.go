package aggregate

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/tracewick/agent/internal/spool"
)

// Member is one claimed/parsed spool file offered for grouping.
type Member struct {
	// File is the parsed SpoolFile (required).
	File *spool.File
	// Name is the relative spool path; optional. When set, a filename aggregation-key
	// hint is parsed and must match the header hash or GroupBy fails (header re-check).
	// Prefer isolating mismatches in the caller (Drain) before GroupBy so one bad name
	// cannot fail an entire batch.
	Name string
	// HintHash overrides filename parsing when non-empty (tests / callers that already parsed the name).
	HintHash string
}

// Group is one merge-compatible set: same ADR-12 key and window id.
type Group struct {
	Key      Key
	Hash     string
	WindowID int64
	Files    []*spool.File
	StartNs  int64
	EndNs    int64
}

// Test seam for GroupBy hash collision path.
var hashOf = func(k Key) string { return k.Hash() }

// GroupBy buckets members by header hash + window id.
//
// The filename hash is only a hint: GroupBy always hashes headers and rejects a member when a
// hint is present and disagrees. Blind merge by filename prefix is forbidden
// (ADR 12: hash is a hint; re-validate headers before merge).
func GroupBy(window *Window, members []Member) ([]Group, error) {
	if window == nil {
		return nil, fmt.Errorf("aggregate: window is required")
	}
	type bucket struct {
		key      Key
		hash     string
		windowID int64
		files    []*spool.File
		startNs  int64
		endNs    int64
	}
	buckets := map[string]*bucket{}
	order := make([]string, 0)

	for i, m := range members {
		if m.File == nil {
			return nil, fmt.Errorf("aggregate: member %d has nil file", i)
		}
		key, err := KeyFromFile(m.File)
		if err != nil {
			return nil, fmt.Errorf("aggregate: member %d: %w", i, err)
		}
		hash := hashOf(key)
		hint := m.HintHash
		if hint == "" && m.Name != "" {
			hint = AggregationKeyHintFromName(m.Name)
		}
		if hint != "" && hint != key.Hash() {
			return nil, fmt.Errorf(
				"aggregate: member %d header hash %s disagrees with name hint %s",
				i,
				key.Hash(),
				hint,
			)
		}
		windowID := window.IDFor(m.File.StartNs)
		groupKey := hash + "\x00" + formatInt(windowID)
		b, ok := buckets[groupKey]
		if !ok {
			b = &bucket{
				key:      key,
				hash:     hash,
				windowID: windowID,
				startNs:  m.File.StartNs,
				endNs:    m.File.EndNs,
			}
			buckets[groupKey] = b
			order = append(order, groupKey)
		} else if !b.key.Equal(key) {
			return nil, fmt.Errorf("aggregate: hash collision or corrupt headers for hash %s", hash)
		}
		b.files = append(b.files, m.File)
		if m.File.StartNs < b.startNs {
			b.startNs = m.File.StartNs
		}
		if m.File.EndNs > b.endNs {
			b.endNs = m.File.EndNs
		}
	}

	sort.Strings(order)
	out := make([]Group, 0, len(order))
	for _, gk := range order {
		b := buckets[gk]
		out = append(out, Group{
			Key:      b.key,
			Hash:     b.hash,
			WindowID: b.windowID,
			Files:    b.files,
			StartNs:  b.startNs,
			EndNs:    b.endNs,
		})
	}
	return out, nil
}

func formatInt(v int64) string {
	return strconv.FormatInt(v, 10)
}
