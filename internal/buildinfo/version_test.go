package buildinfo

import (
	"regexp"
	"testing"
)

func TestVersionUsesReleaseOrNumberedPrerelease(t *testing.T) {
	if !regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(beta|rc)\.[1-9][0-9]*)?$`).MatchString(Version()) {
		t.Fatal("invalid release version", Version())
	}
}
