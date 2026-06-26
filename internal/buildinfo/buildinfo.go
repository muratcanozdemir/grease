// Package buildinfo exposes build-time provenance for the grease binary.
//
// The three variables are populated at link time via -ldflags -X. A binary that
// can report its own version, source commit, and build date is a basic
// operational courtesy — when a built artifact is in someone's hands and
// something is off, "which commit is this" should not require guesswork. The
// defaults below are what an unstamped `go build` or `go run` produces, so a
// developer build is honestly labelled as such rather than masquerading as a
// release.
package buildinfo

import (
	"fmt"
	"runtime"
)

// These are overridden at build time. See the release workflow's ldflags.
var (
	// Version is the release tag, e.g. "v1.2.0", or "dev" for an unstamped build.
	Version = "dev"
	// Commit is the short git SHA the binary was built from.
	Commit = "none"
	// Date is the build timestamp (RFC 3339), set by the build pipeline.
	Date = "unknown"
)

// String renders a single-line provenance summary including the Go toolchain
// and target platform, both of which come from the runtime rather than ldflags
// so they are always accurate.
func String() string {
	return fmt.Sprintf("grease %s (commit %s, built %s, %s, %s/%s)",
		Version, Commit, Date, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
