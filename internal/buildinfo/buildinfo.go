// SPDX-License-Identifier: Apache-2.0

// Package buildinfo is what a settings-image binary knows about the build it came from.
//
// The values are injected with -ldflags "-X github.com/ConfigButler/settings-image/internal/buildinfo.<var>=…"
// by `task build`. A plain `go build` or `go run` keeps the defaults below, which is how a
// development binary is told apart from a released one.
//
// The version comes from .release-please-manifest.json, never from a tag: release-please's
// release PR bumps the manifest, and the build of that merge commit carries the new number.
package buildinfo

import (
	"fmt"
	"runtime"
)

//nolint:gochecknoglobals // set at link time with -ldflags -X; a constant cannot be.
var (
	version   = "dev"
	gitCommit = "unknown"
	gitDirty  = "0"
	buildDate = "unknown"
)

// Info is the build metadata the CLI prints for `version`.
type Info struct {
	Version   string
	GitCommit string
	Dirty     bool
	BuildDate string
	GoVersion string
}

// Get returns the injected build metadata.
func Get() Info {
	return Info{
		Version:   version,
		GitCommit: gitCommit,
		Dirty:     gitDirty == "1",
		BuildDate: buildDate,
		GoVersion: runtime.Version(),
	}
}

// String is the one-line form printed by `settings-image version`.
func (i Info) String() string {
	commit := i.GitCommit
	if i.Dirty {
		commit += "-dirty"
	}
	return fmt.Sprintf("version=%s commit=%s built=%s go=%s", i.Version, commit, i.BuildDate, i.GoVersion)
}
