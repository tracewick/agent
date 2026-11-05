// Package version holds build-time metadata for the tracewick-agent binary.
package version

// Version is the semantic version of this module release.
// GoReleaser overwrites this via -ldflags -X at release build time.
var Version = "0.0.0-dev"

// String returns the version string for CLI and logs.
func String() string {
	return Version
}
