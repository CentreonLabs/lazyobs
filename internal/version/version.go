// Package version exposes build information injected at link time.
package version

import "fmt"

// Set with -ldflags "-X github.com/CentreonLabs/lazyobs/internal/version.Version=..." at build time.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a human-readable version line.
func String() string {
	return fmt.Sprintf("lazyobs %s (commit %s, built %s)", Version, Commit, Date)
}
