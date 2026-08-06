// Package version holds the single RackOps version constant.
//
// It exists so every binary in the repo reports the same number. The release
// process bumps exactly one line here; before this package the edge binary
// carried its own copy and silently drifted a release behind.
package version

// Current is the RackOps version. Bump this on release — nothing else.
const Current = "0.6.0"
