package version

import (
	"regexp"
	"testing"
)

var semverPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$`)

func TestCurrent_SemverFormat(t *testing.T) {
	if !semverPattern.MatchString(Current) {
		t.Fatalf("Current version string %q does not match valid SemVer format", Current)
	}
}
