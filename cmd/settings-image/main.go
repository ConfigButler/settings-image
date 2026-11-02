// SPDX-License-Identifier: Apache-2.0

// Command settings-image turns a folder of settings into a content-addressed OCI image that a
// kubelet can mount as volumes[].image, and pushes it to a registry.
//
// Only `version` exists so far; `build` and `push` follow.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/ConfigButler/settings-image/internal/buildinfo"
)

// Exit codes. A usage error is 2, as for flag.ExitOnError.
const (
	exitOK    = 0
	exitUsage = 2
)

const usage = `settings-image: build settings into a mountable OCI image

Usage:
  settings-image version    print the build this binary came from
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main without the process: it returns the exit code, so tests can drive it.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "version", "--version", "-version":
		_, _ = fmt.Fprintf(stdout, "settings-image %s\n", buildinfo.Get())
		return exitOK
	case "help", "--help", "-h":
		_, _ = fmt.Fprint(stdout, usage)
		return exitOK
	default:
		_, _ = fmt.Fprintf(stderr, "settings-image: unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}
}
