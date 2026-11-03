// SPDX-License-Identifier: Apache-2.0

// Command settings-image turns a folder of settings into a content-addressed OCI image that a
// kubelet can mount as volumes[].image, and pushes it to a registry.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/ConfigButler/settings-image/internal/buildinfo"
)

// Exit codes (docs/cli.md §6). A usage error is 2, as for flag.ExitOnError.
const (
	exitOK      = 0
	exitError   = 1 // a retry might fix it: network, registry, Git
	exitUsage   = 2
	exitRefused = 3 // the input breaks a rule; a retry cannot help
	exitDenied  = 4 // the registry refused the credential (401 or 403); a retry cannot help
)

const usage = `settings-image: build settings into a mountable OCI image

Usage:
  settings-image build <folder> [--oci-layout <dir>] [flags]
                            build the image; write it nowhere, or to an OCI layout
  settings-image push <folder> --repository <host/path> [flags]
                            build the image and push it
  settings-image version    print the build this binary came from

The folder holds resources/ (Kubernetes objects) and raw/ (any file). It must be committed in
a Git work tree, for provenance and the sha- tag; outside Git, add --no-provenance.

Run "settings-image build -h" for the flags.
Docs: https://github.com/ConfigButler/settings-image#readme
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run is main without the process: it returns the exit code, so tests can drive it.
func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "build", "push":
		return runBuild(ctx, args[0] == "push", args[1:], stdin, stdout, stderr)
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
