// Package version holds build-time metadata for the tracewick-agent binary.
package version

// Version is the semantic version of this module release.
// release-please / GoReleaser may overwrite this via ldflags later.
const Version = "0.0.0-dev"

// String returns the version string for CLI and logs.
func String() string {
	return Version
}
