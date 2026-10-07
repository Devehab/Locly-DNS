// Package version holds build metadata for the localdns binary.
package version

// Version is the LocalDNS release version. Release builds override it with
// -ldflags "-X github.com/devehab/locly-dns/internal/version.Version=x.y.z".
var Version = "0.3.0"

// Commit is the git commit the binary was built from (set by release builds).
var Commit = ""
