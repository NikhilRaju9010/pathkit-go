//go:build go1.18

package constcond

// This file has a build constraint (always satisfied here, so the package
// compiles on every system). A constant declared in a constrained file may
// have another value in another build, so it is not trusted.
const taggedOff = false
