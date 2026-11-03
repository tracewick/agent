package aggregate

import (
	"path/filepath"
	"regexp"
)

// With-hash spool basename: <time20>-<aggHash16>-<random16>.spool (SpoolFileName::WITH_HASH).
var withHashNameRE = regexp.MustCompile(`^\d{20}-([0-9a-f]{16})-[0-9a-f]{16}\.spool$`)

// AggregationKeyHintFromName returns the 16-hex aggregation hash from a spool basename, or "".
func AggregationKeyHintFromName(name string) string {
	base := filepath.Base(name)
	m := withHashNameRE.FindStringSubmatch(base)
	if m == nil {
		return ""
	}
	return m[1]
}
