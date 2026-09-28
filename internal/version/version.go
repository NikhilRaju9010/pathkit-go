// Package version reports which version of PathKit is running.
package version

import "runtime/debug"

// Version is stamped in at build time by the release tool, with
// -ldflags "-X github.com/NikhilRaju9010/pathkit-go/internal/version.Version=v0.1.0".
// It is empty in a normal local build.
var Version = ""

// String returns the version to show for --version, in this order:
// the stamped Version; else the module version Go records for
// "go install ...@v0.1.0" (or for a build inside a git checkout);
// else "dev".
func String() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}
