//go:build !pathkit_fixture_never

package models

// SystemKind is in a file with a build condition: another build may use other
// code, so it is not read as the function that runs.
func SystemKind() interface{} { return "linux" }

// Mode is declared in a file with a build condition.
const Mode = "a"
