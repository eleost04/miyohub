// Package buildinfo supplies the same release version to the CLI and services.
package buildinfo

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var source string

func Version() string { return strings.TrimSpace(source) }
